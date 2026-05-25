# Dashboard Changes — May 2026

Dokumen ini merangkum semua perubahan yang ditambahkan ke `dashboard/` dalam
satu sesi pengembangan. Tujuan: arsip rationale + lokasi file + cara deploy
+ troubleshooting, supaya developer berikutnya (atau kita di masa depan) tidak
perlu reverse-engineer keputusan desain.

**Prinsip yang dijaga**: SEMUA perubahan hanya di `dashboard/`. Folder `src/`
(gowa core) **tidak disentuh sama sekali** — sesuai CLAUDE.md guideline
"Never modify `src/` for things that can live in `dashboard/`".

---

## Daftar Perubahan

1. [Excel Import/Export pada tab Jadwal & Reminder](#1-excel-importexport)
2. [Search & Sort pada tab Jadwal & Reminder](#2-search--sort)
3. [Akurasi Status Koneksi Device (Tri-State)](#3-tri-state-device-status)

---

## 1. Excel Import/Export

### Latar Belakang

Tab **Jadwal & Reminder** sebelumnya hanya menerima input manual (form per
jadwal). Untuk volume besar (mis. setup awal banyak reminder, migrasi dari
sheet eksternal, backup/restore), bottleneck banget. User minta export ke
`.xlsx` + import dari `.xlsx`.

### Keputusan Desain

| Keputusan | Pilihan | Alasan |
|---|---|---|
| Library Excel | `github.com/xuri/excelize/v2` (Go) | Pure-Go, no CGO (selaras dengan dashboard yang pakai `modernc.org/sqlite`). Single source of truth — server handle both export & import dengan validasi konsisten. |
| Sisi processing | Backend | Validasi import harus server-side anyway (write ke DB); kalau frontend pakai SheetJS, logic terpecah dua tempat. |
| Behavior import | Skip baris invalid, lanjutkan | Konfirmasi dari user. Cocok untuk file besar campur error kecil — user dapat tabel hasil per-baris untuk fix file sumber. |
| Behavior import | Insert sebagai row baru (no upsert) | Simpler & lebih predictable. Kalau user mau "replace", hapus dulu yang lama. |
| Kolom `id` | Export only, ignored on import | Mencegah accidental overwrite. |
| Format `run_at` | `YYYY-MM-DD HH:MM:SS` di timezone schedule | User lihat jam yang sama di UI & Excel; round-trip parse pakai `parseLocalTime` existing. |
| Format `enabled` | `true`/`false` (lowercase) | Plus parser tolerate `1/0`, `yes/no`, `aktif/tidak`, dll. |
| Scope Export | Selalu full list (tidak ikut filter UI) | Backup integrity — user bisa hapus baris di Excel sendiri kalau perlu subset. |

### File yang Ditambahkan / Diubah

**Backend:**

- `dashboard/go.mod` — tambah `github.com/xuri/excelize/v2 v2.8.1`
- `dashboard/internal/api/schedules_xlsx.go` **(BARU)** — 2 handler:
  - `exportSchedulesXLSX` — generate `.xlsx` dengan header bold + colored fill, 20 kolom, lebar kolom yg readable. Set `Content-Disposition` untuk auto-download.
  - `importSchedulesXLSX` — terima multipart `file`, parse via `excelize.OpenReader`, header lookup case-insensitive (toleran ke kolom yang di-reorder atau dihapus), per-baris validate pakai `h.buildSchedule` (re-use validasi yang sama dengan POST/PUT existing), skip invalid + log per-row result.
- `dashboard/internal/api/handlers.go`:
  - Register routes:
    ```go
    g.Post("/schedules/preview", h.previewSchedule)
    g.Get("/schedules/export.xlsx", h.exportSchedulesXLSX)
    g.Post("/schedules/import", h.importSchedulesXLSX)
    g.Get("/schedules/:id", h.getSchedule)   // ← parametric route AFTER static
    ```
    **Catatan**: Static route (`export.xlsx`, `import`) WAJIB dideklarasi
    SEBELUM `/schedules/:id`. Fiber router pada beberapa build matching greedy
    — `export.xlsx` bisa dianggap sebagai value `:id` dan masuk ke
    `getSchedule` (return 400 "invalid id"). Urutan ini menjamin tidak
    shadowing.
  - Update `health` response untuk include route baru.

**Frontend (`dashboard/web/index.html`):**

- Tombol **Export Excel** + **Import Excel** di header bar tab Jadwal
- Hidden `<input type="file">` di-trigger via tombol Import
- **Modal hasil import** (`#schedule-import-result-modal`): stats berhasil/gagal/total + tabel per-baris (hijau OK, merah gagal, detail error tiap baris)
- Vue data: `schExporting`, `schImporting`, `schImportResult`
- Methods: `doScheduleExport`, `triggerScheduleImport`, `onScheduleImportFile`
- CSS: `.import-row-ok` (hijau muda), `.import-row-fail` (merah muda)

### Endpoint yang Ditambahkan

```
GET    /api/schedules/export.xlsx       Download semua jadwal (sheet "Schedules")
POST   /api/schedules/import            Multipart "file" — return per-row results
```

Format response import:
```json
{
  "imported_count": 12,
  "failed_count": 2,
  "total": 14,
  "results": [
    { "row": 2, "name": "Reminder X", "ok": true, "schedule_id": 45 },
    { "row": 3, "name": "Invalid", "ok": false, "error": "unknown message_type \"\"" }
  ]
}
```

### Struktur Kolom Excel

| Kolom | Required | Notes |
|---|---|---|
| `id` | export only | Diabaikan saat import |
| `name` | ✅ | |
| `device_id` | | |
| `recipient` | ✅ | |
| `message_type` | ✅ | text\|image\|video\|file\|audio\|location\|link |
| `message` | | |
| `media_url` | | |
| `caption` | | |
| `latitude` / `longitude` | | Untuk message_type=location |
| `link_url` | | Untuk message_type=link |
| `schedule_type` | ✅ | once\|daily\|weekly\|monthly\|yearly\|cron |
| `run_at` | | Format `YYYY-MM-DD HH:MM:SS` di timezone schedule |
| `cron_expr` | | Untuk type=cron, atau CSV 0-6 untuk type=weekly |
| `timezone` | | Mis. `Asia/Jakarta` |
| `enabled` | | Default `true` kalau kosong |
| `last_run_at` / `next_run_at` / `last_status` / `run_count` | export only | Read-only state |

### Testing

```bash
# Export (via Nginx aaPanel)
curl -u USER:PASS https://your-domain.com/api/schedules/export.xlsx -o export.xlsx
file export.xlsx   # → Microsoft Excel 2007+

# Import
curl -u USER:PASS -X POST -F "file=@my-schedules.xlsx" \
  https://your-domain.com/api/schedules/import | jq
```

Workflow round-trip test: Export → edit row di Excel → Import → cek hasil di
modal (baris yang OK akan jadi schedule baru, ID lama diabaikan).

---

## 2. Search & Sort

### Latar Belakang

Tab Jadwal sebelumnya cuma tampilkan tabel datar urut DESC by ID. Untuk
user dengan banyak schedule, susah cari schedule spesifik (nama, recipient,
device).

### Keputusan Desain

| Keputusan | Pilihan | Alasan |
|---|---|---|
| Lokasi processing | Frontend (client-side) | Untuk <1000 schedule, lebih cepat dari server roundtrip. Vue computed property bahkan auto-react ke perubahan input. |
| Sort UI | Tri-state per kolom (klik = ASC → DESC → reset) | Pattern standar tabel data, tidak butuh tombol reset terpisah. |
| Search scope | name, device, recipient, message, message_type, schedule_type, cron_expr, last_status | Cover semua field tekstual yang user mungkin cari. |

### File yang Diubah

`dashboard/web/index.html`:

- CSS:
  ```css
  th.sortable { cursor: pointer; user-select: none; white-space: nowrap; }
  th.sortable:hover { background: #f0f4f8 !important; }
  th.sortable .sort-ind { font-size: 10px; margin-left: 4px; color: #999; }
  th.sortable.sorted .sort-ind { color: #2185d0; font-weight: bold; }
  .schedules-toolbar { display: flex; gap: 10px; ...; }
  ```
- Vue data: `schSearch`, `schSortField`, `schSortOrder`
- Computed:
  - `filteredSortedSchedules` — filter dulu (case-insensitive), lalu sort. Handle special types: datetime (compare via `Date.getTime()`, null = paling akhir), boolean (`enabled`), string fallback.
  - `schSortLabel` — untuk tampilan "Urut: Nama ↑" di toolbar
- Methods:
  - `setScheduleSort(field)` — tri-state cycle
  - `resetScheduleSort()` — reset ke default
  - `scheduleSortInd(field)` — return `▲` / `▼` / `↕` untuk header
- Template:
  - Toolbar dengan `<input>` search + indicator "Menampilkan N dari M jadwal"
  - Tabel header: setiap `<th>` clickable kecuali kolom "Aksi"
  - Empty-state berbeda untuk "no data" vs "no match search"
  - `v-for="s in filteredSortedSchedules"` (sebelumnya `schedules`)

### Test

- Ketik di kotak search → list filter live (tidak ada debounce — sudah cepat)
- Klik header "Nama" → urut alfabet ASC, indikator `▲`
- Klik lagi → DESC, indikator `▼`
- Klik ketiga kali → reset ke default (`↕`)
- Klik header "Next Run" → urut by datetime, null di akhir

---

## 3. Tri-State Device Status

### Latar Belakang

User report: di tab Devices, pill "Connected" tetap hijau walaupun device
aktual sudah disconnect (mis. matikan WhatsApp di HP, WS terputus).

### Root Cause

`src/usecase/device.go` fungsi `deriveState`:

```go
if client.IsLoggedIn() {
    state = domainDevice.DeviceStateLoggedIn  // ← bug
} else if client.IsConnected() {
    state = domainDevice.DeviceStateConnected
} else {
    state = domainDevice.DeviceStateDisconnected
}
```

`whatsmeow.IsLoggedIn()` cuma cek apakah kredensial pair tersimpan di local
store — **tidak cek WS aktif**. Akibatnya device paired-tapi-offline (network
drop, server restart, dll) tetap return true → state = "logged_in" → UI
hijau "Connected".

Endpoint per-device `/devices/:id/status` di core **sudah benar** (return
`is_connected` & `is_logged_in` terpisah), tapi list endpoint `/devices`
hanya kasih `state` lossy.

### Keputusan Desain

| Keputusan | Pilihan | Alasan |
|---|---|---|
| Lokasi fix | Dashboard saja | Sesuai prinsip "jangan sentuh src/" — supaya `git pull` upstream tetap painless. |
| Mekanisme | Enrich list di dashboard via parallel `/devices/:id/status` per device | Akurasi sama dengan modifikasi core, tanpa upstream conflict. N+1 query — tapi paralel via goroutine, total latency = max bukan sum. |
| UI | Tri-state pill: Connected (green) / Offline-paired (yellow) / Disconnected (red) | Bedakan "perlu reconnect" vs "perlu scan QR baru" — actionable. |
| Action buttons per state | Connected→Logout, Offline-paired→Reconnect prominent, Disconnected→Scan QR | Reduce friction; user tidak harus tebak harus klik apa. |

### File yang Ditambahkan / Diubah

**Backend:**

- `dashboard/internal/api/device_enrich.go` **(BARU)**:
  - `enrichDeviceListWithStatus(client *wa.Client, raw json.RawMessage) (json.RawMessage, bool)` — parse list, fire goroutine per device call `/devices/:id/status`, merge `is_connected` & `is_logged_in` ke item, re-encode preserving wrapper shape (direct array / `{data:[]}` / `{devices:[]}`).
  - `parseDeviceListPreservingShape` — defensive parser untuk 3 kemungkinan shape response.
  - `encodeDeviceList` — re-encode dengan wrapper yang sesuai.
  - `pickDeviceIdentifier` — return field yang dipakai sebagai X-Device-Id (prefer `id`, fallback ke `device`/`name`/`jid`).
- `dashboard/internal/api/handlers.go`:
  - `listDevices` handler — call `enrichDeviceListWithStatus` sebelum return.
  - `listLoggedInDeviceIDs` (helper untuk apply-to-all) — juga enrich, lalu filter pakai `is_logged_in` flag preferentially (fallback ke state string untuk backward compat dengan core lama).

**Frontend (`dashboard/web/index.html`):**

- Helpers di methods:
  - `isDeviceConnected(d)` — prefer `d.is_connected && d.is_logged_in` (flag baru dari enrichment), fallback ke `d.state === 'logged_in'`, fallback ke legacy `d.is_logged_in` boolean.
  - `isDevicePairedOffline(d)` — return `true` jika `is_logged_in && !is_connected`.
  - `deviceStatusLabel(d)` / `deviceStatusClass(d)` / `deviceStatusIcon(d)` — single source untuk pill rendering (Connected/Offline paired/Disconnected dengan kelas `on`/`warn`/`off`).
- Card UI tab Devices:
  - Pill pakai `deviceStatusClass(d)` / `deviceStatusLabel(d)` (tri-state)
  - Action buttons conditional:
    - Connected → Logout (orange)
    - Offline paired → Reconnect prominent (green) + warning hint
    - Disconnected → Scan QR (green)
  - Debug info di description menampilkan `state`, `is_connected`, `is_logged_in` raw

### Trade-off vs Modifikasi Core

| | Modifikasi src/ | Dashboard enrichment (chosen) |
|---|---|---|
| Upstream conflict | ⚠️ Tinggi (rebase per pull) | ✅ Tidak ada |
| Latency | 1 round-trip | N round-trip paralel (max ≈ 200ms untuk <20 device) |
| Bandwidth | Sama | N× lebih |
| Akurasi | ✅ | ✅ |
| Effort maintenance | Tinggi | Rendah |

### Performance Note

Untuk N device, total latency = `max(per-device status latency)` (goroutine
parallel, bukan sum). Untuk <20 device, total < 500ms. Untuk >>50 device
perlu refactor pakai semaphore worker pool (tidak prematur — tipikal use
case dashboard di bawah threshold itu).

### Test

```bash
# Verifikasi enrichment
curl -u USER:PASS https://your-domain.com/api/devices | jq '.results[0]'
# Harus muncul fields: is_connected, is_logged_in (selain state)
```

UI test:
1. Device online normal → pill hijau "Connected"
2. Matikan WhatsApp di HP → reload tab Devices → pill kuning "Offline (paired)"
3. Klik tombol Reconnect (hijau prominent) → kembali hijau setelah beberapa detik
4. Logout dari dashboard → pill merah "Disconnected" → tombol Scan QR muncul

---

## Deploy Steps (Semua Perubahan)

```bash
# Rebuild dashboard saja — core (whatsapp_go) tidak perlu rebuild
docker compose -f docker-compose.aapanel.yml up -d --build dashboard
```

Setelah container up:

```bash
# Verifikasi route baru terdaftar
curl -u USER:PASS http://127.0.0.1:18088/api/_health | grep -E "(export|import|devices)"
```

Lalu hard refresh browser (Ctrl+Shift+R) untuk reload `index.html` baru dengan
UI tri-state + tombol Excel.

---

## Troubleshooting

### Export error 404 di browser

- Cek apakah binary baru sudah ke-load: `curl http://127.0.0.1:18088/api/_health | grep export`
- Kalau curl direct ke `127.0.0.1` work tapi browser publik 404 → bug Nginx reverse proxy aaPanel (trailing slash di `proxy_pass` atau URL di `Host` header). Fix dengan `scripts/aapanel-install-nginx.sh`.

### Import gagal "kolom wajib tidak ditemukan"

- Pastikan baris pertama file `.xlsx` berisi nama kolom (header). Download template dulu via Export.
- Kolom wajib: `name`, `recipient`, `message_type`, `schedule_type`.

### Device status masih salah setelah rebuild

- Cek `/api/devices` response apakah punya field `is_connected` & `is_logged_in`. Kalau tidak ada → enrichment gagal (mungkin core `/devices/:id/status` error).
- Cek log dashboard: `docker compose -f docker-compose.aapanel.yml logs --tail=100 dashboard` cari pesan `[enrich]`.

### `go: github.com/xuri/excelize/v2 ... missing go.sum entry`

- Dockerfile sudah punya `go mod tidy` di build step. Pastikan rebuild tanpa cache: `docker compose -f docker-compose.aapanel.yml build --no-cache dashboard`.

### Build gagal `proxy.golang.org timeout`

- Server aaPanel firewall outbound. Set `GOPROXY=direct` di Dockerfile sementara, atau pre-fetch dependency di mesin dev lalu commit `go.sum`.

---

## Files Summary

```
dashboard/
├── go.mod                                    # MODIFIED: + excelize/v2
├── internal/api/
│   ├── handlers.go                           # MODIFIED: routes, enrichment, route order
│   ├── schedules_xlsx.go                     # NEW: export & import handlers
│   └── device_enrich.go                      # NEW: parallel status enrichment
└── web/
    └── index.html                            # MODIFIED: search/sort UI, Excel buttons,
                                              #           tri-state device pill, modal
docs/
└── dashboard-changes-2026-05.md              # THIS FILE
```

**Tidak ada perubahan di `src/`** — folder core tetap clean / upstream-aligned.
