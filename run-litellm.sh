#!/bin/bash
# Запуск LiteLLM-прокси с секретами из .env.secrets.
# Требует запущенной Ollama (localhost:11434) и Postgres (docker compose up -d).
set -e

if [ ! -f .env.secrets ]; then
    echo "Ошибка: файл .env.secrets не найден."
    echo "Скопируйте .env.secrets.example в .env.secrets и заполните секреты."
    exit 1
fi

set -a
source .env.secrets
set +a

if [ -z "${LITELLM_MASTER_KEY:-}" ]; then
    echo "Ошибка: LITELLM_MASTER_KEY не задан в .env.secrets."
    exit 1
fi
if [ -z "${DATABASE_URL:-}" ]; then
    echo "Ошибка: DATABASE_URL не задан в .env.secrets (нужен Postgres для виртуальных ключей)."
    exit 1
fi

if [ ! -d .venv-litellm ]; then
    echo "Создаю виртуальное окружение .venv-litellm и устанавливаю litellm[proxy]..."
    python3.12 -m venv .venv-litellm
    .venv-litellm/bin/pip install --upgrade pip
    .venv-litellm/bin/pip install 'litellm[proxy]'
fi

echo "LiteLLM запускается на http://127.0.0.1:4000 (config: litellm.yaml)"
exec .venv-litellm/bin/litellm --config litellm.yaml --host 127.0.0.1 --port 4000
