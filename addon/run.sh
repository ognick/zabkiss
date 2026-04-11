#!/usr/bin/env sh
set -e

OPTIONS=/data/options.json

if [ -f "$OPTIONS" ]; then
    LOG_LEVEL=$(jq -r '.log_level // "info"' "$OPTIONS")
    OPENAI_API_KEY=$(jq -r '.openai_api_key // ""' "$OPTIONS")
    LLM_BASE_URL=$(jq -r '.llm_base_url // "https://api.openai.com/v1"' "$OPTIONS")
    LLM_MODEL=$(jq -r '.llm_model // "gpt-4o-mini"' "$OPTIONS")
    ALLOWED_EMAILS=$(jq -r '.allowed_emails // [] | join(",")' "$OPTIONS")
    HA_TOKEN=$(jq -r '.ha_token // ""' "$OPTIONS")
    HA_URL=$(jq -r '.ha_url // "http://homeassistant:8123"' "$OPTIONS")
    POLICY_CACHE_TTL_SECONDS=$(jq -r '.policy_cache_ttl_seconds // 60' "$OPTIONS")
    YOUTUBE_API_KEY=$(jq -r '.youtube_api_key // ""' "$OPTIONS")
else
    LOG_LEVEL="${LOG_LEVEL:-info}"
    OPENAI_API_KEY="${OPENAI_API_KEY:-}"
    LLM_BASE_URL="${LLM_BASE_URL:-https://api.openai.com/v1}"
    LLM_MODEL="${LLM_MODEL:-gpt-4o-mini}"
    ALLOWED_EMAILS="${ALLOWED_EMAILS:-}"
    HA_TOKEN="${HA_TOKEN:-}"
    HA_URL="${HA_URL:-http://homeassistant:8123}"
    POLICY_CACHE_TTL_SECONDS="${POLICY_CACHE_TTL_SECONDS:-60}"
    YOUTUBE_API_KEY="${YOUTUBE_API_KEY:-}"
fi

export ADDR=":8080"
export DB_PATH="/data/zabkiss.db"
export LOG_LEVEL
export OPENAI_API_KEY
export LLM_BASE_URL
export LLM_MODEL
export ALLOWED_EMAILS
export HA_TOKEN
export HA_URL
export POLICY_CACHE_TTL_SECONDS
export YOUTUBE_API_KEY

exec /usr/bin/zabkiss
