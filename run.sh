#!/bin/bash
# Запуск сервера с загрузкой локальных настроек.
# Секреты загружаются из .env.secrets, остальные настройки — из .env.

if [ ! -f .env.secrets ]; then
    echo "Ошибка: файл .env.secrets не найден."
    echo "Скопируйте .env.secrets.example в .env.secrets и укажите EXTERNAL_API_URL и API_KEY."
    exit 1
fi

if [ ! -f .env ]; then
    echo "Ошибка: файл .env не найден."
    echo "Скопируйте .env.example в .env и настройте параметры."
    exit 1
fi

set -a
source .env.secrets
source .env
set +a

go run .
