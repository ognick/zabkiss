<p align="center">
  <img src="docs/logo.png" alt="ZabKiss" width="200" />
</p>

<h1 align="center">ZabKiss</h1>

<p align="center">
  LLM-powered natural language control for Home Assistant.<br/>
  An AI-first project — designed, architected, and implemented with AI.
</p>

---

ZabKiss lets you control your smart home with natural language. Send a text command from any source — voice assistant, chat bot, mobile app, or HTTP client — and ZabKiss routes it through a policy-gated LLM that translates it into structured device actions, without ever exposing your full Home Assistant instance to the model.

**How it works:**

1. Any client sends a text command to the webhook
2. A Go backend fetches your device whitelist (policy) from Home Assistant
3. The LLM receives only the whitelisted devices and returns a structured JSON command
4. The backend validates the command against the policy and calls the Home Assistant API

**Key properties:**

- The LLM never sees your full Home Assistant — only devices you explicitly allow
- Final safety is enforced server-side, not by the prompt
- All secrets are runtime-only via Home Assistant add-on options, never stored in the image or repository
- Manage your device whitelist, allowed actions, and parameter constraints from a built-in Home Assistant UI panel

## Components

| Component | Description |
|-----------|-------------|
| **HA Custom Add-on** | Go + Gin server — webhook receiver, LLM orchestrator, validator |
| **HA Custom Integration** | Python — policy storage, REST API, Lovelace UI panel |

## Requirements

- Home Assistant OS on any `amd64` or `aarch64` host
- Any HTTP client capable of sending a POST request with a text payload
- An OpenAI-compatible LLM API key
