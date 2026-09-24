# LLM providers for the OpenAI-compatible chat adapter

Researched 2026-09-24. Question: can one OpenAI-compatible `POST {base}/chat/completions` adapter serve Mistral, Scaleway Generative APIs and Ollama behind `LLM.Chat(system, user)`? What does each provider need?

**Short answer:** yes. All three accept the same request (`model`, `messages`, `temperature`, `max_tokens`, `stream`, `response_format: {"type":"json_object"}`) and return `choices[0].message.content`. The adapter only has to vary the base URL, the key (optional for Ollama), the model, the timeout, and how it classifies errors for retries.

## Comparison

| | Mistral | Scaleway Generative APIs | Ollama (local) |
|---|---|---|---|
| Base URL | `https://api.mistral.ai/v1` [M1] | `https://api.scaleway.ai/v1` (default Project) or `https://api.scaleway.ai/{project_id}/v1` [S1][S5] | `http://localhost:11434/v1` [O1]; server binds `127.0.0.1:11434` by default, change with `OLLAMA_HOST` [O2][O3] |
| Endpoint | `/v1/chat/completions` [M1] | `/v1/chat/completions` [S2] | `/v1/chat/completions` [O1] |
| Auth | `Authorization: Bearer <MISTRAL_API_KEY>` (current code) | `Authorization: Bearer <SCW_SECRET_KEY>`, the secret key of an IAM API key; the IAM principal needs `GenerativeApisFullAccess` or narrower [S2][S5] | None. OpenAI clients need a non-empty key, so docs use `ollama`, which is "required but ignored" [O1] |
| JSON mode | `response_format: {"type":"json_object"}`; `json_schema` also supported [M1][M2] | `json_object` and `json_schema` supported [S2][S3] | `json_object` maps to native `format: "json"`; `json_schema` maps to `format: <schema>` [O4] |
| `temperature` | Default varies by model; recommended range 0.0–0.7 [M1] | Supported. For JSON, docs advise < 0.6 (llama-3.3-70b) and never 0 (token loops) [S6] | Supported. **Defaults to 1.0 if omitted** [O4] |
| `max_tokens` | Supported [M1] | Supported. Too low gives truncated, invalid JSON [S6] | Mapped to `num_predict` [O4] |
| `stream` | Default `false` [M1] | Supported, off unless set [S2] | Default `false` [O6] |
| Unsupported params | none relevant | `frequency_penalty`, `n`, `top_logprobs`, `logit_bias`, `user` [S2] | `logprobs`, `tool_choice`, `logit_bias`, `user`, `n` [O1] |
| Error body | 422 returns a `detail` validation body [M1]; other shapes not documented (current code reads `error.message`) | `{"error":{"message","type","param","code"}}`; legacy models use a flat `{"object":"error","message",...}` [S4] | `{"error":{"message","type","param","code"}}`, HTTP status preserved [O4][O6] |
| 429 | Exceeded RPS, tokens/min, tokens/month, **or workspace spending cap** (the last lasts until the next billing cycle) [M3] | 429 "Too Many Requests" (RPM) or "Too Many Tokens" (TPM) [S4] | Not rate limited. Returns **503** when the queue (`OLLAMA_MAX_QUEUE`, default 512) is full [O2][O3] |
| Other errors | — | 400, 401 (missing header), 403 (bad key/permission), 404, 422 (model missing/unknown), 500, 504 (gateway timeout) [S4][S6] | 404 `model "x" not found, try pulling it first`; **no auto-pull** [O5] |
| Rate limits | Per model, per organisation, by tier. Numbers are only in Admin panel > Limits. `X-RateLimit-Remaining` header [M3] | Per organisation, shared by all Projects. Payment validated / identity validated: 300 / 600 RPM; 200k / 400k–2M TPM depending on model; 100 concurrent [S7][S8]. `x-ratelimit-*` headers on every response [S7] | `OLLAMA_NUM_PARALLEL` default 1 per model [O2][O3] |
| Latency notes | Hosted | Hosted. 504 on long requests; smaller input or `max_tokens` helps [S6] | Cold start: model loads on first request and unloads after `OLLAMA_KEEP_ALIVE` (default 5m). A load may stall up to `OLLAMA_LOAD_TIMEOUT` (default 5m) before failing [O2][O3] |
| Data residency | Not researched | Serverless models run in Paris, France (OPCORE). Prompts and outputs are not stored or used for training, except traffic linked to errors, kept up to 2 weeks. GDPR, "not subject to ... the American Cloud Act" [S5][S9] | Local machine |

