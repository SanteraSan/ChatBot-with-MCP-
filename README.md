# Telegram-мост для автоматизаций Cursor

Один процесс принимает команды в Telegram и отдаёт отчёт обратно через MCP-инструмент `report_run`. Новая автоматизация добавляется блоком в `connectors.yaml` и теми же URL и заголовком MCP.

Секреты лежат вне репозитория, по умолчанию в `C:\Users\Windows_11\cursor-bridge.secrets.env`. Пример имён переменных — `.env.example`.

```text
go run ./cmd/bridge
```

MCP слушает `http://127.0.0.1:8787/mcp`. Публичный адрес для Cursor — этот путь за HTTPS-туннелем. Заголовок `Authorization` равен значению `MCP_AUTH_TOKEN`.
