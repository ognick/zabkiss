# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ZabKiss is a Home Assistant automation system that accepts natural language commands from any source (HTTP client, voice assistant, chat bot, mobile app) and routes them through a policy-gated LLM to control smart home devices. It consists of two HA components:

1. **HA Custom Integration** (`custom_components/zabkiss/`) — Python, manages policy/whitelist, provides UI panel, exposes REST API for the add-on
2. **HA Custom Add-on** (`addon/`) — Go + Gin HTTP server, handles Alice webhooks, orchestrates LLM calls, validates responses, calls HA API

## Repository Structure

```
addon/
  backend/              # Go + Gin server
    cmd/server/         # main.go entrypoint
    internal/
      alice/            # Webhook handler + HMAC auth
      llm/              # OpenAI-compatible client + prompt builder
      policy/           # Policy fetcher (from integration) + validator
      ha/               # HA REST API client
      config/           # Runtime config loader from /data/options.json
    Dockerfile          # distroless/static runtime, CGO_ENABLED=0
  config.yaml           # HA Supervisor add-on manifest + options schema
custom_components/zabkiss/
  __init__.py         # Setup: registers REST API view + frontend panel
  api.py              # GET /api/zabkiss/policy (requires_auth=True)
  storage.py          # HA Storage helper wrapper for policy persistence
  config_flow.py      # HA config entry
  panel.py            # Lovelace panel registration
  frontend/           # Lit element UI for policy management
    zabkiss-panel.js  # Built artifact committed to repo
hacs.json             # HACS integration descriptor
.github/workflows/
  ci.yml                # Build + test on PR
  release.yml           # Tag → build multi-arch → push GHCR
repository.json         # HA add-on repository descriptor
```

## Add-on Commands (Go)

```bash
cd addon/backend

# Run
go run ./cmd/server

# Build
go build -o bin/zabkiss ./cmd/server

# Test
go test ./...

# Test single package
go test ./internal/policy/...

# Lint (requires golangci-lint)
golangci-lint run ./...

# Build Docker image locally
docker build -t zabkiss-addon:dev .

# Build for arm64
docker buildx build --platform linux/arm64 -t zabkiss-addon:dev .
```

## Integration Commands (Python)

```bash
cd integration

# Lint
ruff check custom_components/zabkiss/
mypy custom_components/zabkiss/

# Test
pytest tests/

# Build frontend panel
cd custom_components/zabkiss/frontend
npm install
npm run build  # outputs to dist/zabkiss-panel.js, commit this file
```

## Architecture: Critical Data Flow

```
Client POST /webhook
  → HMAC-SHA256 verify (webhook_secret)
  → policy.Client.GetPolicy() — cached, TTL 60s
      → GET http://homeassistant:8123/api/zabkiss/policy
        Authorization: Bearer <ha_token from options>
  → llm.BuildSystemPrompt(policy) — only enabled devices exposed to LLM
  → llm.Client.Execute(prompt, userText)
  → policy.Validate(policy, llmResponse.Actions)  ← hard security boundary
  → ha.Client.CallService() for each validated action
  → Alice response JSON
```

## Architecture: Key Constraints

**Security boundary** — `policy.Validate()` in `internal/policy/validator.go` is the hard gate. LLM output is untrusted. Validation must check:
- `entity_id` exists in whitelist and `enabled: true`
- `service` is in `allowed_services` for that device
- All params exist in `allowed_params` and satisfy min/max/enum constraints
- No extra params beyond those in policy

**Secrets** — Never in code, Dockerfile, or repo. All come from `/data/options.json` at runtime (HA Supervisor injects add-on options there). Config loaded in `internal/config/config.go`. The HA token passed in options is a long-lived HA token, not the Supervisor token.

**Policy API** — Integration's `api.py` registers `/api/zabkiss/policy` with `requires_auth = True`. Add-on authenticates with the HA token from its options. If policy fetch fails, the add-on must reject all commands — no fallback to stale cache without TTL.

**LLM prompt** — Only `enabled: true` devices appear in the system prompt. LLM never sees full HA state. `internal/llm/prompt.go` builds the system prompt from policy.

## Policy Data Model

The policy JSON schema is in `docs/policy-schema.md`. Key shape:

```json
{
  "version": 2,
  "global": { "max_actions_per_request": 3, "require_confirmation_for_risk": ["high", "critical"] },
  "devices": [{
    "entity_id": "light.living_room",
    "alias": "гостиная свет",
    "enabled": true,
    "risk_level": "low",
    "requires_confirmation": false,
    "allowed_services": [{
      "service": "light.turn_on",
      "allowed_params": {
        "brightness": { "type": "integer", "min": 1, "max": 255, "optional": true }
      }
    }]
  }]
}
```

## LLM Response Schema

The add-on expects this exact shape from the LLM:

```json
{
  "status": "ok" | "reject" | "clarify",
  "reason": "string",
  "actions": [{ "target_id": "entity_id", "service": "domain.service", "data": {} }],
  "reply": "string (human-readable, same language as user)"
}
```

`actions` must be `[]` when status is `reject` or `clarify`.

## Add-on Config Schema

Defined in `addon/config.yaml`. Options injected as `/data/options.json`:

| Key | Type | Notes |
|-----|------|-------|
| `llm_api_key` | str | Required. OpenAI-compatible key |
| `llm_base_url` | str | Default: `https://api.openai.com/v1` |
| `llm_model` | str | e.g. `gpt-4o-mini` |
| `ha_token` | str | Required. Long-lived HA token |
| `ha_url` | str | Default: `http://homeassistant:8123` |
| `alice_webhook_secret` | str | Required. HMAC secret shared with the voice assistant skill |
| `policy_cache_ttl_seconds` | int | Default: 60 |

## Deployment

Images are published to `ghcr.io/{owner}/zabkiss/zabkiss-addon/{arch}` on git tag push. Supported arches: `aarch64`, `amd64`. The `release.yml` workflow handles multi-arch build via QEMU + buildx — no secrets are baked into images.