## Notes

**Scaleway**
- Suitable models (serverless, structured output "Yes") [S3]: `mistral-small-3.2-24b-instruct-2506` (128k ctx, 32k out), `mistral-medium-3.5-128b`, `gpt-oss-120b`, `llama-3.3-70b-instruct`, `gemma-4-26b-a4b-it`, `qwen3.6-35b-a3b`.
- **Recommended default: `mistral-small-3.2-24b-instruct-2506`.** It is small and fast for short JSON replies, supports structured output, and has the highest verified-tier TPM (2M) [S3][S8].
- Trap: the quickstart and how-to examples still use `llama-3.1-8b-instruct`, but the supported-models table lists it as **EOL since 25 May 2025** [S1][S3]. `mistral-nemo-instruct-2407` (EOL 16 Apr 2026) and `gemma-3-27b-it` (EOL 1 Aug 2026) are also retired.
- Project scoping: a key with no project in the URL uses the default Project. Put `{project_id}` in the base URL to bill a specific Project [S5][S6]. Treat it as part of `LLM_BASE_URL`, not a separate field.
- Scaleway recommends `json_schema` over `json_object` for reliability. With `json_object`, the prompt must ask for (short) JSON to avoid runaway generations [S3].

**Ollama**
- Model names are `name:tag`, e.g. `llama3.1:8b`. `llama3.1` alone means `:latest`, which is the 8B build (4.9 GB) [O7]. The model must be pulled first (`ollama pull llama3.1:8b`), otherwise you get a 404 [O1][O5].
- Default context is 4096 tokens per the FAQ [O2]. The current `envconfig` says 4k/32k/256k depending on VRAM [O3]. This can only be changed via `OLLAMA_CONTEXT_LENGTH` or a Modelfile `num_ctx`; the OpenAI endpoint has no field for it [O1]. Fine for short prompts.
- **Always send `temperature`**, because the compat layer otherwise uses 1.0 [O4].
- Cold starts: pre-warm with an empty request to `/api/chat` or `ollama run <model> ""` [O2]. Use a client timeout well above the hosted ones (minutes on CPU-only machines). This is an inference from the 5m load/keep-alive defaults, not a documented number.

**Mistral**
- `/v1/chat/completions` is the native API and already matches the OpenAI shape [M1]. `response_format: {"type":"json_object"}` is supported. The system or user message **must** mention JSON, "otherwise the model may produce an infinite whitespace stream" [M2][M4]. The app's prompts already do this.
- Keep `mistral-medium-latest` (current value) as the default.

## What the single adapter must handle

1. **Request:** always send `model`, `messages`, `temperature`, `max_tokens`, and `stream: false` (explicit, harmless). Send `response_format: {"type":"json_object"}` when JSON mode is on. All three accept `json_object`, so one flag per provider is enough (default on).
2. **Auth:** set `Authorization: Bearer <key>` only if a key is configured. Ollama ignores it [O1].
3. **Response:** parse `choices[0].message.content`. Ollama types `content` as `any` [O4], but for text replies it is a string. Consider checking `finish_reason == "length"` to report truncated JSON clearly [S6].
4. **Errors:** on non-2xx, extract `error.message`, else top-level `message` (Scaleway legacy), else `detail` (Mistral 422), else the raw body.
5. **Retries:** retry on network errors, 429, 500, 502, 503 (Ollama overload), 504. **Do not retry** 400/401/403/404/422 (bad key, unpulled or unknown model, bad payload). Note that the current `Chat` retries every error. Mistral's 429 can also mean a spending cap that lasts until the next billing cycle [M3], so cap the attempts (3 is fine). Honouring a `Retry-After` header is a defensive extra; none of the sources document one.
6. **Timeout per provider**, via `http.Client.Timeout`. Ollama needs a much longer one.

## Recommended configuration

