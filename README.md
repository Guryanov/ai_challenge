# Чат с внешним API

Приложение на Go: веб-интерфейс шлёт сообщение во внешний API и показывает ответ.

## Возможности

- Авторизация: `Bearer`, `X-API-Key`, `Api-Key`, `none`.
- Формат: `generic` (`{"message":"..."}`) или `openai` (`/v1/chat/completions`).
- Промпты: `SYSTEM_PROMPT`, `ASSISTANT_PROMPT`, `USER_PROMPT_TEMPLATE`.

## Запуск

```bash
cp .env.secrets.example .env.secrets   # секреты, не коммитится
./run.sh
```

Вручную: `set -a; source .env.secrets; source .env; set +a; go run main.go`

Интерфейс: http://localhost:8080

## Переменные

| Переменная | Описание | По умолчанию |
|---|---|---|
| `EXTERNAL_API_URL` | URL внешнего API | `https://httpbin.org/anything` |
| `API_KEY` | Ключ авторизации | — |
| `AUTH_TYPE` | `bearer`, `x-api-key`, `api-key`, `none` | по `API_KEY` |
| `API_FORMAT` | `generic` или `openai` | по `API_KEY` |
| `SYSTEM_PROMPT` | Системный промпт | — |
| `ASSISTANT_PROMPT` | Начало ответа ассистента | — |
| `USER_PROMPT_TEMPLATE` | Шаблон, `{message}` | — |
| `PORT` | Порт сервера | `8080` |
