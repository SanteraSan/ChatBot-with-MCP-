package bridge

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Bot struct {
	token  string
	client *http.Client
	offset int64
}

func NewBot(token string) *Bot {
	return &Bot{
		token:  token,
		client: &http.Client{Timeout: 40 * time.Second},
	}
}

type Update struct {
	UpdateID int64   `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      Chat   `json:"chat"`
	From      User   `json:"from"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID int64 `json:"id"`
}

func (b *Bot) Poll(ctx context.Context) ([]Update, error) {
	payload := map[string]any{
		"timeout":         25,
		"allowed_updates": []string{"message"},
	}
	if b.offset != 0 {
		payload["offset"] = b.offset
	}
	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := b.call(ctx, "getUpdates", payload, &result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram getUpdates failed")
	}
	for _, update := range result.Result {
		if update.UpdateID >= b.offset {
			b.offset = update.UpdateID + 1
		}
	}
	return result.Result, nil
}

func (b *Bot) Send(ctx context.Context, chatID int64, text string) error {
	var result struct {
		OK bool `json:"ok"`
	}
	err := b.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID,
		"text":    text,
	}, &result)
	if err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("telegram sendMessage failed")
	}
	return nil
}

func (b *Bot) SendFile(ctx context.Context, chatID int64, caption, body string) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if err := writer.WriteField("caption", caption); err != nil {
		return err
	}
	part, err := writer.CreateFormFile("document", "report.txt")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(part, body); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.methodURL("sendDocument"), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram sendDocument: %s", bytes.TrimSpace(raw))
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("telegram sendDocument failed")
	}
	return nil
}

func (b *Bot) SetCommands(ctx context.Context, commands []map[string]string) error {
	var result struct {
		OK bool `json:"ok"`
	}
	return b.call(ctx, "setMyCommands", map[string]any{"commands": commands}, &result)
}

func (b *Bot) Deliver(ctx context.Context, chatID int64, delivery Delivery) error {
	if delivery.File != "" {
		return b.SendFile(ctx, chatID, delivery.Caption, delivery.File)
	}
	for _, message := range delivery.Messages {
		if err := b.Send(ctx, chatID, message); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) call(ctx context.Context, method string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.methodURL(method), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram %s: %s", method, bytes.TrimSpace(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (b *Bot) methodURL(method string) string {
	return "https://api.telegram.org/bot" + b.token + "/" + method
}

func Authorized(header, want string) bool {
	header = strings.TrimSpace(header)
	want = strings.TrimSpace(want)
	if header == "" || want == "" {
		return false
	}
	if secureEqual(header, want) {
		return true
	}
	headerToken := bearerToken(header)
	wantToken := bearerToken(want)
	return headerToken != "" && secureEqual(headerToken, wantToken)
}

func bearerToken(value string) string {
	if len(value) < 7 || !strings.EqualFold(value[:7], "bearer ") {
		return value
	}
	return strings.TrimSpace(value[7:])
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