A single selector plus optional overrides. Per-provider defaults live in code.

| Env var | mistral | scaleway | ollama |
|---|---|---|---|
| `LLM_PROVIDER` | `mistral` | `scaleway` | `ollama` |
| `LLM_BASE_URL` | `https://api.mistral.ai/v1` | `https://api.scaleway.ai/v1` (or `.../{project_id}/v1`) | `http://localhost:11434/v1` |
| `LLM_API_KEY` | required (fall back to existing `MISTRAL_API_KEY`) | required (`SCW_SECRET_KEY`) | not needed |
| `LLM_MODEL` | `mistral-medium-latest` | `mistral-small-3.2-24b-instruct-2506` | `llama3.1:8b` |
| `LLM_TIMEOUT` | `30s` | `30s` | `180s` |
| `LLM_JSON_MODE` | `true` | `true` | `true` |

Keep temperature 0.7 and max_tokens 800 as shared constants. 0.7 is at the top of Mistral's recommended range [M1]. Scaleway's JSON guidance asks for < 0.6 on llama-3.3-70b [S6], so lower it (e.g. 0.5) if JSON glitches appear on Scaleway. The 30s and 180s timeouts are judgement calls, not documented values.

## Sources

- [S1] Scaleway, Generative APIs quickstart: https://www.scaleway.com/en/docs/generative-apis/quickstart/ (source: https://github.com/scaleway/docs-content/blob/main/pages/generative-apis/quickstart.mdx)
- [S2] Scaleway, Using Chat API: https://www.scaleway.com/en/docs/generative-apis/api-cli/using-chat-api/
- [S3] Scaleway, Supported models: https://www.scaleway.com/en/docs/generative-apis/reference-content/supported-models/ ; Structured outputs: https://www.scaleway.com/en/docs/generative-apis/how-to/use-structured-outputs/
- [S4] Scaleway, Understanding errors: https://www.scaleway.com/en/docs/generative-apis/api-cli/understanding-errors/
- [S5] Scaleway, Generative APIs FAQ: https://www.scaleway.com/en/docs/generative-apis/faq/
- [S6] Scaleway, Fixing common issues: https://www.scaleway.com/en/docs/generative-apis/troubleshooting/fixing-common-issues/
- [S7] Scaleway, Rate limits: https://www.scaleway.com/en/docs/generative-apis/reference-content/rate-limits/
- [S8] Scaleway, Organization quotas (Generative APIs section): https://github.com/scaleway/docs-content/blob/main/pages/organizations-and-projects/organization/organization-quotas.mdx
- [S9] Scaleway, Generative APIs privacy policy: https://github.com/scaleway/docs-content/blob/main/pages/generative-apis/reference-content/data-privacy.mdx
- [O1] Ollama, OpenAI compatibility: https://docs.ollama.com/api/openai-compatibility
- [O2] Ollama, FAQ: https://docs.ollama.com/faq
- [O3] Ollama source, `envconfig/config.go`: https://github.com/ollama/ollama/blob/main/envconfig/config.go
- [O4] Ollama source, `openai/openai.go` (error type, `response_format` → `format`, `max_tokens` → `num_predict`, temperature default 1.0): https://github.com/ollama/ollama/blob/main/openai/openai.go
- [O5] Ollama source, `server/routes.go` (404 "try pulling it first"): https://github.com/ollama/ollama/blob/main/server/routes.go
- [O6] Ollama source, `middleware/openai.go` (status preserved, stream false): https://github.com/ollama/ollama/blob/main/middleware/openai.go
- [O7] Ollama library, llama3.1 tags: https://ollama.com/library/llama3.1
- [M1] Mistral, Chat Completion API reference: https://docs.mistral.ai/api/endpoint/chat
- [M2] Mistral, JSON mode: https://docs.mistral.ai/studio/conversations/structured-output/json_mode
- [M3] Mistral, rate limits: https://help.mistral.ai/en/articles/698531-why-am-i-hitting-api-rate-limits-and-how-do-i-increase-them ; https://docs.mistral.ai/admin/billing-usage/usage-limits
- [M4] Mistral, Known limitations: https://docs.mistral.ai/resources/known-limitations
