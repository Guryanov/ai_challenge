#!/bin/bash
# Создаёт пользователей и виртуальные ключи LiteLLM с лимитами через Admin API.
# Использует LITELLM_MASTER_KEY из .env.secrets (только для админ-операций).
# Результат — виртуальные ключи в litellm-keys.local (не коммитится).
# Требует: запущенный LiteLLM (run-litellm.sh) и jq.
set -euo pipefail

if [ ! -f .env.secrets ]; then
    echo "Ошибка: файл .env.secrets не найден."
    exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "Ошибка: требуется jq (brew install jq)."
    exit 1
fi

set -a
source .env.secrets
set +a

if [ -z "${LITELLM_MASTER_KEY:-}" ]; then
    echo "Ошибка: LITELLM_MASTER_KEY не задан в .env.secrets."
    exit 1
fi

BASE="${LITELLM_BASE_URL:-http://127.0.0.1:4000}"
AUTH=(-H "Authorization: Bearer ${LITELLM_MASTER_KEY}" -H "Content-Type: application/json")

# client_id  budget(USD за 30 дней)  rpm  tpm
CLIENTS=(
  "dev 5 60 400"
  "prod 50 600 2000000"
  "team 20 300 1000000"
)

: > litellm-keys.local
for row in "${CLIENTS[@]}"; do
    read -r id budget rpm tpm <<<"$row"

    # Пользователь с бюджетом.
    curl -sS -X POST "$BASE/user/new" "${AUTH[@]}" \
        -d "{\"user_id\":\"$id\",\"max_budget\":$budget,\"budget_duration\":\"30d\"}" >/dev/null

    # Виртуальный ключ пользователя с лимитами и доступом к моделям.
    key=$(curl -sS -X POST "$BASE/key/generate" "${AUTH[@]}" \
        -d "{\"user_id\":\"$id\",\"key_alias\":\"$id\",\"models\":[\"chat\",\"embed\"],\"max_budget\":$budget,\"budget_duration\":\"30d\",\"rpm_limit\":$rpm,\"tpm_limit\":$tpm}" \
        | jq -r '.key')

    if [ -z "$key" ] || [ "$key" = "null" ]; then
        echo "Ошибка: не удалось создать ключ для клиента $id" >&2
        exit 1
    fi

    echo "$id=$key" >> litellm-keys.local
    echo "Создан клиент $id (budget=$budget/30d, rpm=$rpm, tpm=$tpm)"
done

echo
echo "Виртуальные ключи записаны в litellm-keys.local:"
echo "  dev  — разработка"
echo "  prod — прод"
echo "  team — команда"
echo "Выберите ключ нужного клиента и вставьте его в API_KEY в .env.secrets."
