# GoWA Dashboard

Bundle dua aplikasi Go untuk WhatsApp Web multi-device:

1. **`src/`** — **GoWA Core**. REST/MCP server WhatsApp Web berbasis [`whatsmeow`](https://go.mau.fi/whatsmeow). Ini adalah upstream [`aldinokemal/go-whatsapp-web-multidevice`](https://github.com/aldinokemal/go-whatsapp-web-multidevice) yang **tidak dimodifikasi** sehingga update versi bisa di-pull tanpa konflik.
2. **`dashboard/`** — **WhatsApp Dashboard**. Companion app berdiri sendiri (binary Go terpisah) yang menambahkan UI manajemen device + **penjadwalan pesan** (one-time, harian, mingguan, bulanan, tahunan, cron) + log eksekusi. Berkomunikasi dengan core via REST.

```
┌─────────────────────────┐      HTTP      ┌────────────────────────┐      HTTP      ┌────────────────────────┐
│  Browser (Vue 3 SPA)    │ ─────────────► │  dashboard (8088)      │ ─────────────► │  gowa-core (3000)      │
│  Semantic UI + axios    │                │  - SQLite jadwal & log │                │  - whatsmeow / WA Web  │
└─────────────────────────┘                │  - cron + one-shot     │                │  - REST + MCP          │
                                           └────────────────────────┘                └────────────────────────┘
```

---

## Daftar Isi

- [Fitur](#fitur)
- [Struktur Repo](#struktur-repo)
- [Cara Menjalankan](#cara-menjalankan)
- [Konfigurasi](#konfigurasi)
- [REST API Dashboard](#rest-api-dashboard)
- [Tipe Jadwal](#tipe-jadwal)
- [Catatan Penting](#catatan-penting)
- [Lisensi & Atribusi](#lisensi--atribusi)

---

## Fitur

### Core (`src/`)

Lihat [`how-to-use.md`](./how-to-use.md) untuk dokumentasi lengkap upstream. Ringkasan fitur utama:

- **Multi-device** dalam satu instance — pairing via QR atau kode telepon, scope tiap request pakai header `X-Device-Id` atau query `device_id`.
- **Kirim pesan** semua tipe: text, image, video, audio, file/document, location, link preview, contact, poll, sticker (auto-convert WebP), reaction, edit, revoke, forward.
- **Mention** biasa (`@628xxx`), **ghost mention** (tidak tampil `@` di teks), dan keyword `@everyone` untuk seluruh anggota grup.
- **Group management**: buat grup, invite/kick/promote/demote, ubah subject/description, invite-link, leave.
- **WhatsApp Status / Story** posting.
- **Auto reply**, **auto mark-read**, **auto download media**, **auto reject call** (semua opsional via flag/env).
- **AI Auto-Reply dengan RAG** (per-device, opt-in per chat) — multi-provider: Anthropic (Claude) atau OpenAI-compatible (OpenAI/OpenRouter/Sumopod/DeepSeek/Groq/Ollama). Upload PDF/DOCX/TXT/MD sebagai knowledgebase, di-chunk + di-embed otomatis (sqlite-vec). Style preset (formal/casual/technical/custom), guardrail anti-halusinasi, rate limit per-chat, typing indicator selama LLM generate, audit log lengkap. API key tersimpan terenkripsi AES-GCM via `AI_ENCRYPTION_KEY`.
- **Webhook** outbound dengan HMAC signature dan **event filtering** (`message`, `message.ack`, `message.reaction`, `group.participants`, `call.offer`, dll.).
- **Chatwoot** bidirectional sync (incoming → inbox, outgoing → conversation).
- **MCP server mode** (`./whatsapp mcp`) untuk integrasi dengan AI agent via Model Context Protocol.
- **Storage** SQLite (default) atau PostgreSQL via `DB_URI`.
- **Subpath deployment** (`--base-path=/gowa`), **basic auth multi-user**, **trusted proxies**, **debug mode**.

> REST mode dan MCP mode tidak bisa jalan bersamaan dalam satu proses (keterbatasan whatsmeow).

### Dashboard (`dashboard/`)

UI web (Vue 3 + Semantic UI, di-embed via `go:embed`) dengan enam tab:

| Tab                  | Fungsi                                                                                                          |
|----------------------|-----------------------------------------------------------------------------------------------------------------|
| **Devices**          | List device upstream, tambah device baru, login QR (di-proxy melalui dashboard — core tidak perlu publik), login via kode telepon, logout, reconnect, hapus device. |
| **Kirim Sekarang**   | Form kirim instan: text, image, video, file, audio, location, link. Pilih device & tujuan (nomor / JID grup).   |
| **Jadwal & Reminder**| CRUD jadwal: enable/disable, preview 5 fire-time berikutnya, tombol "Run Now" untuk uji manual, kolom next-run. |
| **Broadcast**        | Kirim ke banyak nomor sekaligus (paste comma/newline/space-separated, auto-normalize 08xxx → 62xxx). Anti-spam: random delay (min/max), batch break tiap N pesan, shuffle order, spintax `{a\|b\|c}` untuk variasi pesan. Live progress + cancel button + per-recipient log. |
| **Riwayat**          | Log eksekusi global + per-jadwal (status sukses/error, response upstream, pesan error).                          |
| **AI Reply**         | 4 sub-section yang nge-proxy ke core: **Config** (provider/model/prompt style/API key dengan masked-key indicator + test connection + **toggle "Apply ke semua device terhubung"** untuk fan-out config ke seluruh nomor logged-in), **Knowledgebase** (upload PDF/DOCX/TXT/MD + list + reindex + delete), **Chat Toggle** (opt-in per chat JID, auto-format nomor `08xxx`/`62xxx` → `@s.whatsapp.net`, **toggle apply-to-all** untuk enable AI di chat yg sama di seluruh device), **Logs** (audit eksekusi dengan filter chat/status). Setting tersimpan di core (per-device, encrypted at rest). |

Kemampuan inti dashboard:

- **Penjadwalan fleksibel** — `once` / `daily` / `weekly` (multi-pilihan hari) / `monthly` / `yearly` / `cron` (5-field [`robfig/cron/v3`](https://github.com/robfig/cron)).
- **Timezone-aware** — tiap jadwal punya zona waktu sendiri (default dari `DASHBOARD_TZ`, mis. `Asia/Jakarta`).
- **Persisten** — SQLite pure-Go (`modernc.org/sqlite`), tidak butuh CGO, binary statis kecil.
- **One-shot survive restart** — schedule type `once` yang terlewat saat downtime tetap di-fire setelah dashboard start ulang (status "missed run").
- **Broadcast dengan anti-spam** — kirim ke banyak nomor dengan random delay (default 8-25 detik), batch break tiap N pesan, shuffle order, dan spintax `{a|b|c}` untuk varian otomatis. Worker async — boleh tutup browser, broadcast tetap jalan. Live progress + per-recipient log + cancel button.
- **Auto-delete logs** — worker background hapus `schedule_logs` & broadcasts selesai yang lebih lama dari `DASHBOARD_LOG_RETENTION_DAYS` (default 30 hari) tiap `DASHBOARD_CLEANUP_INTERVAL_HOURS` (default 6 jam). UI di tab Riwayat juga punya tombol "Cleanup Sekarang" + retention override manual.
- **API Core status badge** — pill berwarna di kanan atas dashboard auto-poll core tiap 30 detik. State: **API Core Connected** (hijau + latency ms), **Mengecek API Core...** (grey), **API Core Disconnected** (merah + error tooltip). Klik untuk re-check.
- **QR proxy** — endpoint `/api/qr/:filename` mem-fetch PNG QR dari core, sehingga browser tidak perlu akses langsung ke port core (cocok di belakang reverse proxy / private network).
- **Basic auth opsional** terpisah dari upstream (`DASHBOARD_BASIC_AUTH=user:pass`).
- **Health probe** `/api/_health` (info build/route) + `/api/_health/upstream` (live ping core).
- **Tidak menyentuh `src/`** — semua state dashboard di `dashboard/data/dashboard.db`, upgrade core cukup `git pull` upstream.

### Helper scripts untuk deployment aaPanel

Dua script di [`scripts/`](./scripts/) yang otomatis benerin problem umum:

- **[`scripts/aapanel-install-nginx.sh`](./scripts/aapanel-install-nginx.sh)** — install/replace blok `location` di nginx vhost aaPanel dengan config yang sudah dijamin benar (tanpa trailing-slash bug + `Host $host` benar). Sekali jalan, backup otomatis, dengan rollback kalau syntax error. Pakai: `sudo sh scripts/aapanel-install-nginx.sh DOMAIN`.
- **[`scripts/aapanel-check.sh`](./scripts/aapanel-check.sh)** — verifikasi end-to-end (container running → dashboard sehat di port 18088 → GET & POST via public URL → AI Reply enabled). Output berwarna dengan instruksi fix yang spesifik kalau ada yang gagal. Pakai: `sh scripts/aapanel-check.sh https://DOMAIN [AUTH=user:pass]`.

Dokumentasi nginx detail: [`docs/aapanel-nginx.conf.example`](./docs/aapanel-nginx.conf.example).

---

## Struktur Repo

```
.
├── src/                        # GoWA Core (upstream, jangan dimodifikasi)
│   ├── cmd/                    # Cobra CLI: rest, mcp subcommand
│   ├── domains/                # Interface + DTO (contract-only)
│   ├── infrastructure/         # whatsmeow, chatstorage, chatwoot
│   ├── usecase/                # Business logic
│   ├── ui/{rest,mcp}/          # Fiber HTTP & MCP handler
│   ├── views/                  # Vue.js 3 (Semantic UI) — UI core
│   └── ...
├── dashboard/                  # Companion app — binary terpisah
│   ├── main.go                 # Fiber app, embed web/
│   ├── internal/
│   │   ├── api/                # REST handler /api/*
│   │   ├── config/             # Env loader (godotenv)
│   │   ├── scheduler/          # robfig/cron + time.Timer
│   │   ├── store/              # SQLite (modernc.org/sqlite)
│   │   └── wa/                 # Client HTTP ke core
│   ├── web/index.html          # SPA Vue 3 (di-embed)
│   ├── Dockerfile              # Multi-stage alpine, non-root uid 20001
│   └── entrypoint.sh
├── docker/golang.Dockerfile    # Image core
├── docker-compose.yml          # Hanya core
├── docker-compose.full.yml     # Core + dashboard
├── docker-compose.aapanel.yml  # Versi untuk aaPanel (bind 127.0.0.1 + reverse proxy)
├── docs/                       # OpenAPI core, dokumentasi webhook & Chatwoot
├── how-to-use.md               # Manual lengkap core
└── readme.md                   # File ini
```

---

## Cara Menjalankan

### Opsi A — Docker Compose (rekomendasi)

```bash
# Build & jalankan core + dashboard sekaligus
docker compose -f docker-compose.full.yml up -d --build
```

- Core   → http://localhost:3000 (UI bawaan untuk pairing & operasi langsung)
- Dashboard → http://localhost:8088

Untuk deploy di aaPanel (port di-bind ke loopback supaya tidak bentrok, expose via Nginx reverse proxy):

```bash
docker compose -f docker-compose.aapanel.yml up -d --build
```

### Opsi B — Lokal tanpa Docker

Butuh **Go 1.25+** dan **FFmpeg** (untuk media core).

Terminal 1 — core:

```bash
cd src
cp .env.example .env        # sesuaikan: APP_PORT, APP_BASIC_AUTH, WHATSAPP_WEBHOOK, dll.
go run . rest               # REST API port 3000
```

Terminal 2 — dashboard:

```bash
cd dashboard
cp .env.example .env        # set WHATSAPP_API_URL, DASHBOARD_TZ, dll.
go mod tidy
go run .                    # http://localhost:8088
```

Build binary single-file:

```bash
# core
cd src && go build -o whatsapp && ./whatsapp rest

# dashboard (pure-Go, no CGO)
cd dashboard && CGO_ENABLED=0 go build -ldflags="-w -s" -o whatsapp-dashboard
```

Windows: ada helper `dashboard/start.bat`.

### Opsi C — MCP mode

```bash
cd src && go run . mcp      # http://localhost:8080
```

REST dan MCP **tidak bisa jalan bersamaan** dalam satu proses.

---

## Konfigurasi

Prioritas: **CLI flag > environment variable > `.env`**.

### Dashboard (`dashboard/.env`)

| Variable                | Default                  | Keterangan                                                                |
|-------------------------|--------------------------|---------------------------------------------------------------------------|
| `DASHBOARD_HOST`        | `0.0.0.0`                | Bind address.                                                             |
| `DASHBOARD_PORT`        | `8088`                   | Port HTTP.                                                                |
| `DASHBOARD_DB`          | `dashboard.db`           | Path file SQLite (dalam Docker default: `/data/dashboard.db`).            |
| `DASHBOARD_TZ`          | `Local`                  | Timezone default jadwal baru (mis. `Asia/Jakarta`).                       |
| `DASHBOARD_BASIC_AUTH`  | (kosong)                 | `user:pass` untuk proteksi UI dashboard. Kosong = terbuka.                |
| `WHATSAPP_API_URL`      | `http://localhost:3000`  | URL core REST API (di Docker: `http://whatsapp_go:3000`).                 |
| `WHATSAPP_API_USER`     | (kosong)                 | Basic auth user untuk core (jika `APP_BASIC_AUTH` di core diaktifkan).    |
| `WHATSAPP_API_PASSWORD` | (kosong)                 | Basic auth password untuk core.                                            |
| `DASHBOARD_LOG_RETENTION_DAYS` | `30`              | Auto-cleanup: hapus `schedule_logs` & broadcasts selesai yang > N hari. `0` = nonaktif. |
| `DASHBOARD_CLEANUP_INTERVAL_HOURS` | `6`           | Interval worker cleanup (jam). Min 1. Default 6 jam.                      |

### Core (`src/.env`)

Variable utama (full list lihat [`how-to-use.md`](./how-to-use.md)):

| Variable                       | Default                                       | Keterangan                                              |
|--------------------------------|-----------------------------------------------|---------------------------------------------------------|
| `APP_PORT` / `APP_HOST`        | `3000` / `0.0.0.0`                            | Port & bind address core.                               |
| `APP_DEBUG`                    | `false`                                       | Logging debug.                                          |
| `APP_OS`                       | `Chrome`                                      | Nama device yang tampil di mobile WhatsApp.             |
| `APP_BASIC_AUTH`               | -                                             | `user1:pass1,user2:pass2`.                              |
| `APP_BASE_PATH`                | -                                             | Subpath deploy (mis. `/gowa`).                          |
| `DB_URI`                       | `file:storages/whatsapp.db?_foreign_keys=on`  | SQLite default; bisa `postgres://...`.                  |
| `WHATSAPP_AUTO_REPLY`          | -                                             | Pesan auto-reply.                                       |
| `WHATSAPP_AUTO_MARK_READ`      | `false`                                       | Otomatis tandai pesan masuk sebagai dibaca.             |
| `WHATSAPP_AUTO_DOWNLOAD_MEDIA` | `true`                                        | Otomatis download media masuk.                          |
| `WHATSAPP_WEBHOOK`             | -                                             | URL webhook (boleh CSV multi-URL).                      |
| `WHATSAPP_WEBHOOK_SECRET`      | `secret`                                      | HMAC secret untuk header signature.                     |
| `WHATSAPP_WEBHOOK_EVENTS`      | -                                             | Filter event (kosong = semua).                          |
| `WHATSAPP_PRESENCE_ON_CONNECT` | `unavailable`                                 | `available` / `unavailable` / `none`.                   |
| `CHATWOOT_ENABLED`             | `false`                                       | Aktifkan Chatwoot sync; butuh `CHATWOOT_URL`, `CHATWOOT_API_TOKEN`, `CHATWOOT_ACCOUNT_ID`, `CHATWOOT_INBOX_ID`, `CHATWOOT_DEVICE_ID`. |
| `AI_REPLY_ENABLED`             | `false`                                       | Feature gate AI auto-reply. Saat `true`, **`AI_ENCRYPTION_KEY` wajib** diisi.        |
| `AI_ENCRYPTION_KEY`            | -                                             | 32-byte hex (64 chars) untuk AES-GCM enkripsi API key di SQLite. Generate: `openssl rand -hex 32`. **Backup terpisah** — kehilangan key = API key tersimpan unreadable. |
| `AI_MAX_KB_FILE_SIZE`          | `10485760`                                    | Max ukuran upload dokumen knowledgebase (bytes, default 10MB).                       |
| `AI_REQUEST_TIMEOUT_SEC`       | `10`                                          | Per-call timeout untuk LLM + embeddings (detik). Untuk Sumopod/provider lambat naikkan ke 30–60. |
| `AI_RATE_LIMIT_SECONDS`        | `3`                                           | Interval minimum (detik) antara reply AI per chat.                                   |
| `AI_VECTOR_DIMENSION`          | `1536`                                        | Dimensi embedding (default cocok untuk `text-embedding-3-small`).                    |

---

## REST API Dashboard

Semua di-prefix `/api`. Endpoint device adalah proxy ke core (otomatis menyisipkan `X-Device-Id`, basic auth, dll.).

| Method   | Path                          | Keterangan                                                  |
|----------|-------------------------------|-------------------------------------------------------------|
| `GET`    | `/api/_health`                | Probe versi build + URL upstream + list endpoint.            |
| `GET`    | `/api/_health/upstream`       | Live ping ke core. Return `{ok, latency_ms, checked_at, upstream_url}`. UI pakai utk badge **API Core Connected**. |
| `GET`    | `/api/_stats`                 | Row counts per tabel + retention config (untuk admin/maintenance UI).   |
| `POST`   | `/api/_cleanup`               | Trigger cleanup manual. Query: `?days=N` override retention. Return `{deleted_*}` per tabel. |
| `GET`    | `/api/devices`                | List semua device.                                           |
| `POST`   | `/api/devices`                | Buat device baru. Body: `{"device_id":"alias"}`.             |
| `DELETE` | `/api/devices/:id`            | Hapus device.                                                |
| `GET`    | `/api/devices/:id/status`     | Status koneksi (connected/loggedIn/dll).                     |
| `GET`    | `/api/devices/:id/login`      | Mulai QR login; balikan `qr_link` sudah di-rewrite ke `/api/qr/...`. |
| `GET`    | `/api/devices/:id/login-code` | Login pakai kode telepon. Query: `phone=628xxx`.             |
| `POST`   | `/api/devices/:id/logout`     | Logout device.                                               |
| `POST`   | `/api/devices/:id/reconnect`  | Reconnect socket.                                            |
| `GET`    | `/api/qr/:filename`           | Proxy gambar QR PNG dari core.                               |
| `POST`   | `/api/send`                   | Kirim pesan sekarang (text/image/video/file/audio/location/link). |
| `GET`    | `/api/broadcast`              | List broadcast (terbaru di atas).                            |
| `POST`   | `/api/broadcast`              | Buat & start broadcast. Body wajib `device_id`, `recipients` (raw string), `message_type`, `message`. Opsional: `delay_min_ms` (min 3000), `delay_max_ms`, `batch_size`, `batch_pause_min_ms`, `batch_pause_max_ms`, `shuffle_order`, `start_now`. |
| `POST`   | `/api/broadcast/preview`      | Parse `recipients` string → return `{valid_count, valid[], invalid_count, invalid[]}` (untuk live preview di UI). |
| `GET`    | `/api/broadcast/:id`          | Detail broadcast + flag `running`.                            |
| `GET`    | `/api/broadcast/:id/recipients` | List per-recipient log. Query: `?status=pending\|sent\|failed`. |
| `POST`   | `/api/broadcast/:id/cancel`   | Hentikan broadcast yang sedang running (worker stop at next iter). |
| `DELETE` | `/api/broadcast/:id`          | Hapus broadcast (gagal kalau masih running, cancel dulu).     |
| `GET`    | `/api/schedules`              | List jadwal.                                                 |
| `POST`   | `/api/schedules`              | Buat jadwal.                                                 |
| `GET`    | `/api/schedules/:id`          | Detail jadwal.                                               |
| `PUT`    | `/api/schedules/:id`          | Update jadwal.                                               |
| `DELETE` | `/api/schedules/:id`          | Hapus jadwal.                                                |
| `POST`   | `/api/schedules/:id/toggle`   | Enable/disable.                                              |
| `POST`   | `/api/schedules/:id/run`      | Eksekusi sekali sekarang (manual).                           |
| `GET`    | `/api/schedules/:id/logs`     | Log eksekusi per jadwal. Query: `?limit=50`.                 |
| `POST`   | `/api/schedules/preview`      | Preview N fire-time berikutnya (tanpa simpan). `?count=5`.    |
| `GET`    | `/api/logs`                   | Log eksekusi global terbaru. Query: `?limit=100`.            |
| `GET`    | `/api/aireply/config`         | Config AI per-device. API key dikembalikan dalam bentuk masked (`sk-v****Aw5A`). |
| `PUT`    | `/api/aireply/config`         | Simpan config. API key kosong = pertahankan yang tersimpan.   |
| `POST`   | `/api/aireply/config/test`    | Test koneksi provider; balas `{latency_ms, model_response}`.  |
| `POST`   | `/api/aireply/documents`      | Upload dokumen KB (`multipart/form-data` field `file`).       |
| `GET`    | `/api/aireply/documents`      | List dokumen KB beserta status (`processing`/`ready`/`failed`). |
| `DELETE` | `/api/aireply/documents/:id`  | Hapus dokumen + semua chunk-nya.                              |
| `POST`   | `/api/aireply/documents/reindex` | Re-embed semua chunk (pakai setelah ganti embed model).    |
| `GET`    | `/api/aireply/chat-settings`  | List chat opt-in per-device.                                  |
| `PUT`    | `/api/aireply/chat-settings/:chat_jid` | Toggle AI on/off untuk satu chat. Body `{"enabled":bool}`. |
| `GET`    | `/api/aireply/logs`           | Audit log eksekusi AI. Query: `?chat_jid=&status=&limit=50`.  |
| `POST`   | `/api/aireply/config/apply-to-all` | Fan-out: simpan config AI **dan** replikasi chat-toggles ke semua device logged_in. Query `?with_chats=false` untuk skip chat replication (default true). Body sama dengan PUT `/aireply/config`. Return `{success_count, total, results[], chat_sync{source_chats,target_devices,applied_ok,applied_fail,errors}}`. **Penting**: tanpa replikasi chat-toggle, device kedua punya config tapi tidak balas otomatis. |
| `POST`   | `/api/aireply/chat-settings/:chat_jid/apply-to-all` | Fan-out: enable/disable toggle AI untuk satu chat JID di seluruh device. Body `{"enabled":bool}`. Berguna untuk customer multi-channel. |
| `GET`    | `/api/aireply/multi-device-health` | Audit per-device readiness: `has_config`, `has_api_key`, `chat_enabled_count`, `status` (ready/no_config/no_api_key/no_chats), `hint`. Pakai untuk diagnose kenapa device tertentu tidak balas auto. |
| `POST`   | `/api/aireply/pause` | Pause global AI Reply (semua device + semua chat). Body: `{"minutes": N}`. N ≤ 0 = indefinite (sampai resume manual atau container restart). Saat paused, AI dan static auto-reply dua-duanya skip. State in-memory di core. |
| `POST`   | `/api/aireply/resume` | Cabut pause, AI Reply kembali aktif. |
| `GET`    | `/api/aireply/pause-status` | `{paused, paused_until, remaining_seconds}`. Dashboard SPA poll setiap 30 detik saat tab AI Reply terbuka. |

Contoh payload `POST /api/schedules`:

```json
{
  "name": "Reminder rapat mingguan",
  "device_id": "6289605618749@s.whatsapp.net",
  "recipient": "120363xxxxxxxxxxxx@g.us",
  "message_type": "text",
  "message": "Halo tim, rapat jam 09:00.",
  "schedule_type": "weekly",
  "run_at": "2026-05-18T09:00",
  "cron_expr": "1,3,5",
  "timezone": "Asia/Jakarta",
  "enabled": true
}
```

Untuk REST API core (kirim langsung tanpa dashboard), lihat [`docs/openapi.yaml`](./docs/openapi.yaml).

---

## Tipe Jadwal

| Tipe      | Field yang dipakai                                   | Contoh                                                          |
|-----------|------------------------------------------------------|-----------------------------------------------------------------|
| `once`    | `run_at` (tanggal + jam)                             | Reminder satu kali 12 Mei 2026 jam 14:00.                       |
| `daily`   | `run_at` (jam-menit diambil)                         | Tiap hari jam 08:00.                                            |
| `weekly`  | `run_at` (jam-menit) + `cron_expr` CSV hari (0=Min)  | `cron_expr="1,3,5"` jam 09:00 → Senin/Rabu/Jumat.               |
| `monthly` | `run_at` (tanggal + jam)                             | Tiap tanggal 1 jam 07:00.                                       |
| `yearly`  | `run_at` (bulan + tanggal + jam)                     | Setiap 17 Agustus jam 10:00.                                    |
| `cron`    | `cron_expr` 5-field                                  | `0 9 * * 1-5` → jam 09:00 setiap weekday.                       |

Format cron: 5 field `menit jam hari-bulan bulan hari-pekan` (parser `robfig/cron/v3`, hari-pekan `0-6` dengan `0=Minggu`).

Tipe pesan yang valid: `text`, `image`, `video`, `file`, `audio`, `location`, `link`. Field wajib berbeda per tipe (mis. `media_url` untuk media, `latitude`+`longitude` untuk location, `link_url` untuk link). Validasi penuh ada di `dashboard/internal/api/handlers.go`.

---

## Catatan Penting

- **Jangan modifikasi `src/`** kecuali memang perlu fork penuh — dashboard sengaja didesain sebagai overlay sehingga `git pull` upstream aman.
- **Device ID vs JID** (penting saat integrasi ke API): `device_id` di header bisa alias ("my-device") atau JID; saat menyimpan/lookup data chat selalu pakai JID *tanpa* device-number (`ToNonAD()`). Detail lengkap di [`CLAUDE.md`](./CLAUDE.md).
- **FFmpeg + libwebp** diperlukan oleh core untuk konversi media & sticker. Image Docker bawaan sudah menyertakan keduanya.
- **Database**: core default `storages/whatsapp.db` (SQLite); set `DB_URI=postgres://...` untuk PostgreSQL. Dashboard selalu pakai SQLite pure-Go lokal — tidak ada CGO, binary jalan di Alpine tanpa libc tambahan.
- **Webhook payload v8+** menyertakan `device_id` top-level (lihat [`docs/webhook-payload.md`](./docs/webhook-payload.md)).
- **AI Reply auto-enable di Docker**: entrypoint core ([`docker/entrypoint.sh`](./docker/entrypoint.sh)) default `AI_REPLY_ENABLED=true` dan **auto-generate `AI_ENCRYPTION_KEY`** kalau kosong, lalu simpan di `storages/.ai-encryption-key` (persisten lewat volume). Jadi tab AI Reply di dashboard langsung jalan tanpa konfigurasi tambahan. **Backup file `storages/.ai-encryption-key` terpisah** (mis. password manager) — kehilangan file ini = semua API key provider tersimpan jadi unreadable. Untuk matikan fitur, set `AI_REPLY_ENABLED=false` di `src/.env`. Dokumentasi lengkap: [`docs/PRD-ai-auto-reply.md`](./docs/PRD-ai-auto-reply.md).
- **Cache**: dashboard mengembalikan `Cache-Control: no-store` untuk seluruh static asset, supaya UI selalu pakai versi terbaru setelah rebuild Docker image.

---

## Lisensi & Atribusi

- Core (`src/`) adalah karya [@aldinokemal](https://github.com/aldinokemal) — lihat [`LICENCE.txt`](./LICENCE.txt) dan repo upstream [`aldinokemal/go-whatsapp-web-multidevice`](https://github.com/aldinokemal/go-whatsapp-web-multidevice). Dukung lewat [Patreon](https://www.patreon.com/c/aldinokemal) jika kamu pakai untuk produksi.
- Dashboard (`dashboard/`) ditulis ulang dari nol sebagai overlay di repo ini; mengikuti lisensi yang sama (MIT).
