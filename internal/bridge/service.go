package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Service struct {
	cfg   Config
	store *Store
	bot   *Bot
	http  *http.Client
}

func New(cfg Config, store *Store, bot *Bot) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
		bot:   bot,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Service) Handler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "telegram-bridge", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_run",
		Description: "Send the finished automation report to the Telegram chat that started the run. Call once, only after analysis is complete. text must be plain text and should stay within 3500 characters.",
	}, s.reportRun)
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	mux.Handle("/mcp", s.protect(stream))
	return mux
}

type reportArgs struct {
	DeliveryToken string `json:"delivery_token" jsonschema:"delivery_token from the webhook body, unchanged"`
	Text          string `json:"text" jsonschema:"plain-text report, at most 3500 characters"`
}

func (s *Service) reportRun(ctx context.Context, _ *mcp.CallToolRequest, args reportArgs) (*mcp.CallToolResult, any, error) {
	run, ok := s.store.ByToken(strings.TrimSpace(args.DeliveryToken))
	if !ok || time.Now().After(run.Expires) {
		return toolError("unknown delivery_token"), nil, nil
	}
	if run.Reported {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "already delivered"}}}, map[string]bool{"ok": true}, nil
	}
	delivery := PrepareDelivery(args.Text, run.Reminded)
	if err := s.bot.Deliver(ctx, run.ChatID, delivery); err != nil {
		return toolError("telegram delivery failed"), nil, nil
	}
	if err := s.store.Update(run.Token, func(run *Run) { run.Reported = true }); err != nil {
		log.Printf("mark reported: %v", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "delivered"}}}, map[string]bool{"ok": true}, nil
}

func toolError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func (s *Service) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Protocol-Version, Mcp-Session-Id")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !Authorized(r.Header.Get("Authorization"), s.cfg.MCPAuth) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) Poll(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		updates, err := s.bot.Poll(ctx)
		if err != nil {
			log.Printf("telegram poll: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, update := range updates {
			s.handle(ctx, update.Message)
		}
	}
}

func (s *Service) Sweep(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, run := range s.store.DueReminder(now) {
				text := fmt.Sprintf("Отчёт не получен за %s. Прогон мог ещё идти или остановиться с ошибкой. Статус смотрите в панели Cursor.\nrunUuid: %s\nbackgroundComposerId: %s",
					s.timeoutText(run.ConnectorID), run.RunUUID, run.ComposerID)
				if err := s.bot.Send(ctx, run.ChatID, text); err != nil {
					log.Printf("reminder: %v", err)
					continue
				}
				if err := s.store.Update(run.Token, func(run *Run) { run.Reminded = true }); err != nil {
					log.Printf("mark reminded: %v", err)
				}
			}
			if err := s.store.Purge(now); err != nil {
				log.Printf("purge: %v", err)
			}
		}
	}
}

func (s *Service) timeoutText(connectorID string) string {
	for _, connector := range s.cfg.Connectors {
		if connector.ID == connectorID {
			return connector.Timeout.String()
		}
	}
	return "заданное время"
}

func (s *Service) handle(ctx context.Context, message Message) {
	if message.Chat.ID == 0 || message.Text == "" {
		return
	}
	if !s.cfg.Allows(message.From.ID) {
		_ = s.bot.Send(ctx, message.Chat.ID, "Эта команда доступна только разрешённым пользователям.")
		return
	}
	switch CommandName(message.Text) {
	case "commands", "help", "start":
		_ = s.bot.Send(ctx, message.Chat.ID, s.help())
	default:
		connector, ok := s.cfg.ByCommand[CommandName(message.Text)]
		if !ok {
			return
		}
		s.start(ctx, message, connector)
	}
}

func (s *Service) help() string {
	var b strings.Builder
	b.WriteString("Команды:\n")
	for _, connector := range s.cfg.Connectors {
		names := "/" + connector.Command
		if strings.Contains(connector.Command, "_") {
			names += " или /" + strings.ReplaceAll(connector.Command, "_", "-")
		}
		fmt.Fprintf(&b, "%s — %s\n", names, connector.Title)
	}
	b.WriteString("/commands — этот список")
	return b.String()
}

func (s *Service) start(ctx context.Context, message Message, connector Connector) {
	if !WebhookURLComplete(connector.WebhookURL) {
		_ = s.bot.Send(ctx, message.Chat.ID, "Webhook URL автоматизации неполный. Скопируйте его целиком из Cursor, вместе с id после /webhook/.")
		return
	}
	if active, ok := s.store.Active(connector.ID, time.Now()); ok {
		_ = s.bot.Send(ctx, message.Chat.ID, fmt.Sprintf("Прогон /%s ещё идёт.\nrunUuid: %s", connector.Command, active.RunUUID))
		return
	}
	token, err := newToken()
	if err != nil {
		_ = s.bot.Send(ctx, message.Chat.ID, "Не удалось создать токен запуска.")
		return
	}
	started, err := s.trigger(ctx, connector, token)
	if err != nil {
		log.Printf("trigger %s: %v", connector.ID, err)
		_ = s.bot.Send(ctx, message.Chat.ID, "Cursor не принял запуск. Проверьте webhook URL и ключ.")
		return
	}
	now := time.Now()
	run := Run{
		Token:       token,
		ChatID:      message.Chat.ID,
		UserID:      message.From.ID,
		ConnectorID: connector.ID,
		ComposerID:  started.ComposerID,
		RunUUID:     started.RunUUID,
		Started:     now,
		TimeoutAt:   now.Add(connector.Timeout),
		Expires:     now.Add(24 * time.Hour),
	}
	if err := s.store.Save(run); err != nil {
		log.Printf("save run: %v", err)
		_ = s.bot.Send(ctx, message.Chat.ID, "Запуск ушёл в Cursor, но локальная запись прогона не сохранилась.")
		return
	}
	_ = s.bot.Send(ctx, message.Chat.ID, fmt.Sprintf("Запуск принят.\nrunUuid: %s\nbackgroundComposerId: %s", run.RunUUID, run.ComposerID))
}

type startedRun struct {
	ComposerID string
	RunUUID    string
}

func (s *Service) trigger(ctx context.Context, connector Connector, token string) (startedRun, error) {
	payload, err := json.Marshal(map[string]string{
		"delivery_token": token,
		"command":        connector.Command,
	})
	if err != nil {
		return startedRun{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connector.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return startedRun{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	key := connector.WebhookKey
	if !strings.HasPrefix(strings.ToLower(key), "bearer ") {
		key = "Bearer " + key
	}
	req.Header.Set("Authorization", key)
	resp, err := s.http.Do(req)
	if err != nil {
		return startedRun{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return startedRun{}, err
	}
	if resp.StatusCode >= 300 {
		return startedRun{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var parsed struct {
		Success               bool   `json:"success"`
		BackgroundComposerID  string `json:"backgroundComposerId"`
		RunUUID               string `json:"runUuid"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return startedRun{}, err
	}
	if !parsed.Success || parsed.RunUUID == "" {
		return startedRun{}, fmt.Errorf("unexpected webhook response")
	}
	return startedRun{ComposerID: parsed.BackgroundComposerID, RunUUID: parsed.RunUUID}, nil
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
