#!/usr/bin/env sh
set -e

OPTIONS=/data/options.json

if [ -f "$OPTIONS" ]; then
    LOG_LEVEL=$(jq -r '.log_level // "info"' "$OPTIONS")
    OPENAI_API_KEY=$(jq -r '.openai_api_key // ""' "$OPTIONS")
    ALLOWED_EMAILS=$(jq -r '.allowed_emails // [] | join(",")' "$OPTIONS")
else
    LOG_LEVEL="${LOG_LEVEL:-info}"
    OPENAI_API_KEY="${OPENAI_API_KEY:-}"
    ALLOWED_EMAILS="${ALLOWED_EMAILS:-}"
fi

export ADDR=":8080"
export DB_PATH="/data/zabkiss.db"
export LOG_LEVEL
export OPENAI_API_KEY
export ALLOWED_EMAILS

exec /usr/bin/zabkiss
