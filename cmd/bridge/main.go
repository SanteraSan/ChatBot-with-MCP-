package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/santerasan/chatbot-with-mcp/internal/bridge"
)

func main() {
	connectors := envOr("CONNECTORS", "connectors.yaml")
	secrets := envOr("SECRETS_FILE", `C:\Users\Windows_11\cursor-bridge.secrets.env`)
	cfg, err := bridge.Load(connectors, secrets)
	if err != nil {
		log.Fatal(err)
	}
	store, err := bridge.OpenStore(cfg.StorePath)
	if err != nil {
		log.Fatal(err)
	}
	bot := bridge.NewBot(cfg.BotToken)
	svc := bridge.New(cfg, store, bot)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	commands := []map[string]string{{"command": "commands", "description": "Список команд"}}
	for _, connector := range cfg.Connectors {
		commands = append(commands, map[string]string{
			"command":     connector.Command,
			"description": connector.Title,
		})
	}
	if err := bot.SetCommands(ctx, commands); err != nil {
		log.Printf("set commands: %v", err)
	}

	go svc.Poll(ctx)
	go svc.Sweep(ctx)
	server := &http.Server{Addr: cfg.Addr, Handler: svc.Handler()}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	log.Printf("mcp listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
