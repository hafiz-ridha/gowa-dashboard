# Go WhatsApp Web Multi-Device + Dashboard

This repo bundles **two independent Go applications**:

1. **`src/`** — gowa core (upstream [`aldinokemal/go-whatsapp-web-multidevice`](https://github.com/aldinokemal/go-whatsapp-web-multidevice)). WhatsApp Web API server with REST + MCP modes. Multi-device via whatsmeow. **Treat as upstream** — minimise changes here to keep `git pull` low-conflict.
2. **`dashboard/`** — companion app (port 8088). Separate Go binary that proxies core's REST API and adds a Vue 3 SPA for multi-device management, message scheduling (one-shot/daily/weekly/monthly/yearly/cron via robfig/cron), and a UI for the AI Reply feature. Talks to core over HTTP. State (schedules + logs) lives in its own SQLite (`modernc.org/sqlite`, pure-Go, no CGO).

Run them together with `docker compose -f docker-compose.full.yml up -d --build`.

## Recent additions (deployment + features beyond upstream)

These are dashboard-overlay additions; `src/` itself stays close to upstream.

- **aaPanel deployment helpers** ([`scripts/aapanel-install-nginx.sh`](scripts/aapanel-install-nginx.sh), [`scripts/aapanel-check.sh`](scripts/aapanel-check.sh), [`docs/aapanel-nginx.conf.example`](docs/aapanel-nginx.conf.example)) — auto-fix two known aaPanel UI bugs that break POST `/api/devices` & broadcast (`proxy_pass http://host:port/` with trailing slash + `proxy_set_header Host http://host:port` instead of `$host`). Install script edits the vhost via awk, validates `nginx -t`, rolls back on failure. Check script does container-up → direct-dashboard probe → public-URL probe (including the POST that catches both nginx bugs) → AI Reply availability ping.
- **AI Reply auto-enable** ([`docker/entrypoint.sh`](docker/entrypoint.sh)) — defaults `AI_REPLY_ENABLED=true` if unset; auto-generates `AI_ENCRYPTION_KEY` (32-byte hex) and persists to `/app/storages/.ai-encryption-key` on first start. Dashboard's [`aiForward`](dashboard/internal/api/handlers.go) detects 404 from `/aireply/*` and returns `code: "AI_REPLY_DISABLED"` so the SPA can show a banner instead of a raw toast.
- **Device delete = full purge** ([`src/usecase/device.go RemoveDevice`](src/usecase/device.go)) — DELETE `/devices/:id` previously only unmapped from `m.devices` + deleted device record, leaving orphan whatsmeow session + chatstorage data. Now calls `manager.PurgeDevice(ctx, deviceID)` which: logout WA session, disconnect client, delete chatstorage, remove from primary store, remove from in-memory map. Aligns with user expectation that "tombol Hapus" = total cleanup.
- **Dashboard ListDevices uses `/devices` (rich)** ([`dashboard/internal/wa/client.go`](dashboard/internal/wa/client.go)) — switched from `/app/devices` (returns `{name, device}` only) to `/devices` (returns `{id, jid, state, phone_number, display_name, created_at}`). The `state` field powers SPA's single-pill UI ("Connected" green when `state === 'logged_in'`, "Disconnected" red otherwise) and accurate `loggedInDeviceCount`. SPA helper `isDeviceConnected(d)` is the single source of truth.
- **Broadcast** ([`dashboard/internal/broadcast/`](dashboard/internal/broadcast/broadcast.go)) — bulk-send worker with random delay, batch break, shuffle, spintax `{a|b|c}` parser, recipient parser (08xxx→62xxx, comma/newline/space-separated, dedupe). Status stored in `broadcasts` + `broadcast_recipients` tables. Orphaned `running` jobs at startup get marked cancelled (state is in-memory only).
- **Auto-delete logs** ([`store.CleanupOldLogs`](dashboard/internal/store/store.go) + `startCleanupLoop` goroutine in [`dashboard/main.go`](dashboard/main.go)) — periodic cleanup of `schedule_logs` and finished `broadcasts` (CASCADE deletes `broadcast_recipients`) older than `DASHBOARD_LOG_RETENTION_DAYS`. Runs every `DASHBOARD_CLEANUP_INTERVAL_HOURS`. Manual trigger: `POST /api/_cleanup?days=N`. UI panel in Riwayat tab.
- **API status badge** — Vue SPA polls `/api/_health/upstream` every 30s. Pill turns green ("API Core Connected" + latency) on success, red on failure. Click to re-check.

## Structure

```
src/                              # ── gowa CORE (upstream-aligned) ──
├── main.go                       # Entry point; embeds views/ via go:embed
├── cmd/                          # Cobra CLI: rest, mcp subcommands + root config
├── config/settings.go            # All config vars (Viper-bound) — includes 6 AI_* gates
├── domains/                      # Contracts only: interfaces + DTOs
│   └── aireply/                  # AI Reply: AIConfig, KBDocument, ReplyLog, IService, etc.
├── infrastructure/
│   ├── whatsapp/                 # WhatsApp protocol layer
│   │   ├── ai_reply.go           # Bridges events → IAIReplyHandler (with LID-norm + detached ctx + presence)
│   │   ├── jid_utils.go          # NormalizeJIDFromLID
│   │   └── event_*.go            # One file per event type, registered in event_handler.go
│   ├── chatstorage/              # SQLite/PostgreSQL persistence (chats, messages)
│   ├── chatwoot/                 # Chatwoot CRM bidirectional sync
│   └── aireply/                  # AI Reply: SQLite repo + sqlite-vec virtual table store
├── usecase/
│   ├── ...                       # Business logic (1:1 with domains)
│   └── aireply/                  # Service orchestrator + chunker, parser, prompt builder,
│                                 # AES-GCM crypto, rate limiter, Anthropic + OpenAI providers
├── validations/                  # ozzo-validation input checks + tests
├── ui/
│   ├── rest/                     # Fiber HTTP handlers + middleware (aireply.go = 10 endpoints)
│   └── mcp/                      # MCP server handlers
├── views/                        # Vue.js 3 components (Semantic UI, plain JS) — includes AI*.js
├── pkg/                          # Shared helpers + error types
├── statics/                      # Runtime media + QR codes
└── storages/                     # Runtime SQLite DBs (chats, messages, ai_config, kb_*)

dashboard/                        # ── DASHBOARD COMPANION (separate binary) ──
├── main.go                       # Fiber app, port 8088, embeds web/ via go:embed
├── internal/
│   ├── api/handlers.go           # /api/* routes (devices, send, schedules, broadcast, aireply proxy)
│   ├── wa/client.go              # Thin HTTP client to core (forwards X-Device-Id)
│   ├── scheduler/scheduler.go    # robfig/cron + time.Timer (one-shot vs recurring)
│   ├── broadcast/broadcast.go    # Worker bulk-send with random delay + batch break + spintax + shuffle
│   ├── store/store.go            # SQLite for schedules + broadcasts + execution logs
│   └── config/config.go          # godotenv loader
└── web/index.html                # Single-file Vue 3 SPA (6 tabs: Devices, Send, Schedules,
                                  # Broadcast, Logs, AI Reply). AI Reply tab proxies to core /aireply/*.
```

## Commands

```bash
# Core (src/)
cd src && go run . rest                   # REST API (port 3000)
cd src && go run . mcp                    # MCP server (port 8080)
cd src && go build -o whatsapp            # Build binary
cd src && go test ./...                   # Run all tests
cd src && go vet ./...                    # Static analysis

# Dashboard (dashboard/)
cd dashboard && go run .                  # Dashboard (port 8088)
cd dashboard && CGO_ENABLED=0 go build -ldflags="-w -s" -o whatsapp-dashboard

# Combined Docker (recommended for local dev)
docker compose -f docker-compose.full.yml up -d --build
docker compose -f docker-compose.full.yml up -d --build dashboard   # rebuild dashboard only
docker compose -f docker-compose.full.yml up -d --build whatsapp_go # rebuild core only
```

## Where to Look

### Core (`src/`)

| Task | Location | Notes |
|------|----------|-------|
| Add message type | `domains/send/`, `usecase/send.go`, `validations/send_validation.go` | 3-file pattern |
| Add API endpoint | `ui/rest/`, `usecase/`, `domains/` | Handler → usecase → domain |
| Add MCP tool | `ui/mcp/` | Mirrors REST; `query.go` for read ops |
| Handle WA event | `infrastructure/whatsapp/event_*.go` | Register in `event_handler.go` switch |
| Add DB migration | `infrastructure/chatstorage/sqlite_repository.go` → `getMigrations()` | Append only — never insert in middle |
| Add Vue component | `views/components/` | Plain JS, no .vue SFC |
| Device management | `infrastructure/whatsapp/device_manager.go` | Central orchestrator |
| Chatwoot integration | `infrastructure/chatwoot/` | `client.go` (API) + `sync.go` (sync) |
| AI Reply config / KB / chat-toggle / logs | `usecase/aireply/service.go`, `infrastructure/aireply/`, `ui/rest/aireply.go` | 10 endpoints under `/aireply/*`, device-scoped via `X-Device-Id` |
| AI Reply apply-to-all (dashboard) | `dashboard/internal/api/handlers.go` `aiApplyConfigToAll` & `aiApplyChatToAll` + `replicateChatSettings` | Fan-out endpoint: list logged-in devices dari core via `WA.ListDevices()`, lalu PUT config / chat-setting ke setiap device. **`aiApplyConfigToAll` juga otomatis replicate chat-settings dari source device → semua target device** (default `with_chats=true`), karena tanpa chat-toggle row, AI Reply silent-skip. **Pre-flight check** untuk empty API key: kalau body api_key kosong + ada device target yang belum punya key tersimpan → return 400 `AI_API_KEY_REQUIRED_FOR_NEW_DEVICES` (mencegah silent fail karena core's `Decrypt([]byte{})` return `("",nil)` tanpa error). UI checkbox "Apply ke semua device terhubung" muncul ketika `loggedInDeviceCount >= 2`. `parseDeviceList()` handle multiple core response shapes (direct array / `{data:[]}` / `{devices:[]}`) defensively. `listLoggedInDeviceIDs` filter berdasar `state` string (core's actual field, bukan `is_logged_in`) dan pakai `id`/`device` (alias yang map key di DeviceManager), bukan `jid` (tidak di-resolve oleh ResolveDevice). |
| AI Reply multi-device health audit | `dashboard/internal/api/handlers.go` `aiMultiDeviceHealth` di `GET /api/aireply/multi-device-health` | Per-device audit: `has_config`, `has_api_key`, `chat_enabled_count`, `status` ("ready"/"no_config"/"no_api_key"/"no_chats"), `hint` actionable. UI panel di Config tab ("Multi-Device Health Check") panggil endpoint ini saat klik tombol "Cek Semua Device". Berguna untuk diagnose kenapa device tertentu silent fail tanpa trial-and-error. |
| AI provider (Claude / OpenAI-compat) | `usecase/aireply/provider_anthropic.go`, `provider_openai.go` | Implements `IAIProvider` |
| AI Reply event bridge | `infrastructure/whatsapp/ai_reply.go` | Runs **before** static auto-reply in `event_message_handler.go`; LID-normalises chat/sender JIDs; uses detached ctx |
| LID resolution | `infrastructure/whatsapp/jid_utils.go` (`NormalizeJIDFromLID`) | Required before any per-chat DB lookup |
| CLI flags / config | `cmd/root.go` | Viper+Cobra, `.env` loading; AI flags wired in `initFlags()` + `initEnvConfig()` |
| Shared helpers | `pkg/utils/whatsapp.go`, `general.go` | JID, media, phone formatting |

### Dashboard (`dashboard/`)

| Task | Location | Notes |
|------|----------|-------|
| Add dashboard endpoint | `internal/api/handlers.go` + `internal/wa/client.go` | Handler proxies via thin client |
| Add dashboard UI tab | `web/index.html` | Single Vue 3 app; add tab in nav + `<div v-if="tab === '...'">`, then data + methods |
| Schedule logic | `internal/scheduler/scheduler.go` | `robfig/cron` for recurring, `time.AfterFunc` for one-shot |
| Schedule DB | `internal/store/store.go` | One file, includes migrations inline |
| Broadcast worker | `internal/broadcast/broadcast.go` | Goroutine per job, random delay + batch break + ctx cancel. ParseRecipients() does normalization (08xxx→62xxx) + dedupe + spintax `{a\|b}` |
| Broadcast DB | `internal/store/store.go` | `broadcasts` (header + anti-spam knobs) + `broadcast_recipients` (per-row status). `MarkOrphanedRunningBroadcastsAsCancelled()` di-call dari `main.go` saat startup |
| Log retention / cleanup | `store.CleanupOldLogs(days)` + `startCleanupLoop()` in `main.go` | Goroutine sleeps 1min on boot then runs every `CleanupIntervalHours`. Single tx delete + optional `VACUUM` when >500 rows. Endpoint `POST /api/_cleanup?days=N` for manual trigger |
| API health probe | `handlers.healthUpstream` at `/api/_health/upstream` | Pings core's `/app/devices` and reports `{ok, latency_ms, checked_at, error}`. Always returns HTTP 200 — `ok` flag is the source of truth. Treats core 400/401 as alive (just needs device id) |
| API status badge UI | `web/index.html` — `checkUpstreamHealth()` + `apiStatusClass`/`Icon`/`Label` computed | Polls `/api/_health/upstream` every 30s. Tooltip shows URL + last-check time + latency + error. Click pill to re-check immediately |
| DB stats / cleanup UI | `web/index.html` — Riwayat tab "Auto-Delete Logs" panel | Calls `/api/_stats` (row counts + retention config) + `/api/_cleanup?days=N` (manual cleanup with override) |
| aaPanel helpers | `scripts/aapanel-install-nginx.sh`, `scripts/aapanel-check.sh`, `docs/aapanel-nginx.conf.example` | Install rewrites the `location` block via awk with FK-safe braces matching; check script does container-up → direct → public probes including POST (catches the trailing-slash + Host bugs) + AI Reply availability |
| Proxy QR image | `handlers.go:qrImage` | Rewrites core's `qr_link` to `/api/qr/:filename` so browser doesn't need direct core access |

## Critical: Device ID vs JID

Two distinct identifiers — confusing them causes silent data bugs:

- **Device ID** (`instance.ID()`): User alias or UUID (e.g., `"my-device"`)
- **JID** (`instance.JID()`): WhatsApp JID (e.g., `"6289605618749@s.whatsapp.net"`)

The `chats`/`messages` tables store device_id as the **JID without device number**:
```go
deviceID := client.Store.ID.ToNonAD().String()  // ✅ "6289605618749@s.whatsapp.net"
// NOT instance.ID()  // ❌ may return alias or "6289605618749:11@s.whatsapp.net"
```

## Critical: AI Reply gating

`HandleIncoming` returns `false` (skips AI, lets static `WHATSAPP_AUTO_REPLY` fire) when:
1. `config.AIReplyEnabled` is false (env: `AI_REPLY_ENABLED`)
2. `deviceID` empty, or `text` empty after trim
3. **No chat-setting row** for this chat — opt-in is **per chat JID**, there is no global "AI for all" toggle

It returns `true` (claims ownership, static auto-reply suppressed) when:
- **Global pause active** ([pause.go IsPaused](src/usecase/aireply/pause.go)) — atomic in-memory flag, set via `POST /aireply/pause`, cleared via `POST /aireply/resume` or container restart. Affects ALL devices + ALL chats.
- **Chat-setting row exists with `enabled=false`** — explicit opt-out, user wants ZERO replies on this chat. (Semantic change: previously this was treated the same as "no row" which let static fire. Fixed in [service.go HandleIncoming](src/usecase/aireply/service.go:55).)
- Rate limit (default 3s/chat) hit — logged as `rate_limited`
- AI processing fired but errored mid-way — log status `error`
- AI processing succeeded (reply sent OR `out_of_scope` template sent)

It returns `false` (skip AI, static may fire) and silently swallows when:
- No `ai_config` row for device

Guardrail (`out_of_scope` template) only fires when there ARE KB chunks but query doesn't match threshold. **Empty KB auto-bypasses guardrail** so users without uploaded docs still get LLM answers (see `service.go` near the guardrailActive check). Pre-LLM `presence("composing")` is fired and refreshed every 10s; `presence("paused")` on send.

## Conventions

- **Clean Architecture**: `domains/` → `usecase/` → `ui/` (never reverse)
- **1:1 mapping**: Each domain has matching usecase, validation, and UI handler
- **Device scoping**: All chat/message DB queries must include `device_id`
- **LID normalization**: WhatsApp sends `@lid` JIDs — call `NormalizeJIDFromLID()` before DB ops
- **Optional booleans**: Use `*bool` for optional filter params (nil = not set)
- **Config priority**: CLI flags > env vars > `.env` file
- **Wrapper pattern**: `IChatStorageRepository` has two wrappers that inject device_id:
  - `infrastructure/whatsapp/chatstorage_wrapper.go` (for event handlers)
  - `infrastructure/chatstorage/device_repository.go` (for usecase layer)

## Anti-Patterns

- **Never** query chats/messages without device_id scoping
- **Never** use raw event JIDs for DB lookups without `NormalizeJIDFromLID` (events arrive as `@lid`; stored toggles/configs use `@s.whatsapp.net` form — silent mismatch is the failure mode)
- **Never** add `IChatStorageRepository` methods without updating both wrapper files
- **Never** insert migrations in the middle — always append to `getMigrations()`
- **Never** put business logic in domain packages — they define contracts only
- **Never** remove the Device == 0 check in receipt forwarding (prevents duplicate webhooks)
- **Never** propagate the whatsmeow event ctx to long work (LLM, embeddings, big DB scans). It expires fast and cancels mid-flight. Use `context.WithTimeout(context.Background(), ...)` — see `ai_reply.go` for the pattern.
- **Never** bind `nil []byte` to NOT NULL secret columns (e.g. `api_key_encrypted`). SQL driver sends NULL → constraint fires **before** ON CONFLICT UPDATE can run its preserve-existing CASE. Normalise `nil` → `[]byte{}` at the repo boundary.
- **Never** trust Fiber 2.52 `c.Params()` to URL-decode path params. `%40` stays as `%40`. Call `url.PathUnescape()` before use, or call core with `@` raw.
- **Never** modify `src/` for things that can live in `dashboard/`. The dashboard is the overlay; `src/` should stay close to upstream to keep `git pull` painless. Net new features that need event-handler hooks (like AI Reply) are the exception.

## Testing

- Standard Go testing + `testify/assert` + `testify/suite`
- Table-driven tests throughout
- `httptest` for HTTP testing, `fiber.Test()` for middleware
- Tests colocated with source: `*_test.go` next to implementation
- Run: `cd src && go test ./...`

## Key Dependencies

| Package | Role |
|---------|------|
| `go.mau.fi/whatsmeow` | WhatsApp Web protocol |
| `github.com/gofiber/fiber/v2` | REST framework (both core & dashboard) |
| `github.com/mark3labs/mcp-go` | MCP server (v0.45.0) |
| `github.com/spf13/cobra` + `viper` | CLI + config |
| `github.com/go-ozzo/ozzo-validation/v4` | Input validation |
| `github.com/mattn/go-sqlite3` / `github.com/lib/pq` | Core: SQLite (CGO) / PostgreSQL |
| `modernc.org/sqlite` | Dashboard: pure-Go SQLite, no CGO needed |
| `asg017/sqlite-vec` (via core SQLite) | Vector search for KB chunks (AI Reply) |
| `github.com/robfig/cron/v3` | Dashboard schedule engine |

## Notes

- **Go 1.25.0+** required (`src/go.mod`)
- REST and MCP modes cannot run simultaneously (whatsmeow limitation)
- FFmpeg required for media processing (`ConvertToJPEG`, `ConvertToMP4`)
- HTML/JS assets embedded in binary via `go:embed` (both core and dashboard)
- Database: core SQLite default, PostgreSQL via `DB_URI`; dashboard always SQLite (`./dashboard/data/dashboard.db` in Docker)
- Docker: multi-stage alpine, non-root users (core `gowauser` uid 20001, dashboard `dashuser` uid 20001)
- CI: GitHub Actions → GoReleaser (Linux/Windows/macOS) + multi-arch Docker (GHCR + Docker Hub)
- Hot reload: `.air.toml` configured (excludes `statics/`, `storages/`)
- `status@broadcast` chat always returns name "Status" regardless of other naming
- **AI Encryption Key**: `AI_ENCRYPTION_KEY` (32-byte hex) is the only way to decrypt API keys stored in `ai_config.api_key_encrypted`. **Treat like a password**; losing it = re-enter all provider API keys. **In Docker (entrypoint.sh), priority is keyfile > env > generate** — `/app/storages/.ai-encryption-key` is authoritative once it exists, because that's what encrypted existing rows. Env var only acts as bootstrap on first boot (copied to keyfile then ignored on subsequent boots). To rotate: stop core, `rm keyfile`, restart — but this orphans all stored provider API keys. Backup the keyfile separately.
- **AI_ENCRYPTION_KEY validation**: entrypoint validates env var format with pure-POSIX `is_valid_hex_key()` (length 64 + `case` glob `*[!0-9a-fA-F]*`). Invalid values are logged + ignored (fall through to keyfile/generate). Dashboard's `aiForward` translates core's `"AI_ENCRYPTION_KEY must be hex"` 500 error to a friendly `code: "AI_ENCRYPTION_KEY_INVALID"` banner.
- **AI Reply default-on in Docker**: `docker/entrypoint.sh` defaults `AI_REPLY_ENABLED=true` if unset, so the dashboard's AI Reply tab works out-of-the-box. To disable, set `AI_REPLY_ENABLED=false` explicitly in `src/.env`. Dashboard handler ([`aiForward`](dashboard/internal/api/handlers.go)) detects core's 404 on `/aireply/*` and returns `{code: "AI_REPLY_DISABLED"}` so the SPA shows a banner with rebuild instructions.
- **Sumopod & similar OpenAI-compat providers** can be slow on cold start — set `AI_REQUEST_TIMEOUT_SEC=60` or higher.
- **Dashboard mirrors core's AI Reply** via `/api/aireply/*` (10 endpoints, all `X-Device-Id` scoped). Settings live in core's DB; dashboard is just a UI proxy. Don't add AI state to dashboard's SQLite.
- **Two distinct `.env` files** — `src/.env` (core) and `dashboard/.env` (dashboard). Both have `.env.example` templates that must stay in sync with the real ones.
- **Dashboard log retention** — `DASHBOARD_LOG_RETENTION_DAYS` (default 30) + `DASHBOARD_CLEANUP_INTERVAL_HOURS` (default 6) drive [`startCleanupLoop`](dashboard/main.go) goroutine. Cleanup deletes `schedule_logs.ran_at < cutoff` and `broadcasts.finished_at < cutoff` in a single tx; `broadcast_recipients` cascade via FK. Running/pending broadcasts are never deleted regardless of age. `VACUUM` only runs if >500 rows deleted in a sweep.
- **aaPanel pitfalls** — Two bugs in aaPanel's "Add reverse proxy" UI that the helper scripts work around:
  1. `proxy_pass http://127.0.0.1:18088/` (trailing slash) → nginx rewrites URI to empty before forwarding, so dashboard sees `POST ` (empty path) → "Cannot POST" 404 from Fiber.
  2. `proxy_set_header Host http://127.0.0.1:18088` → upstream URL stuffed into `Host` header; fasthttp parses malformed Host and routing misses → 404. RFC 7230 §5.4 says Host = `host[:port]` without scheme.
  Both fixed by `scripts/aapanel-install-nginx.sh`. Symptom in the wild: `Cannot use import statement outside a module` in browser (because reverse proxy mistakenly fell through to core, which serves Vue ES modules) + `/api/devices 404`.
