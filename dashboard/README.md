# WhatsApp Dashboard (companion app)

Dashboard web untuk mengelola pesan WhatsApp melalui [go-whatsapp-web-multidevice](../). Mendukung:

- Multi-device: pilih device aktif dari semua device yang terdaftar di core API.
- Kirim pesan instan (text, image, video, file, audio, location, link).
- **Penjadwalan pesan**: sekali (one-time), harian, mingguan (pilih hari-hari), bulanan, tahunan, atau cron expression bebas.
- **Reminder otomatis**: pesan dikirim tepat pada tanggal & jam yang dipilih, sesuai timezone.
- Log eksekusi setiap pengiriman + tombol "run now" untuk uji manual.

Dashboard ini adalah aplikasi **terpisah** (binary Go sendiri di folder `dashboard/`). Inti project di `src/` **tidak diubah sama sekali**, jadi setiap update versi upstream bisa langsung di-pull tanpa konflik.

---

## Arsitektur

```
┌──────────────────────┐ HTTP   ┌─────────────────────────┐
│  Browser (Vue 3 UI)  │ ─────► │  dashboard (port 8088)  │
└──────────────────────┘        │  - SQLite schedules db  │
                                │  - cron engine          │
                                └───────────┬─────────────┘
                                            │ HTTP (REST API)
                                            ▼
                                ┌─────────────────────────┐
                                │  core go-whatsapp       │
                                │  (src/ - port 3000)     │
                                └─────────────────────────┘
```

Dashboard menyimpan jadwal di file `dashboard.db` (SQLite, pure-Go driver). Saat tiba waktunya, scheduler memanggil endpoint `/send/...` di core API.

---

## Cara Menjalankan

### 1. Jalankan core REST API

Dari folder `src/` (terminal pertama):

```bash
cd src
go run . rest
```

Pastikan minimal sudah ada satu device yang sudah login (scan QR di [http://localhost:3000](http://localhost:3000)).

### 2. Jalankan dashboard

Di terminal kedua:

```bash
cd dashboard
copy .env.example .env       # sesuaikan jika perlu
go mod tidy
go run .
```

Buka [http://localhost:8088](http://localhost:8088).

> Build binary single-file: `go build -o whatsapp-dashboard.exe` (Windows) atau `go build -o whatsapp-dashboard` (Linux/macOS). HTML UI di-embed via `go:embed` sehingga binary bisa dipindah ke folder lain.

---

## Konfigurasi (.env)

| Variable | Default | Keterangan |
|---|---|---|
| `DASHBOARD_HOST` | `0.0.0.0` | Bind address dashboard. |
| `DASHBOARD_PORT` | `8088` | Port HTTP dashboard. |
| `DASHBOARD_DB` | `dashboard.db` | Path file SQLite untuk menyimpan jadwal & log. |
| `DASHBOARD_BASIC_AUTH` | (kosong) | Optional `user:pass` untuk login dashboard. |
| `WHATSAPP_API_URL` | `http://localhost:3000` | URL core REST API. |
| `WHATSAPP_API_USER` / `WHATSAPP_API_PASSWORD` | (kosong) | Basic auth core API (kalau `APP_BASIC_AUTH` di-set). |
| `DASHBOARD_TZ` | `Local` | Timezone default untuk jadwal baru, mis. `Asia/Jakarta`. |

---

## Tipe Jadwal

| Tipe | Field yang dipakai | Contoh |
|---|---|---|
| `once` | `run_at` | Reminder pertemuan jam 14:00 tanggal 12 Mei 2026. |
| `daily` | `run_at` (jam-menit) | Tiap hari jam 08:00 (broadcast). |
| `weekly` | `run_at` (jam-menit) + pilihan hari (`cron_expr` = "1,3,5") | Senin/Rabu/Jumat jam 09:00. |
| `monthly` | `run_at` (tanggal + jam) | Tiap tanggal 1 jam 07:00. |
| `yearly` | `run_at` (tanggal + bulan + jam) | Setiap 17 Agustus jam 10:00. |
| `cron` | `cron_expr` (5 field) | `0 9 * * 1-5` = jam 09:00 setiap weekday. |

Format cron memakai library `robfig/cron/v3` (5 field: `menit jam hari-bulan bulan hari-pekan`, hari-pekan `0-6` dengan 0=Minggu).

---

## API Dashboard

Selain UI, dashboard juga menyediakan REST API sendiri (di `/api`) untuk integrasi:

| Method | Path | Keterangan |
|---|---|---|
| `GET` | `/api/devices` | Proxy ke `/app/devices` upstream. |
| `GET` | `/api/devices/:id/status` | Proxy status device. |
| `POST` | `/api/send` | Kirim pesan sekarang (text/image/video/file/audio/location/link). |
| `GET` | `/api/schedules` | List semua jadwal. |
| `POST` | `/api/schedules` | Buat jadwal baru. |
| `GET` | `/api/schedules/:id` | Detail jadwal. |
| `PUT` | `/api/schedules/:id` | Update jadwal. |
| `DELETE` | `/api/schedules/:id` | Hapus jadwal. |
| `POST` | `/api/schedules/:id/toggle` | Enable/disable jadwal. |
| `POST` | `/api/schedules/:id/run` | Jalankan sekali sekarang (manual). |
| `GET` | `/api/schedules/:id/logs` | Log eksekusi per jadwal. |
| `POST` | `/api/schedules/preview` | Preview 5 jadwal berikutnya (tanpa simpan). |
| `GET` | `/api/logs` | Log eksekusi global terbaru. |

---

## Update Versi Core

Karena dashboard tidak menyentuh folder `src/`, update core cukup dilakukan dengan cara biasa (mis. `git pull` di repo upstream). Yang dibutuhkan dashboard hanyalah:

- REST endpoint upstream tidak berubah (`/app/devices`, `/send/message`, dll.), dan
- Header `X-Device-Id` masih dipakai untuk pemilihan device.

Jika upstream menambah endpoint baru, cukup tambahkan method di `internal/wa/client.go` dan handler/UI di sini — tidak perlu modifikasi core.
