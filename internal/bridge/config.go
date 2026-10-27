package bridge

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Connector struct {
	ID         string
	Command    string
	Title      string
	WebhookURL string
	WebhookKey string
	Timeout    time.Duration
}

type Config struct {
	BotToken   string
	Allowed    map[int64]struct{}
	MCPAuth    string
	Addr       string
	Connectors []Connector
	ByCommand  map[string]Connector
	StorePath  string
}

func Load(connectorsPath, secretsPath string) (Config, error) {
	if err := loadEnvFile(secretsPath); err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	raw, err := os.ReadFile(connectorsPath)
	if err != nil {
		return Config{}, err
	}
	var file struct {
		Connectors []struct {
			ID            string `yaml:"id"`
			Command       string `yaml:"command"`
			Title         string `yaml:"title"`
			WebhookURLEnv string `yaml:"webhook_url_env"`
			WebhookKeyEnv string `yaml:"webhook_key_env"`
			Timeout       string `yaml:"timeout"`
		} `yaml:"connectors"`
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return Config{}, err
	}
	cfg := Config{
		BotToken:  strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		MCPAuth:   strings.TrimSpace(os.Getenv("MCP_AUTH_TOKEN")),
		Addr:      envOr("MCP_ADDR", ":8787"),
		StorePath: envOr("RUN_STORE", "data/runs.json"),
		Allowed:   map[int64]struct{}{},
		ByCommand: map[string]Connector{},
	}
	if cfg.BotToken == "" || cfg.MCPAuth == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN and MCP_AUTH_TOKEN are required")
	}
	for _, id := range strings.Split(os.Getenv("TELEGRAM_ALLOWED_USER_IDS"), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("allowed user id %q: %w", id, err)
		}
		cfg.Allowed[n] = struct{}{}
	}
	if len(cfg.Allowed) == 0 {
		return Config{}, fmt.Errorf("TELEGRAM_ALLOWED_USER_IDS is empty")
	}
	for _, item := range file.Connectors {
		timeout, err := time.ParseDuration(item.Timeout)
		if err != nil {
			return Config{}, fmt.Errorf("connector %q timeout: %w", item.ID, err)
		}
		connector := Connector{
			ID:         item.ID,
			Command:    NormalizeCommand(item.Command),
			Title:      item.Title,
			WebhookURL: strings.TrimSpace(os.Getenv(item.WebhookURLEnv)),
			WebhookKey: strings.TrimSpace(os.Getenv(item.WebhookKeyEnv)),
			Timeout:    timeout,
		}
		if connector.ID == "" || connector.Command == "" || connector.WebhookURL == "" || connector.WebhookKey == "" {
			return Config{}, fmt.Errorf("connector %q needs id, command, and webhook env values", connector.ID)
		}
		if _, ok := cfg.ByCommand[connector.Command]; ok {
			return Config{}, fmt.Errorf("duplicate command %q", connector.Command)
		}
		cfg.ByCommand[connector.Command] = connector
		cfg.Connectors = append(cfg.Connectors, connector)
	}
	if len(cfg.Connectors) == 0 {
		return Config{}, fmt.Errorf("no connectors")
	}
	return cfg, nil
}

func (c Config) Allows(userID int64) bool {
	_, ok := c.Allowed[userID]
	return ok
}

func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, strings.TrimSpace(val)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func CommandName(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return NormalizeCommand(strings.Fields(text)[0])
}

func NormalizeCommand(command string) string {
	command = strings.TrimSpace(command)
	command = strings.TrimPrefix(command, "/")
	if name, _, ok := strings.Cut(command, "@"); ok {
		command = name
	}
	return strings.ReplaceAll(command, "-", "_")
}

func WebhookURLComplete(raw string) bool {
	const prefix = "https://api2.cursor.sh/automations/webhook/"
	rest := strings.TrimPrefix(strings.TrimSpace(raw), prefix)
	return rest != strings.TrimSpace(raw) && rest != "" && !strings.Contains(rest, " ")
}
