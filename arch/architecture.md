# ZabKiss — Architecture

## Overview

ZabKiss is a smart home automation gateway: it receives natural language commands from various voice assistants and chat bots (webhooks), routes them through a policy-gated LLM, and executes validated actions on Home Assistant.

The backend is a Go application using [goscade](https://github.com/ognick/goscade) for component lifecycle management and graceful shutdown.

---

## Layered Architecture

```
┌────────────────────────────────────────────────────────┐
│                    HTTP Layer                          │
│  ┌─────────────────────────────────────────────────┐   │
│  │  ingress/http/                                │   │
│  │  ┌──────────────┐  ┌──────────────┐            │   │
│  │  │  adapters/   │  │  middleware/ │            │   │
│  │  │  alice/      │  │  auth, log  │            │   │
│  │  └──────┬───────┘  └─────────────┘            │   │
│  └─────────┼───────────────────────────────────────┘  │
└────────────┼───────────────────────────────────────────┘
                      │ calls
┌─────────────────────▼──────────────────────────────────┐
│                 Service Layer  (core)                  │
│  ┌─────────────────────────────────────────────────┐   │
│  │  service/                                       │   │
│  │  command.go   — ProcessCommand(ctx, req) resp   │   │
│  │  policy.go    — fetch & cache policy            │   │
│  └────────┬──────────────────────────────────┬─────┘   │
│           │                                  │         │
└───────────┼──────────────────────────────────┼─────────┘
            │ uses (interfaces only)           │
     ┌──────▼──────┐  ┌──────────┐  ┌─────────▼──────┐
     │   LLM Layer │  │  HA Layer│  │  Repo Layer     │
     │  llm/       │  │  ha/     │  │  repository/    │
     │  client.go  │  │  client  │  │  state.go       │
     │  openai/    │  │  service │  │  context.go     │
     │  ...        │  │  calls   │  │  sqlite/        │
     └─────────────┘  └──────────┘  └─────────────────┘
```

### Dependency direction

```
handler → service → { llm, ha, repository }
```

Service defines **interfaces**; concrete adapters live in their own packages and are wired at startup (in `cmd/server/main.go`).

---

## Package Layout

```
addon/backend/
├── cmd/
│   └── server/
│       └── main.go          # Wire everything, start goscade lifecycle
│
├── pkg/                     # Shared across the whole project
│   ├── httpserver/
│   │   └── server.go        # http.Server wrapped as goscade Component
│   ├── sqlitedb/
│   │   └── db.go            # SQLite connection wrapped as goscade Component
│   └── logger/
│       └── logger.go        # Logger interface — used project-wide, defined here once
│
├── internal/
│   │
│   ├── domain/              # Pure domain types — no serialization tags, no framework imports
│   │   ├── command.go       # CommandRequest, CommandResponse, Turn
│   │   ├── policy.go        # Policy, Device, AllowedService, ParamConstraint
│   │   └── device.go        # DeviceState and other device-centric types
│   │
│   ├── service/             # Core business logic — no framework imports
│   │   ├── command.go       # ProcessCommand: orchestrates policy → LLM → validate → HA
│   │   ├── policy.go        # Policy fetch, cache, TTL enforcement
│   │   └── validator.go     # Hard security gate: validate LLM actions against domain.Policy
│   │
│   ├── http/                # HTTP entry points — one subdir per vendor / concern
│   │   ├── alice/           # Yandex Alice entry point — no interface, wired directly in main
│   │   │   └── handler.go   # Handler + Register(r chi.Router) + private DTOs + mapping to domain
│   │   └── auth/            # OAuth token exchange
│   │       └── handler.go   # POST /auth/token — exchange Yandex OAuth code for token
│   │
│   ├── llm/                 # LLM adapters
│   │   ├── llm.go           # LLMClient interface
│   │   └── openai/
│   │       ├── client.go    # OpenAI-compatible HTTP client + private request/response DTOs
│   │       └── prompt.go    # System prompt builder from policy
│   │
│   ├── actuator/            # Actuator port — fetch available commands, execute them
│   │   ├── actuator.go      # ActuatorClient interface
│   │   └── homeassistant/
│   │       └── client.go    # HA REST API implementation + private request/response DTOs
│   │
│   ├── repository/          # State & context persistence
│   │   ├── repository.go    # StateRepo, ContextRepo, PolicyStore interfaces
│   │   └── sqlite/
│   │       ├── state.go     # Device state cache + private row structs
│   │       └── context.go   # Conversation context per user + private row structs
│   │
│   └── config/
│       └── config.go        # Load /data/options.json
```

---

## Core Data Flow

```
POST /webhook/alice
  │
  ▼
alice.Handler
  1. Verify HMAC-SHA256 signature          (alice/auth.go)
  2. Parse Alice request JSON              (alice/types.go)
  3. Map to domain CommandRequest          (alice/handler.go)
  │
  ▼
service.ProcessCommand(ctx, req)
  4. GetPolicy() — cached, TTL 60 s        (service/policy.go)
  5. repo.GetContext(userID)               — conversation history
  6. llm.Execute(systemPrompt, userText)   — only enabled devices in prompt
  7. policy.Validate(policy, actions)      — hard security boundary
  8. ha.CallService() for each action      — execute validated actions
  9. repo.SaveContext(userID, turn)        — persist conversation turn
  │
  ▼
alice.Handler
  10. Map domain Response → Alice JSON
  11. Write HTTP 200
```

---

## goscade Component Wiring

`main.go` creates components and registers them with the goscade lifecycle.

goscade wrappers (`pkg/httpserver`, `pkg/sqlitedb`) implement the `goscade.Component` interface and are the only packages allowed to depend on the goscade library — keeping the framework out of `internal/`.

```
lc := goscade.NewLifecycle(logger, goscade.WithShutdownHook())

db      := sqlitedb.New(cfg.DBPath)           // pkg/sqlitedb — goscade Component
httpSrv := httpserver.New(cfg.Addr, router)   // pkg/httpserver — goscade Component

lc.Register(db)
lc.Register(httpSrv, db) // httpSrv depends on db

goscade.Run(ctx, lc, func() {
    slog.Info("ZabKiss ready", "addr", cfg.Addr)
})
```

Components start in dependency order and shut down in reverse order (graceful drain).

---

## Security Boundaries

| Boundary | Where | Rule |
|---|---|---|
| Alice signature | `alice/auth.go` | Reject if HMAC mismatch |
| Policy fetch failure | `service/policy.go` | Reject all commands — no stale cache |
| LLM output validation | `service/validator.go` | entity_id in whitelist + enabled, service in allowed_services, params within constraints |
| Secrets | `config/config.go` | Only from `/data/options.json`, never hardcoded |

---

## Adding a New Voice Assistant

Adding a new voice assistant (e.g. Google Home, SberSalut):

1. Create `internal/http/<name>/`
2. Implement `handler.go` with `Register(r chi.Router)` — auth, parse, map to domain types
3. Call `<name>.New(...).Register(r)` in `cmd/zabkiss/main.go`

No changes to `service/` or any other layer.

---

## Interface Placement

Interfaces are defined **at the point of consumption**, not at the point of implementation. This is the standard Go convention and enforces Dependency Inversion without artificial indirection.

| Interface | Defined in | Implemented in |
|-----------|-----------|----------------|
| `LLMClient` | `internal/llm/llm.go` | `internal/llm/openai/` |
| `ActuatorClient` | `internal/actuator/actuator.go` | `internal/actuator/homeassistant/` |
| `PolicyStore` | `internal/repository/repository.go` | `internal/actuator/homeassistant/` |
| `StateRepo` | `internal/repository/repository.go` | `internal/repository/sqlite/` |
| `ContextRepo` | `internal/repository/repository.go` | `internal/repository/sqlite/` |
| `Logger` | `pkg/logger/logger.go` | `log/slog` adapter |

**Exception — `pkg/logger`:** the Logger interface is used across every layer (service, adapters, http middleware). Defining it in any one package would create an import cycle or force unnecessary coupling. It lives in `pkg/` as a shared contract.

```
// internal/service/types.go — interfaces owned by the consumer
type LLMClient interface {
    Execute(ctx context.Context, systemPrompt, userText string) (LLMResponse, error)
}

type StateRepo interface {
    GetState(ctx context.Context, entityID string) (map[string]any, error)
    SaveState(ctx context.Context, entityID string, state map[string]any) error
}

// pkg/logger/logger.go — shared project-wide
type Logger interface {
    Info(msg string, args ...any)
    Error(msg string, args ...any)
    Debug(msg string, args ...any)
}
```

---

## Design Principles

### Clean Architecture & SOLID

The codebase follows Clean Architecture and SOLID principles throughout:

- **Dependency Inversion** — `service/` never imports concrete adapters. Port interfaces live at the root of their own package (`llm/client.go`, `actuator/client.go`, `repository/repository.go`) and are injected at construction time.
- **Interface Segregation** — each interface is narrow and role-specific; adapters implement only what the service needs.
- **Private DTOs** — data transfer structs (JSON shapes, DB row structs) are unexported and defined in the same file as the code that uses them (handler, repo, client). They never leak across package boundaries. Only domain types in `internal/domain/` are public.
- **`internal/domain/`** — pure domain structs split by subdomain (no serialization tags, no framework imports). This is the shared language between `service/` and surrounding layers (adapters, repos, LLM client). Policy structs also live here instead of in `policy/`.
- **Single Responsibility** — `service/command.go` orchestrates the flow; validation, LLM calls, HA calls, and persistence are each delegated to a single focused collaborator.
- **Open/Closed** — adding a new voice assistant or LLM provider requires no changes to `service/`; extend by adding a new adapter and wiring it in `main.go`.

### Dependency Injection

All dependencies are wired **exclusively in `cmd/server/main.go`**. No package constructs its own collaborators. The pattern is:

```go
// main.go — the only place where concrete types are instantiated
db       := sqlite.New(cfg.DBPath)
haClient := harest.New(cfg.HAUrl, cfg.HAToken)
llmClient := openai.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
policyStore := policyfetch.New(haClient)

svc := service.New(llmClient, haClient, policyStore, db, db)

alice.NewHandler(svc, cfg.AliceWebhookSecret).Register(mux)
```

Inner packages (`service`, `llm/openai`, `ha/rest`, etc.) expose constructors that accept interfaces — they have zero knowledge of each other.

---

## Tech Stack

| Concern | Choice | Reason |
|---|---|---|
| Go | 1.26 | Green Tea GC, errors.AsType, slog.NewMultiHandler |
| Lifecycle / DI | [goscade](https://github.com/ognick/goscade) | Graceful start/stop ordering |
| HTTP router | `net/http` + `chi` | Lightweight, idiomatic |
| LLM | OpenAI-compatible REST | Works with OpenAI, local models, etc. |
| DB | SQLite (via `modernc.org/sqlite`) | Zero external deps, embedded |
| Logging | `log/slog` | Standard library, structured |
| Config | JSON unmarshal from `/data/options.json` | HA Supervisor convention |
