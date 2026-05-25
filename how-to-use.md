# Cara Pakai WhatsApp Dashboard

Panduan lengkap dari nol — dibuat dengan bahasa sederhana. Bahkan kalau Anda bukan orang IT, ikuti saja langkah-langkahnya satu per satu. Setiap langkah punya **gambaran apa yang terjadi**, supaya tidak bingung kalau ada yang berbeda di komputer Anda.

---

## Daftar Isi

1. [Aplikasi ini sebenarnya apa?](#1-aplikasi-ini-sebenarnya-apa)
2. [Yang harus disiapkan dulu](#2-yang-harus-disiapkan-dulu)
3. [Instalasi langkah demi langkah](#3-instalasi-langkah-demi-langkah)
4. [Menjalankan aplikasi untuk pertama kali](#4-menjalankan-aplikasi-untuk-pertama-kali)
5. [Cara menambah device WhatsApp (scan QR)](#5-cara-menambah-device-whatsapp-scan-qr)
6. [Cara kirim pesan sekarang juga](#6-cara-kirim-pesan-sekarang-juga)
7. [Cara membuat jadwal & reminder otomatis](#7-cara-membuat-jadwal--reminder-otomatis)
7B. [Cara broadcast ke banyak nomor (anti-spam built-in)](#7b-cara-broadcast-ke-banyak-nomor-anti-spam-built-in)
7C. [Status koneksi ke Core API (badge di kanan atas dashboard)](#7c-status-koneksi-ke-core-api-badge-di-kanan-atas-dashboard)
8. [Cara melihat riwayat pengiriman + auto-delete logs](#8-cara-melihat-riwayat-pengiriman)
9. [Cara upgrade ke versi terbaru](#9-cara-upgrade-ke-versi-terbaru)
10. [Kalau ada masalah (troubleshooting)](#10-kalau-ada-masalah-troubleshooting)
11. [Cara install pakai Docker (container)](#11-cara-install-pakai-docker-container)
12. [Cara install di aaPanel (anti-conflict dengan container lain)](#12-cara-install-di-aapanel-anti-conflict-dengan-container-lain)

---

## 1. Aplikasi ini sebenarnya apa?

Anggap saja ini **WhatsApp Web versi pribadi** yang punya dua kemampuan tambahan:

- **Bisa pegang banyak nomor WhatsApp sekaligus** (multi-device). Misalnya nomor pribadi + nomor toko + nomor admin.
- **Bisa kirim pesan otomatis sesuai jadwal**. Misalnya tiap pagi jam 8 kirim "selamat pagi" ke grup keluarga, atau setiap tanggal 1 kirim reminder bayar listrik ke diri sendiri.

Aplikasi ini terdiri dari **dua bagian** yang harus jalan bersamaan:

| Bagian | Tugasnya | Port |
|---|---|---|
| **Core (di folder `src/`)** | Menyambung ke server WhatsApp resmi, menyimpan chat. | 3000 |
| **Dashboard (di folder `dashboard/`)** | Tampilan untuk Anda — pilih device, kirim pesan, buat jadwal. | 8088 |

Keduanya **harus jalan dua-duanya** supaya bisa dipakai. Tenang, nanti saya kasih cara mudahnya.

---

## 2. Yang harus disiapkan dulu

### 2.1. Komputer

- Komputer Windows (panduan ini fokus Windows; Linux/Mac juga bisa dengan langkah serupa).
- Bisa terhubung internet.
- Punya hak admin untuk install software (Go).

### 2.2. Install Go (satu kali saja)

**Go** adalah bahasa program yang dipakai aplikasi ini — Anda **tidak perlu** belajar programming, cukup install saja seperti install aplikasi biasa.

1. Buka [https://go.dev/dl/](https://go.dev/dl/) di browser.
2. Klik tombol biru besar yang ada tulisan **"Microsoft Windows"** — file `.msi`-nya akan ter-download.
3. Buka file `.msi` itu, klik **Next → Next → Install**. Tunggu sampai selesai. Tutup installer.
4. **Penting**: tutup semua jendela Command Prompt / PowerShell yang sedang terbuka, lalu buka lagi yang baru (supaya Windows membaca daftar program terbaru).
5. **Cara cek Go sudah terinstall**:
   - Tekan tombol Windows, ketik `cmd`, tekan Enter.
   - Di jendela hitam yang muncul, ketik: `go version`
   - Kalau muncul tulisan seperti `go version go1.23.4 windows/amd64`, berarti **berhasil**.
   - Kalau muncul `'go' is not recognized`, restart komputer lalu coba lagi.

### 2.3. FFmpeg (opsional, hanya kalau mau kirim video/audio)

Ini program kecil untuk konversi video. Kalau Anda **cuma kirim teks dan gambar**, lewati saja bagian ini.

1. Download dari [https://www.gyan.dev/ffmpeg/builds/](https://www.gyan.dev/ffmpeg/builds/) — cari yang **"release essentials"**, klik `.7z` atau `.zip`.
2. Ekstrak isinya ke `C:\ffmpeg`.
3. Pastikan ada file `C:\ffmpeg\bin\ffmpeg.exe`.
4. Tambahkan `C:\ffmpeg\bin` ke **PATH** Windows:
   - Tekan Windows + R, ketik `sysdm.cpl`, Enter.
   - Tab **Advanced → Environment Variables**.
   - Di bagian bawah ("System variables"), cari **Path**, klik **Edit → New**, paste: `C:\ffmpeg\bin`
   - **OK → OK → OK**.

---

## 3. Instalasi langkah demi langkah

### 3.1. Pastikan folder aplikasi sudah ada di komputer

Folder utamanya: `F:\KIRO\go-whatsapp-web-multidevice-main\` (atau lokasi lain kalau Anda taruh di tempat berbeda).

Pastikan di dalamnya ada minimal dua folder ini:
- `src\` (program intinya)
- `dashboard\` (tampilan dashboard yang baru dibuat)

### 3.2. Siapkan core (bagian `src/`)

1. Tekan tombol Windows, ketik `cmd`, Enter.
2. Pindah ke folder `src/` aplikasi. Kalau folder aplikasi di `F:\KIRO\go-whatsapp-web-multidevice-main\`, ketik:

   ```
   F:
   cd F:\KIRO\go-whatsapp-web-multidevice-main\src
   ```

3. Ketik perintah ini untuk mengunduh komponen yang dibutuhkan (cuma sekali saja, butuh internet, sekitar 1–5 menit):

   ```
   go mod tidy
   ```

   Tunggu sampai cursor kembali ke kondisi siap (tanda `>` muncul lagi). Kalau ada peringatan kuning, biasanya tidak masalah.

### 3.3. Siapkan dashboard

1. Buka jendela **Command Prompt baru** (jangan tutup yang tadi).
2. Pindah ke folder `dashboard\`:

   ```
   F:
   cd F:\KIRO\go-whatsapp-web-multidevice-main\dashboard
   ```

3. Salin file `.env.example` menjadi `.env`:

   ```
   copy .env.example .env
   ```

4. **Boleh dilewati untuk pertama kali**: kalau mau ubah pengaturan (port, zona waktu, dll), buka file `.env` dengan Notepad dan ubah seperlunya. Defaultnya sudah cocok untuk Indonesia (Asia/Jakarta).

5. Unduh komponen dashboard:

   ```
   go mod tidy
   ```

   Tunggu selesai (1–3 menit).

**Selesai instalasi.** Mulai sekarang, untuk dipakai sehari-hari Anda **hanya perlu menjalankan dua perintah** — instalasi cuma sekali ini saja.

---

## 4. Menjalankan aplikasi untuk pertama kali

Aplikasi ini perlu **dua jendela Command Prompt terbuka bersamaan**:

### Jendela 1 — Core (WhatsApp engine)

1. Buka Command Prompt baru.
2. Ketik:

   ```
   cd F:\KIRO\go-whatsapp-web-multidevice-main\src
   go run . rest
   ```

3. Tunggu sampai muncul tulisan seperti **"Listening on 0.0.0.0:3000"** atau yang serupa. **Jendela ini jangan ditutup** selama Anda mau pakai aplikasi.

### Jendela 2 — Dashboard

1. Buka Command Prompt baru (yang **lain**, jangan tutup jendela 1).
2. Ketik:

   ```
   cd F:\KIRO\go-whatsapp-web-multidevice-main\dashboard
   go run .
   ```

   Atau lebih praktis: **double-click file `start.bat`** di Windows Explorer.

3. Tunggu sampai muncul tulisan **"dashboard listening on http://0.0.0.0:8088"**.

### Buka tampilan dashboard

1. Buka browser (Chrome / Edge / Firefox).
2. Ketik di address bar: `http://localhost:8088`
3. Tekan Enter.

Halaman dashboard akan muncul. 🎉

> **Cara menutup aplikasi nantinya**: di kedua jendela Command Prompt, tekan **Ctrl + C** (lalu Y kalau diminta). Tutup browser. Selesai.

---

## 5. Cara menambah device WhatsApp (scan QR)

Tambah device sekarang bisa **langsung dari dashboard** — tidak perlu buka halaman lain.

1. Di dashboard, masuk tab **Devices**.
2. Klik tombol biru **+ Tambah Device** di kanan atas.
3. Isi:
   - **Nama device**: bebas, untuk pengenal Anda sendiri. Hanya boleh huruf, angka, `_`, dan `-` (tanpa spasi). Contoh: `toko-1`, `pribadi`, `admin`.
   - **Metode login**: pilih salah satu:
     - **Scan QR** (default, mudah) — QR code akan muncul, scan dari HP.
     - **Pairing code** — alternatif kalau kamera HP tidak bisa scan QR; Anda akan dapat kode 8 karakter yang dimasukkan manual di WhatsApp.
4. Klik **Buat & Lanjutkan**.

### Kalau pilih Scan QR

5. QR code muncul. Di HP Anda:
   - Buka **WhatsApp** → titik tiga di pojok kanan atas → **Perangkat Tertaut** → **Tautkan Perangkat**.
   - Arahkan kamera ke QR di layar.
6. QR akan otomatis refresh tiap 60 detik kalau belum di-scan. Jangan khawatir, tunggu saja.
7. Begitu HP berhasil scan, tulisan akan berubah jadi **"Berhasil tersambung!"** (centang hijau besar). Klik **Tutup**.

### Kalau pilih Pairing code

5. Masukkan **Nomor WhatsApp** dengan kode negara tanpa tanda `+`. Contoh untuk `0896-1234-5678` → ketik `6289612345678`.
6. Klik **Buat & Lanjutkan**. Akan muncul **kode 8 karakter** seperti `ABCD-1234`.
7. Di HP Anda:
   - Buka **WhatsApp** → titik tiga → **Perangkat Tertaut** → **Tautkan dengan nomor telepon sebagai gantinya**.
   - Masukkan kode 8 karakter tadi.
8. Tunggu sampai tulisan berubah jadi **"Berhasil tersambung!"**. Klik **Tutup**.

### Tombol-tombol di kartu device

Setiap kartu device punya tombol kecil di bagian bawah:

| Tombol | Fungsi |
|---|---|
| 🟢 **Scan QR** | (kalau belum logged in) Tampilkan QR untuk login ulang. |
| 🟠 **Logout** | Putuskan koneksi WhatsApp dari device ini. Untuk pakai lagi harus scan QR. |
| 🔵 **Sync** | Reconnect — pakai kalau status disconnected padahal masih logged in. |
| 🔴 **Trash** | Hapus device sepenuhnya beserta session. **Tidak bisa di-undo.** |

**Ulangi langkah di atas** untuk setiap nomor WhatsApp lain yang ingin ditambahkan. Boleh banyak.

> **Catatan keamanan**: dengan cara ini, **HP Anda tetap berfungsi normal** untuk WhatsApp seperti biasa. Aplikasi cuma jadi "perangkat tertaut" tambahan, sama seperti WhatsApp Web di browser. Status di HP: Settings → Perangkat Tertaut → akan terlihat sebagai sesi baru.

---

## 6. Cara kirim pesan sekarang juga

1. Di dashboard, klik tab **Kirim Sekarang**.
2. Pilih:
   - **Device**: nomor yang mau dipakai untuk mengirim.
   - **Tipe Pesan**: Text untuk pesan biasa. (Bisa juga Image, Video, File, dll — tapi untuk pemula mulai dari Text saja dulu.)
   - **Penerima**: nomor tujuan dengan kode negara, tanpa tanda **+** dan tanpa spasi. Contoh untuk nomor Indonesia `0896-1234-5678` → tulis `6289612345678`.
   - **Pesan**: ketik isi pesan.
3. Klik tombol biru **Kirim**.
4. Kalau berhasil, akan muncul kotak hijau dengan tulisan `success`. Kalau gagal, kotak merah dengan keterangan masalahnya.

### Cara kirim gambar / file / video

Tipe pesan-pesan ini butuh **link URL** (alamat file di internet), bukan file dari komputer Anda langsung. Caranya:

- Upload dulu file Anda ke layanan seperti **Google Drive (link publik)**, **Imgur** (untuk gambar), **WeTransfer**, atau hosting gambar gratis lainnya.
- Salin link/URL file-nya.
- Tempel di kolom **Media URL** di dashboard.

Untuk **lokasi**, isi latitude dan longitude (bisa diambil dari Google Maps → klik kanan pada lokasi → angka pertama latitude, angka kedua longitude).

---

## 7. Cara membuat jadwal & reminder otomatis

Ini fitur utama dashboard ini. Bisa untuk:

- **Reminder sekali pakai**: misalnya "ingatkan saya bayar pajak motor tanggal 17 Mei 2026 jam 09:00".
- **Pesan rutin**: misalnya "tiap pagi jam 7 kirim selamat pagi ke grup keluarga".
- **Penagihan bulanan**: misalnya "setiap tanggal 1 jam 08:00 kirim invoice ke pelanggan".
- **Ulang tahun tahunan**: misalnya "setiap 17 Agustus jam 10:00 kirim ucapan ke teman".

### 7.1. Buka editor jadwal

1. Di dashboard, klik tab **Jadwal & Reminder**.
2. Klik tombol biru **+ Buat Jadwal Baru** di kanan atas.

### 7.2. Isi formulir

**Bagian atas (identitas):**

| Kolom | Isi dengan |
|---|---|
| Nama jadwal | Bebas, untuk pengingat Anda sendiri. Contoh: `Selamat pagi grup keluarga`, `Reminder bayar listrik`. |
| Device | Pilih nomor WhatsApp pengirim. |
| Penerima | Nomor tujuan (contoh: `6289612345678`) **atau** ID grup (contoh: `xxxxxxxxxx@g.us` — bisa dilihat di tab Devices kalau Anda buka grup). |

**Bagian Isi Pesan:**

- Pilih **Tipe pesan** (Text / Image / Video / dll).
- Untuk Text, isi pesannya saja.
- Untuk media, isi URL-nya seperti di bagian 6.

**Bagian Pengulangan — ini yang penting:**

Pilih salah satu **Tipe jadwal**:

| Tipe jadwal | Kapan dikirim? | Apa yang harus diisi? |
|---|---|---|
| **Sekali** | Hanya sekali, di tanggal & jam tertentu. | **Tanggal & jam pengiriman** (kalender + jam). |
| **Harian** | Setiap hari, pada jam yang sama. | **Tanggal & jam acuan** — yang dipakai cuma **jamnya** (tanggalnya bebas, biasanya pilih hari ini). |
| **Mingguan** | Pada hari-hari tertentu dalam seminggu, jam tertentu. | **Tanggal & jam acuan** (jam saja yang dipakai) + klik **hari-hari** yang diinginkan (boleh lebih dari satu, contoh: Senin + Rabu + Jumat). |
| **Bulanan** | Setiap bulan pada tanggal & jam tertentu. | **Tanggal & jam acuan** — tanggalnya menentukan **tanggal berapa tiap bulan** (mis. pilih tanggal 1 → tiap tanggal 1). |
| **Tahunan** | Setiap tahun pada tanggal + bulan + jam tertentu. | **Tanggal & jam acuan** — tanggal+bulan menentukan kapan tiap tahun (mis. 17 Agustus). |
| **Cron expression** | Untuk yang paham format cron Linux. Pemula lewati. | Format `menit jam tanggal bulan hari-pekan`. |

**Timezone**: defaultnya `Asia/Jakarta` (WIB). Biarkan saja kecuali Anda di WITA / WIT atau luar negeri.

**Tombol Preview**: klik untuk melihat 5 jadwal berikutnya — pastikan sesuai dengan yang Anda harapkan sebelum simpan.

### 7.3. Simpan

Klik tombol hijau **Simpan**. Jadwal langsung aktif dan akan otomatis jalan saat waktunya tiba — **selama core dan dashboard tetap berjalan** (jendela Command Prompt tidak ditutup).

### 7.4. Tabel jadwal — apa arti tombol-tombolnya?

Di tab **Jadwal & Reminder** ada tabel daftar jadwal dengan ikon-ikon kecil di kolom **Aksi**:

| Ikon | Fungsi |
|---|---|
| ✏️ Edit | Ubah jadwal. |
| ⏸ / ▶️ | Pause (sementara matikan) atau Lanjutkan jadwal. |
| ⚡ Bolt | Jalankan **sekarang juga** (manual, di luar jadwal — untuk test). |
| 📃 List | Lihat riwayat pengiriman jadwal ini. |
| 🗑 Trash | Hapus jadwal selamanya. |

---

## 7B. Cara broadcast ke banyak nomor (anti-spam built-in)

Klik tab **Broadcast** di dashboard. Cocok untuk: promo, pengumuman ke pelanggan, undangan, dll.

### 7B.1. Isi formulir

1. **Nama broadcast** (opsional) — untuk pengingat Anda di tab Riwayat. Default: `Broadcast YYYY-MM-DD HH:MM`.
2. **Device pengirim** — pilih nomor WhatsApp yang akan mengirim.
3. **Tipe pesan** — Text / Gambar / Video / File / Audio.
4. **Isi pesan** — Anda bisa pakai **spintax** untuk variasi otomatis tiap penerima:
   - `{Halo|Hai|Hi}` → setiap pesan random pilih satu (Halo / Hai / Hi).
   - `{Halo|Hai} {kak|bro|sis}` → kombinasi: "Halo kak", "Hai bro", "Halo sis", dst.
   - Indicator "Estimasi varian" di bawah field nunjukin total kombinasi.
5. **Daftar nomor tujuan** — paste-friendly. Pisahkan dengan **koma, baris baru, spasi, atau titik-koma**. Format yang diterima:
   - `6281234567890` (international tanpa +)
   - `08123456789` (Indonesia local, auto-konversi ke 62…)
   - `+628123456789` (dengan plus, auto-strip)
   - `xxxxx@g.us` (JID grup — broadcast ke grup juga bisa)
   - Duplikat otomatis di-dedupe.
   - Counter "N nomor valid · M invalid" muncul live saat Anda mengetik.
   - Klik **▼ Lihat hasil parse** untuk inspect daftar valid/invalid sebelum kirim.

### 7B.2. Pengaturan Anti-Spam (klik "Pengaturan Anti-Spam" untuk ekspansi)

Default sudah aman untuk volume kecil-menengah. Knob yang tersedia:

| Field | Default | Fungsi |
|---|---|---|
| Jeda min antar pesan | 8 detik | Minimum delay antar dua kirim. Backend paksa min 3 detik. |
| Jeda max antar pesan | 25 detik | Maximum delay. Tiap pesan random antara min-max. |
| Batch size | 0 (nonaktif) | Mis. 30 = istirahat lebih lama setiap 30 pesan. |
| Pause batch min/max | 120-300 detik | Jeda saat batch break (random di rentang itu). |
| Acak urutan kirim | ON | Shuffle daftar penerima — hindari pattern alphabet/sequence. |

### 7B.3. Rekomendasi setting per volume

| Volume | Delay | Batch size | Pause batch | Catatan |
|---|---|---|---|---|
| <50 nomor | 8-15 dtk | 0 | — | Setting default cukup. |
| 50-150 nomor | 10-25 dtk | 30 | 120-240 dtk | Batch break mulai aktif. |
| 150-500 nomor | 15-40 dtk | 25 | 180-360 dtk | Hati-hati, sebar ke beberapa hari kalau bisa. |
| >500 nomor | — | — | — | **Jangan dalam sehari**. Pecah per hari, atau pakai WhatsApp Business API resmi. |

### 7B.4. Kirim & monitor

1. Klik **Kirim Broadcast (N nomor)**. Kalau N ≥ 100, dashboard tampilkan konfirmasi dengan estimasi waktu.
2. Setelah klik, broadcast langsung jalan di **background** — Anda boleh tutup tab browser, broadcast tetap berjalan di server.
3. Banner biru di atas form akan tampil dengan **progress bar live** (refresh tiap 3 detik): `X terkirim · Y gagal · dari N total`.
4. Klik **Hentikan Broadcast** untuk cancel (pesan yang sudah terkirim tidak bisa di-undo).
5. Setelah selesai, broadcast pindah ke tabel **Riwayat Broadcast** di bawah.

### 7B.5. Inspect detail per penerima

Klik ikon **list** di kolom Aksi pada baris broadcast. Modal muncul dengan filter Status (Semua / Pending / Terkirim / Gagal). Penerima yang gagal kelihatan error message-nya — biasanya:

- `recipient not on WhatsApp` — nomor tidak terdaftar di WA.
- `rate limited` — kena throttle WhatsApp; coba lagi dengan delay lebih panjang.
- `not connected` — device disconnect saat broadcast.

### 7B.6. Tips menghindari banned WhatsApp

- **Jangan kirim ke nomor yang tidak pernah chat duluan**. Banyak "unknown number" = trigger antispam WA paling cepat.
- **Random delay wajib**. Backend dashboard memaksa min 3 detik antar pesan, tapi 8-25 detik jauh lebih aman.
- **Variasikan pesan** dengan spintax. Pesan identik ke ratusan nomor = pattern detection langsung kena.
- **Batas aman tidak resmi**:
  - Nomor WA baru: ≤200 pesan/hari, ≤50 ke nomor unknown.
  - Nomor WA lama (>3 bulan aktif): ≤500 pesan/hari.
  - Nomor WA Business verified: lebih tinggi, tapi tetap hati-hati.
- **Hindari URL pendek/suspicious**. WhatsApp scan link, kalau ke domain bermasalah pesan di-shadow ban.
- **Untuk produksi serius**: pakai [WhatsApp Business API resmi](https://business.whatsapp.com/products/business-platform) (Meta) — biaya per pesan, tapi tidak ada risiko banned.

---

## 7C. Status koneksi ke Core API (badge di kanan atas dashboard)

Di pojok kanan atas dashboard ada **pill berwarna** yang menunjukkan status real-time ke Core API. Pill ini auto-poll tiap **30 detik**, jadi tidak perlu refresh halaman. Hover untuk tooltip detail (URL, latency, timestamp last check, error message kalau ada). Klik pill untuk re-check langsung.

| State | Warna | Arti |
|---|---|---|
| **API Core Connected** | hijau ✓ | Core merespons normal. Latency dalam ms ditampilkan di samping label. |
| **Mengecek API Core...** | abu-abu (spinner) | Sedang ping core. Tampil saat awal load atau setelah klik re-check. |
| **API Core Disconnected** | merah ✗ | Core tidak respond / network error / 5xx. Hover untuk lihat error message. |

> Kalau pill merah terus, biasanya: container `gowa-core` mati (`docker compose ... ps`) atau `WHATSAPP_API_URL` di `dashboard/.env` salah arah (harus `http://whatsapp_go:3000` di Docker).

---

## 8. Cara melihat riwayat pengiriman

Klik tab **Riwayat**. Anda akan lihat daftar semua pengiriman otomatis terakhir — sukses (hijau) atau gagal (merah). Kalau gagal, kolom "Detail" akan menampilkan penyebabnya (misal nomor salah, device disconnect, dll).

Untuk riwayat **per jadwal**: di tab Jadwal, klik ikon 📃 di baris jadwal.

### 8.1. Panel "Auto-Delete Logs" (di atas tabel Riwayat)

Untuk jaga ukuran `dashboard.db` tidak terus membengkak, dashboard punya **auto-cleanup** yang berjalan otomatis di background. Klik tombol ikon **database** di kanan atas tab Riwayat untuk lihat panel info, atau tombol ↻ di dalam panel untuk reload.

Panel menampilkan:

- **Status auto-cleanup** (AKTIF/NONAKTIF) — tergantung env `DASHBOARD_LOG_RETENTION_DAYS`.
- **Retention period** — default 30 hari. Log lebih lama dari ini di-hapus dari tanggal paling lama.
- **Row count** untuk setiap tabel: `schedules`, `schedule_logs`, `broadcasts`, `broadcast_recipients`.

Tombol-tombol:

- **Jalankan Cleanup Sekarang** — manual trigger pakai retention default.
- Dropdown **7 / 14 / 30 / 60 / 90 / 180 / 365 hari** + tombol **Hapus > N hari** — manual override untuk pembersihan agresif (mis. mau bersihkan ke 7 hari saja).

Cleanup yang dihapus:

- `schedule_logs.ran_at < cutoff` (semua log eksekusi jadwal lama).
- `broadcasts.finished_at < cutoff` (broadcasts yang sudah selesai — completed/cancelled/failed). Broadcast yg masih **running** atau **pending** TIDAK pernah dihapus apa pun umurnya.
- `broadcast_recipients` ikut terhapus otomatis (FK CASCADE).
- Kalau > 500 row dihapus dalam satu sweep, otomatis `VACUUM` untuk reclaim disk space.

### 8.2. Konfigurasi retention via .env

Di `dashboard/.env`:

```env
DASHBOARD_LOG_RETENTION_DAYS=30      # 0 = nonaktif (UI manual cleanup tetap bisa)
DASHBOARD_CLEANUP_INTERVAL_HOURS=6   # interval background worker (min 1)
```

Setelah ubah:

```bash
docker compose -f docker-compose.aapanel.yml restart dashboard
docker compose -f docker-compose.aapanel.yml logs --tail=20 dashboard | grep retention
# Harus muncul: [main] log retention enabled: 30 days, cleanup every 6 hour(s)
```

---

## 9. Cara upgrade ke versi terbaru

Salah satu keuntungan utama: **dashboard tidak menyentuh kode inti** sama sekali, jadi upgrade core aman. Caranya:

### 9.1. Backup dulu (jaga-jaga)

Tutup core dan dashboard (Ctrl+C di kedua jendela). Lalu copy seluruh folder aplikasi ke tempat lain sebagai cadangan. Misalnya copy `F:\KIRO\go-whatsapp-web-multidevice-main\` ke `F:\KIRO\BACKUP-2026-05-11\`.

**Yang penting di-backup**:
- Folder `src\storages\` — di sinilah session WhatsApp dan database disimpan. Kalau hilang, Anda harus scan QR ulang.
- File `dashboard\dashboard.db` — daftar semua jadwal Anda. Kalau hilang, jadwal harus dibuat ulang.
- File `.env` (di `src\` dan `dashboard\`) — pengaturan Anda.

### 9.2. Download versi baru

1. Download versi terbaru dari sumber resmi (GitHub repo aplikasi). Biasanya berupa file ZIP.
2. Ekstrak ke folder **baru**, misalnya `F:\KIRO\go-whatsapp-web-multidevice-NEW\`.

### 9.3. Pindahkan data lama ke versi baru

Dari folder lama ke folder baru, copy:

| Yang di-copy | Dari | Ke |
|---|---|---|
| Folder session WhatsApp | `lama\src\storages\` | `baru\src\storages\` |
| Folder dashboard (utuh) | `lama\dashboard\` | `baru\dashboard\` |
| File `.env` core (kalau ada) | `lama\src\.env` | `baru\src\.env` |

Setelah itu folder `baru\` sudah punya:
- ✅ Engine WhatsApp versi terbaru
- ✅ Session login lama (tidak perlu scan QR ulang)
- ✅ Dashboard + semua jadwal yang sudah Anda buat

### 9.4. Hapus folder lama, rename folder baru

Setelah dipastikan jalan normal, hapus folder lama dan rename `F:\KIRO\go-whatsapp-web-multidevice-NEW\` menjadi `F:\KIRO\go-whatsapp-web-multidevice-main\` (atau apa pun nama yang Anda pakai sehari-hari).

### 9.5. Jalankan lagi seperti biasa

```
# Jendela 1
cd F:\KIRO\go-whatsapp-web-multidevice-main\src
go run . rest

# Jendela 2
cd F:\KIRO\go-whatsapp-web-multidevice-main\dashboard
go run .
```

> **Catatan**: kalau setelah upgrade dashboard menampilkan error "API not connected", coba reload halaman browser. Kalau masih error, pastikan jendela 1 (core) sudah jalan dan tidak ada error merah.

---

## 10. Kalau ada masalah (troubleshooting)

### ❌ "go is not recognized" di Command Prompt

Berarti Go belum terinstall atau Windows belum baca. Restart komputer dan coba lagi. Kalau masih, install ulang Go (langkah 2.2).

### ❌ Dashboard kosong / tab Devices tidak menampilkan apa-apa

- Pastikan jendela 1 (core) **masih berjalan**.
- Pastikan di jendela 1 sudah tulis `Listening on 0.0.0.0:3000`.
- Klik tombol **Reload** di tab Devices.
- Kalau masih kosong, buka `http://localhost:3000` di browser baru — kalau halaman tidak terbuka, berarti core tidak jalan.

### ❌ "QR code tidak muncul" saat tambah device

- Pastikan di jendela 1 (core) tidak ada tulisan merah.
- Coba refresh halaman `http://localhost:3000`.
- Kalau masih, coba klik logout lalu login lagi.

### ❌ Pesan terjadwal tidak terkirim pada waktunya

- Pastikan **kedua jendela** (core + dashboard) tetap terbuka saat waktu yang dijadwalkan tiba. Kalau komputer dimatikan atau Command Prompt ditutup, jadwal **tidak akan jalan**.
- Cek tab **Riwayat** — apakah ada log error?
- Cek jadwal: pastikan statusnya "aktif" (bukan "jeda"), device terpilih benar, dan nomor penerima sudah dengan kode negara (62 untuk Indonesia).
- Cek **timezone**: kalau Anda di Indonesia tapi timezone-nya `UTC`, jadwalnya akan meleset 7 jam. Edit jadwal → ubah timezone ke `Asia/Jakarta`.

### ❌ Pesan ke nomor yang tidak punya WhatsApp

Otomatis gagal — di kolom Detail di tab Riwayat akan tertulis. Pastikan nomor target memang aktif di WhatsApp.

### ❌ Komputer harus selalu nyala?

**Ya** — supaya jadwal jalan, kedua program harus tetap aktif. Solusi:

- Pakai komputer yang memang selalu hidup (server kecil, mini PC, dll).
- Atau pakai komputer biasa, tapi pastikan tidak sleep/hibernate. Tutup display saja boleh, asal CPU jalan.
- Untuk pemakaian lebih serius: pertimbangkan VPS (Virtual Private Server) — di luar lingkup panduan ini.

### ❌ Lupa cara membuka dashboard

Cukup ingat dua hal:
- Folder aplikasi: `F:\KIRO\go-whatsapp-web-multidevice-main\`
- Alamat dashboard di browser: `http://localhost:8088`
- Alamat core (scan QR): `http://localhost:3000`

### Butuh bantuan lebih lanjut?

Catat **pesan error tepat** yang muncul (screenshot atau salin teksnya), lalu tanyakan ke teknisi / komunitas pengguna aplikasi ini. Dengan pesan error yang jelas, biasanya masalahnya cepat ditemukan.

---

## 11. Cara install pakai Docker (container)

Cara ini **lebih mudah dirawat** karena Anda **tidak perlu install Go** atau apa pun di komputer — semua kebutuhan sudah dibungkus di dalam "container". Cocok untuk dipasang di:

- VPS / cloud server (DigitalOcean, AWS, Hetzner, dll), supaya bisa jalan 24 jam.
- Komputer pribadi yang sudah punya **Docker Desktop**.
- NAS Synology / mini PC yang mendukung Docker.

### 11.1. Yang dibutuhkan

| Kebutuhan | Cara install |
|---|---|
| **Docker Engine + Docker Compose** | Windows / Mac: install [Docker Desktop](https://www.docker.com/products/docker-desktop/). Linux: ikuti [panduan resmi](https://docs.docker.com/engine/install/). |
| Internet (sekali, untuk build) | Untuk download dependency saat pertama kali build. |

**Cara cek Docker sudah terinstall:**

```
docker --version
docker compose version
```

Kalau dua-duanya menampilkan versinya, berarti sudah siap.

### 11.2. File yang sudah disiapkan

Di folder aplikasi sudah ada tiga file penting untuk Docker:

| File | Fungsinya |
|---|---|
| `docker/golang.Dockerfile` | Resep build container untuk **core** (WhatsApp engine). Sudah ada dari awal. |
| `dashboard/Dockerfile` | Resep build container untuk **dashboard**. (File baru, **tidak menyentuh core**.) |
| `docker-compose.full.yml` | File "orkestrator" yang menjalankan **kedua container sekaligus** dan menghubungkannya. (File baru, terpisah dari `docker-compose.yml` asli.) |

> Catatan: `docker-compose.yml` asli (yang hanya menjalankan core) **tetap ada dan tidak diubah**. Anda bisa pilih mau pakai yang mana.

### 11.3. Langkah instalasi pertama kali

1. **Buka terminal** (Command Prompt / PowerShell / Terminal) di folder utama aplikasi:

   ```
   cd F:\KIRO\go-whatsapp-web-multidevice-main
   ```

2. **Siapkan file `.env`** (sekali saja, sebelum build):

   Untuk core:
   ```
   copy src\.env.example src\.env
   ```
   *(Kalau file `src\.env.example` tidak ada, Anda bisa skip langkah ini — core akan jalan dengan default.)*

   Untuk dashboard:
   ```
   copy dashboard\.env.example dashboard\.env
   ```

   (Boleh diedit Notepad kalau mau ubah port atau timezone.)

3. **Build & jalankan kedua container** dengan satu perintah:

   ```
   docker compose -f docker-compose.full.yml up -d --build
   ```

   Penjelasan perintahnya:
   - `-f docker-compose.full.yml` → pakai file orkestrator yang baru (bukan yang asli).
   - `up` → nyalakan service.
   - `-d` → jalan di latar belakang (detached), Anda bebas tutup terminal.
   - `--build` → build image dulu sebelum jalan (perlu untuk pertama kali atau setelah perubahan kode).

   Proses pertama kali butuh **3–10 menit** (download base image, build Go binary). Setelah selesai akan muncul tulisan seperti:

   ```
   ✔ Container go-whatsapp-web-multidevice-main-whatsapp_go-1   Started
   ✔ Container go-whatsapp-web-multidevice-main-dashboard-1     Started
   ```

4. **Akses aplikasi** di browser:
   - Dashboard: [http://localhost:8088](http://localhost:8088)
   - Halaman QR core (untuk scan device): [http://localhost:3000](http://localhost:3000)

   *(Kalau Docker di-install di server jarak jauh, ganti `localhost` dengan IP atau domain server.)*

### 11.4. Perintah Docker sehari-hari (cheat sheet)

Jalankan dari folder utama aplikasi.

| Yang ingin dilakukan | Perintah |
|---|---|
| Lihat status semua container | `docker compose -f docker-compose.full.yml ps` |
| Lihat log core (live) | `docker compose -f docker-compose.full.yml logs -f whatsapp_go` |
| Lihat log dashboard (live) | `docker compose -f docker-compose.full.yml logs -f dashboard` |
| Stop semua container | `docker compose -f docker-compose.full.yml stop` |
| Mulai lagi (tanpa rebuild) | `docker compose -f docker-compose.full.yml start` |
| Restart semua | `docker compose -f docker-compose.full.yml restart` |
| Stop + hapus container (data **tetap aman**) | `docker compose -f docker-compose.full.yml down` |

> Tips: kalau bosan mengetik `-f docker-compose.full.yml` berulang-ulang, rename file `docker-compose.yml` asli jadi `docker-compose.core-only.yml`, lalu rename `docker-compose.full.yml` jadi `docker-compose.yml`. Setelah itu cukup ketik `docker compose up -d --build`.

### 11.5. Di mana data tersimpan?

Walaupun aplikasi jalan di dalam container, **data Anda tersimpan di komputer host** (di folder yang sama dengan aplikasi), sehingga **aman walau container dihapus**:

| Folder | Isinya |
|---|---|
| `storages/` | Session login WhatsApp + chat history. **Jangan dihapus** kecuali ingin scan QR ulang. |
| `statics/` | Media yang diunduh dari WhatsApp (foto/video/dll). |
| `dashboard/data/` | File `dashboard.db` berisi semua jadwal & log. **Jangan dihapus.** |

### 11.6. Scan QR & pakai aplikasi

Sama persis dengan instalasi non-Docker:

1. Buka [http://localhost:3000](http://localhost:3000) → buat device → scan QR.
2. Buka [http://localhost:8088](http://localhost:8088) → klik **Reload** di tab Devices → mulai kirim pesan & buat jadwal.

Lihat [bagian 5–8 di atas](#5-cara-menambah-device-whatsapp-scan-qr) untuk detailnya.

### 11.7. Cara upgrade versi pakai Docker

Jauh lebih simpel dari instalasi manual:

1. **Backup dulu**: copy folder `storages/`, `statics/`, dan `dashboard/data/` ke tempat aman.

2. **Stop container yang lama:**

   ```
   docker compose -f docker-compose.full.yml down
   ```

3. **Update kode aplikasi:**
   - Kalau Anda meng-clone dari Git: `git pull`
   - Kalau download manual: hapus folder `src/` lama, ganti dengan `src/` dari versi baru. **Jangan sentuh** folder `dashboard/`, `storages/`, `statics/`, dan `dashboard/data/`.

4. **Build ulang & jalankan:**

   ```
   docker compose -f docker-compose.full.yml up -d --build
   ```

   Docker akan otomatis: build image baru, buang yang lama, jalankan dengan data yang **tetap utuh** (karena volume `storages/`, `statics/`, `dashboard/data/` tidak ikut terhapus).

5. **Verifikasi**: buka dashboard, cek device masih connected, jadwal masih ada.

### 11.8. Masalah umum di Docker

#### ❌ Port 3000 / 8088 sudah dipakai aplikasi lain

Edit `docker-compose.full.yml`, ubah bagian `ports:`. Contoh untuk pindah dashboard ke port 9090:

```yaml
dashboard:
  ports:
    - "9090:8088"   # angka kiri = port di host, angka kanan = di dalam container (jangan diubah)
```

Lalu `docker compose -f docker-compose.full.yml up -d`.

#### ❌ Dashboard tidak bisa connect ke core

Cek log: `docker compose -f docker-compose.full.yml logs dashboard`. Pastikan tulisan **WhatsApp API URL** menunjuk ke `http://whatsapp_go:3000` (bukan `localhost:3000`). Di dalam Docker network, container harus saling memanggil via service name.

#### ❌ "permission denied" di folder storages

Jalankan sekali di host (Linux/Mac):

```
sudo chown -R 20001:20000 storages statics dashboard/data
```

Di Windows biasanya tidak masalah karena Docker Desktop menangani permission otomatis.

#### ❌ Build gagal "go.sum not found"

Tidak masalah — Dockerfile dashboard sudah menangani kasus ini dengan menjalankan `go mod tidy` otomatis di dalam container. Build akan tetap berhasil.

#### ❌ Ingin lihat isi container untuk debug

```
docker compose -f docker-compose.full.yml exec dashboard sh
```

Ketik `exit` untuk keluar.

---

## 12. Cara install di aaPanel (anti-conflict dengan container lain)

Panduan ini cocok kalau server VPS Anda sudah pakai **aaPanel** dan **sudah ada container Docker lain** yang jalan (Nginx, MySQL, n8n, Portainer, dll). Setup di sini sengaja **memakai port tinggi + bind hanya ke localhost**, supaya tidak adu rebutan port dengan container lain. Lalu nanti diakses publik via **Nginx reverse proxy + SSL gratis** yang sudah disediakan aaPanel.

### 12.1. Prasyarat

| Yang perlu disiapkan | Cara |
|---|---|
| aaPanel sudah terinstall | (Biasanya sudah, kalau belum: [aapanel.com](https://www.aapanel.com)) |
| Modul **Docker** sudah aktif | aaPanel → App Store → cari "Docker" → Install. Otomatis install Docker + Docker Compose. |
| Domain/subdomain mengarah ke IP server | Misal `wa.namadomainsaya.com` → A record ke IP VPS. |

**Cek dulu Docker sudah jalan:** masuk **aaPanel → Terminal**, ketik:

```
docker --version
docker compose version
```

Dua-duanya harus tampil versinya.

### 12.2. Upload kode aplikasi ke server

Pilih salah satu cara:

**Cara A — Via Git (paling rapi, mudah upgrade):**

1. Buka aaPanel → **Terminal**.
2. Pindah ke folder web atau folder bebas pilihan Anda. Saya rekomendasikan `/www/wwwroot/` (sesuai standar aaPanel):

   ```
   cd /www/wwwroot/
   git clone <URL-REPO-ANDA> gowa
   cd gowa
   ```

3. Pastikan folder `dashboard/`, `docker-compose.aapanel.yml`, dan `src/` semua ada di sini.

**Cara B — Upload manual via File Manager:**

1. Compress folder `go-whatsapp-web-multidevice-main` di komputer lokal jadi `.zip`.
2. aaPanel → **File** → masuk `/www/wwwroot/` → klik **Upload** → pilih file zip.
3. Klik kanan file zip → **Unzip**. Rename folder hasilnya jadi `gowa` supaya pendek.

Hasil akhirnya: folder aplikasi ada di `/www/wwwroot/gowa/`.

### 12.3. Cek port mana yang sudah dipakai (penting!)

Di aaPanel **Terminal**:

```
ss -tlnp | grep -E ':(13000|18088)'
```

Kalau tidak ada output → port aman.
Kalau ada output → ganti port di langkah berikutnya.

> **Default yang saya pilih**: `13000` (untuk core) dan `18088` (untuk dashboard). Port tinggi seperti ini jarang dipakai. Kalau bentrok, ganti ke angka lain misal `13001`/`18089`.

**Kalau perlu ganti port**, edit file `docker-compose.aapanel.yml`. Cari bagian:

```yaml
ports:
  - "127.0.0.1:13000:3000"
```

Angka pertama (sebelum titik dua kedua) = port host. **Boleh diganti.** Angka kedua = port internal container, **JANGAN diganti.**

### 12.4. Build & jalankan dengan compose khusus aaPanel

Di aaPanel **Terminal**, dari folder aplikasi:

```
cd /www/wwwroot/gowa

# (opsional) bikin file .env kosong, supaya compose tidak warning
touch src/.env dashboard/.env

# build + jalankan
docker compose -f docker-compose.aapanel.yml up -d --build
```

Tunggu 3–10 menit untuk build pertama kali. Setelah selesai, cek statusnya:

```
docker compose -f docker-compose.aapanel.yml ps
```

Harusnya muncul **2 container**: `gowa-core` (Up) dan `gowa-dashboard` (Up).

**Cek aksesibilitas lokal di server:**

```
curl -I http://127.0.0.1:13000
curl -I http://127.0.0.1:18088
```

Dua-duanya harus return `HTTP/1.1 200 OK` (atau 302/404 — yang penting bukan "Connection refused").

### 12.5. Bikin domain & reverse proxy di aaPanel

Saatnya bikin URL publik yang cantik (`https://wa.namadomainsaya.com`) yang otomatis menghantar ke dashboard di port 18088.

> ⚠️ **Penting — baca dulu sebelum lanjut**: aaPanel UI "Add reverse proxy" punya **dua bug** yang sering bikin "Tambah Device" gagal dengan `404 Cannot POST`:
>
> 1. Bisa tambah **trailing slash** di `proxy_pass http://127.0.0.1:18088/;` — bikin nginx me-rewrite URI.
> 2. Bisa naruh **target URL sebagai `Host` header** (`proxy_set_header Host http://127.0.0.1:18088;`) — padahal harusnya `$host`. Engine Fiber dashboard parse URL aneh karena Host malformed.
>
> Cuma satu dari dua sudah cukup bikin gagal. Kalau Anda tidak mau debug nginx sama sekali, **lewatin UI aaPanel dan pakai auto-installer di section 12.5.1**. Cuma satu perintah, langsung benar.

**Untuk dashboard (port 18088):**

1. aaPanel → menu kiri **Website** → klik **Add site**.
2. Isi:
   - **Domain**: misal `wa.namadomainsaya.com`
   - **Root directory**: biarkan default
   - **PHP version**: pilih **Pure static** (tidak butuh PHP)
3. Klik **Submit**.
4. **Skip tab "Reverse proxy" di UI aaPanel** — kita pasang config nginx sendiri yang sudah dijamin benar (lihat 12.5.1).
5. Aktifkan **SSL gratis**: di domain yang sama → tab **SSL** → **Let's Encrypt** → pilih domain → **Apply**. Tunggu sampai sukses. Aktifkan **Force HTTPS**.

> Kalau Anda tetap mau pakai UI "Add reverse proxy": isi **Target URL `http://127.0.0.1:18088` tanpa `/` di akhir**, submit, lalu buka tab **Config file**, dan **edit baris `proxy_set_header Host`** supaya nilainya `$host` (bukan `http://127.0.0.1:18088`). Atau jalanin auto-installer di 12.5.1 yang otomatis benerin dua-duanya.

### 12.5.1. Install config nginx yang sudah teruji (satu perintah)

Cara paling cepat & paling jaminan benar:

```bash
cd /www/wwwroot/gowa     # sesuaikan path repo Anda
chmod +x scripts/aapanel-install-nginx.sh
sudo sh scripts/aapanel-install-nginx.sh wa.namadomainsaya.com
```

Script ini akan:

1. Backup config lama (`.bak.YYYYMMDD-HHMMSS`).
2. Replace blok `location /` di config nginx aaPanel pakai blok yang sudah dijamin tanpa dua bug di atas — `proxy_pass` tanpa trailing slash + `Host $host` yang benar.
3. Validasi syntax + reload nginx.
4. Otomatis rollback ke backup kalau syntax invalid.

Buka browser → `https://wa.namadomainsaya.com` → dashboard muncul dengan SSL. 🎉

### 12.5.2. Verifikasi setup end-to-end (wajib dijalankan setelah deploy)

Setelah reverse proxy + SSL aktif, jalankan script verifikasi di Terminal aaPanel:

```bash
cd /www/wwwroot/gowa     # sesuaikan kalau lokasinya lain
sh scripts/aapanel-check.sh https://wa.namadomainsaya.com
```

Kalau dashboard pakai basic auth (`DASHBOARD_BASIC_AUTH` di `dashboard/.env`), tambahkan kredensial:

```bash
AUTH=user:pass sh scripts/aapanel-check.sh https://wa.namadomainsaya.com
```

Script ini cek 3 hal:
- Kedua container running.
- Dashboard merespons benar di `127.0.0.1:18088` (bypass nginx).
- Public URL Anda merespons identik — termasuk **POST request** yang sensitif ke trailing-slash bug.

Kalau muncul **"deteksi: nginx reverse proxy aaPanel salah konfigurasi"**, lanjut ke 12.5.3.

### 12.5.3. Fix kalau POST gagal (Tambah Device 404)

**Cara tercepat — pakai auto-installer:**

```bash
sudo sh scripts/aapanel-install-nginx.sh wa.namadomainsaya.com
sh scripts/aapanel-check.sh https://wa.namadomainsaya.com    # verify
```

**Atau manual via aaPanel UI:**

1. **Website → klik domain → tab Config file**.
2. Cari blok `location /` atau `location ^~ /` yang ada `proxy_pass`.
3. Pastikan dua hal:

   ```nginx
   # ✓ BENAR
   proxy_pass http://127.0.0.1:18088;             # tanpa / di akhir
   proxy_set_header Host $host;                    # Host = hostname klien
   ```

   ```nginx
   # ✗ SALAH (default aaPanel UI sering kayak gini)
   proxy_pass http://127.0.0.1:18088/;            # ada / -> path ke-strip
   proxy_set_header Host http://127.0.0.1:18088;  # Host malformed -> Fiber gagal route
   ```

4. Klik **Save**. aaPanel reload nginx otomatis.
5. Re-run `sh scripts/aapanel-check.sh https://wa.namadomainsaya.com` — harus semua hijau.

> **Kenapa dua-duanya jadi masalah?**
>
> - **Trailing slash di `proxy_pass`**: dengan `/` di akhir, nginx me-rewrite URI yang match `location`-nya. Untuk `location /` + `proxy_pass http://host:port/`, hasilnya path kosong di upstream. Tanpa slash, URI dilewatkan apa adanya. Detail: [nginx docs proxy_pass](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass).
> - **Host header salah**: kalau `Host` di-set ke URL upstream (mengandung `://` dan port), engine HTTP upstream (fasthttp di Fiber) parse-nya secara aneh karena bukan format Host yang valid (RFC 7230 §5.4 — Host = host[:port], TANPA scheme). Akibatnya routing internal mismatch dan request fallthrough ke 404.

**Untuk halaman scan QR (port 13000)** — boleh ditambahkan subdomain terpisah:

Ulangi langkah 1–5 di atas dengan domain `qr.namadomainsaya.com` dan target `http://127.0.0.1:13000`.

> **Lebih aman lagi**: di tab **Reverse proxy** halaman QR ini, klik tombol konfigurasi lalu tambahkan basic auth Nginx supaya tidak semua orang bisa akses halaman scan QR. Cari opsi **Configure file** → tambahkan di dalam block proxy:
>
> ```nginx
> auth_basic "QR Login";
> auth_basic_user_file /www/server/panel/data/htpasswd-qr;
> ```
>
> Lalu bikin file password-nya: `htpasswd -c /www/server/panel/data/htpasswd-qr admin`

### 12.6. Cek di Docker Manager aaPanel

aaPanel → **Docker** (di sidebar) → tab **Container**:
- Anda akan lihat 2 container baru: `gowa-core` dan `gowa-dashboard` dengan status **running** 🟢.
- Klik salah satu untuk lihat log, restart, atau buka shell.

Container lain yang sudah ada **tetap jalan normal**, tidak terganggu.

### 12.7. Cheat sheet di aaPanel Terminal

Selalu dari folder `/www/wwwroot/gowa`:

| Yang ingin dilakukan | Perintah |
|---|---|
| Lihat status | `docker compose -f docker-compose.aapanel.yml ps` |
| Lihat log core | `docker compose -f docker-compose.aapanel.yml logs -f --tail=100 whatsapp_go` |
| Lihat log dashboard | `docker compose -f docker-compose.aapanel.yml logs -f --tail=100 dashboard` |
| Restart semua | `docker compose -f docker-compose.aapanel.yml restart` |
| Stop semua | `docker compose -f docker-compose.aapanel.yml stop` |
| Stop + hapus container (data aman) | `docker compose -f docker-compose.aapanel.yml down` |
| Update kode + rebuild | `git pull && docker compose -f docker-compose.aapanel.yml up -d --build` |
| Install / re-install config nginx yang benar | `sudo sh scripts/aapanel-install-nginx.sh wa.domainsaya.com` |
| Verifikasi setup end-to-end | `sh scripts/aapanel-check.sh https://wa.domainsaya.com` |

### 12.8. Backup data di aaPanel

Data yang **wajib** di-backup berkala (cron schedule di aaPanel **Cron**):

- `/www/wwwroot/gowa/storages/` — session login WhatsApp + chat.
- `/www/wwwroot/gowa/dashboard/data/` — jadwal & log dashboard.

Contoh cron backup harian (aaPanel → **Cron** → Add task):

```
tar czf /www/backup/gowa-$(date +%Y%m%d).tar.gz -C /www/wwwroot/gowa storages dashboard/data
```

### 12.9. Masalah umum di aaPanel

#### ❌ "port is already allocated"

Berarti port 13000 atau 18088 ternyata sudah dipakai container lain. Cek:

```
docker ps --format "table {{.Names}}\t{{.Ports}}" | grep -E '(13000|18088)'
```

Edit `docker-compose.aapanel.yml`, ganti angka port host (kiri) ke angka lain. Lalu:

```
docker compose -f docker-compose.aapanel.yml up -d
```

Update juga target URL di Reverse Proxy aaPanel agar match.

#### ❌ Domain sudah dibuka tapi 502 Bad Gateway

Container belum jalan atau port di reverse proxy salah. Cek:

```
curl -I http://127.0.0.1:18088
```

Kalau "Connection refused" → container mati, jalankan lagi `docker compose -f docker-compose.aapanel.yml up -d`. Kalau 200 → masalahnya di setting reverse proxy aaPanel, cek angka port-nya.

#### ❌ "Tambah Device" gagal: `Request failed with status code 404` / `Cannot POST` / `Cannot use import statement outside a module`

Tiga gejala ini biasanya bareng — penyebabnya **reverse proxy aaPanel salah**. Tiga kemungkinan:

1. **Target URL salah** — mis. masih nunjuk ke `127.0.0.1:13000` (core), bukan `18088` (dashboard). Konsekuensi: browser dapat HTML core yang pakai `<script type="module">`, browser tolak load karena MIME salah → "Cannot use import statement outside a module". Dan core tidak punya `/api/devices` → 404.

2. **Trailing slash di `proxy_pass`** — aaPanel kadang generate `proxy_pass http://127.0.0.1:18088/;` (dengan `/`). Ini bikin nginx rewrite URI sehingga POST `/api/devices` jadi path kosong di upstream → dashboard return `404 Cannot POST `.

3. **Host header salah** — aaPanel UI taruh **target URL** sebagai nilai `Host` header (`proxy_set_header Host http://127.0.0.1:18088;`), padahal harusnya `$host`. Fasthttp di dashboard parse URL aneh karena Host malformed → routing miss → 404.

Auto-fix (perbaiki ketiganya sekaligus):

```bash
cd /www/wwwroot/gowa
sudo sh scripts/aapanel-install-nginx.sh wa.namadomainsaya.com
sh scripts/aapanel-check.sh https://wa.namadomainsaya.com    # verify
```

Manual: ikuti [section 12.5.3](#1253-fix-kalau-post-gagal-tambah-device-404) — perhatikan dua-duanya, `proxy_pass` tanpa slash DAN `Host $host`.

#### ❌ Container Docker lain jadi error setelah saya install gowa

Tidak mungkin terjadi dengan compose ini — karena saya pakai **network terpisah** (`gowa-private`), **container name unik** (`gowa-*`), dan **port loopback only**. Kalau memang terjadi, kemungkinan koincidensi (mis. resource server penuh). Cek RAM dan disk dengan `free -h` dan `df -h` di Terminal.

#### ❌ Container dashboard error: "unable to open database file: out of memory (14)"

Pesan SQLite "out of memory (14)" sebenarnya **menyesatkan** — itu kode `SQLITE_CANTOPEN`, artinya **file database tidak bisa dibuat karena permission folder `/data`**. Penyebabnya: folder `dashboard/data` di host masih root-owned, sementara container jalan sebagai user uid 20001.

**Solusi (jalankan di Terminal aaPanel)**:

```
cd /www/wwwroot/gowa
docker compose -f docker-compose.aapanel.yml down
mkdir -p dashboard/data
chown -R 20001:20000 dashboard/data
docker compose -f docker-compose.aapanel.yml up -d --build
```

Versi Dockerfile terbaru sudah otomatis chown lewat `entrypoint.sh`, jadi pastikan Anda **rebuild image** dengan `--build` di akhir. Setelah rebuild, masalah ini tidak akan terulang.

#### ❌ "AI_ENCRYPTION_KEY must be hex" / "encoding/hex: invalid byte"

Error ini muncul kalau `AI_ENCRYPTION_KEY` di core tidak valid hex 64-karakter. Sering kejadian saat user salah set passphrase manual (mis. `AI_ENCRYPTION_KEY=my-secret-password`) di `src/.env`. AES-GCM butuh **persis 64 karakter dari 0-9 dan a-f**.

##### Priority resolution di entrypoint (baru, lebih aman)

Mulai dari versi entrypoint terbaru, urutan prioritas key adalah:

1. **Keyfile** `storages/.ai-encryption-key` — **paling tinggi**. Ini yang dipakai untuk encrypt API key provider yang sudah tersimpan di SQLite, jadi keyfile adalah "source of truth" supaya data lama tidak corrupt.
2. **Env var** `AI_ENCRYPTION_KEY` di `src/.env` — cuma dipakai sebagai **bootstrap awal** (kalau keyfile belum ada). Pertama kali boot, env value disimpan ke keyfile, lalu di boot berikutnya keyfile yang menang.
3. **Auto-generate** — fallback kalau keduanya kosong/invalid.

**Konsekuensi praktis:**

- Kalau Anda set env var, lalu boot pertama → env dipakai DAN otomatis copy ke keyfile. Aman.
- Kalau Anda ganti env var di boot berikutnya → **diabaikan**. Keyfile menang. Log akan munculkan `NOTE: AI_ENCRYPTION_KEY env var ≠ keyfile content. Pakai keyfile...`.
- Mau **rotate key** beneran (mis. key bocor)? Harus eksplisit:
  ```bash
  docker compose -f docker-compose.aapanel.yml stop whatsapp_go
  rm storages/.ai-encryption-key
  # set AI_ENCRYPTION_KEY baru di src/.env (atau biarkan kosong supaya auto-gen)
  docker compose -f docker-compose.aapanel.yml up -d whatsapp_go
  ```
  **Catatan**: rotate = semua API key provider yang sudah tersimpan di SQLite jadi unreadable. User harus input ulang via dashboard AI Reply tab.

##### Fix tercepat kalau Anda kena error hex sekarang

Biarkan entrypoint auto-generate (recommended):

```bash
cd /www/wwwroot/gowa.hafizridha.com/gowa-dashboard-main

# 1. Hapus / kosongkan baris AI_ENCRYPTION_KEY di src/.env (entrypoint akan handle)
sed -i 's|^AI_ENCRYPTION_KEY=.*|AI_ENCRYPTION_KEY=|' src/.env

# 2. Rebuild & restart core supaya entrypoint baru jalan
docker compose -f docker-compose.aapanel.yml up -d --build whatsapp_go

# 3. Verifikasi entrypoint pakai key valid (cek log)
docker compose -f docker-compose.aapanel.yml logs --tail=20 whatsapp_go | grep -i AI_ENCRYPTION
# Harus muncul salah satu dari ini:
#   [entrypoint] Generated new AI_ENCRYPTION_KEY -> /app/storages/.ai-encryption-key
#   [entrypoint] AI_ENCRYPTION_KEY: abcd…1234 (64 hex chars, source: generated)
# atau (kalau keyfile sudah ada dari boot sebelumnya):
#   [entrypoint] AI_ENCRYPTION_KEY: abcd…1234 (64 hex chars, source: keyfile)

# 4. Backup keyfile — kalau hilang, semua API key tersimpan jadi unreadable
cp storages/.ai-encryption-key ~/gowa-ai-key-$(date +%Y%m%d).bak
chmod 600 ~/gowa-ai-key-*.bak
```

##### API key provider (form) vs encryption key (env/keyfile) — beda

Untuk menghindari kebingungan: **dua key yang berbeda**:

| Yang mana | Format | Sumber | Fungsi |
|---|---|---|---|
| `AI_ENCRYPTION_KEY` | 64-char hex | `src/.env` atau auto-gen di keyfile | Master key AES-GCM untuk encrypt-at-rest provider key di SQLite. Set sekali, jangan diutak-atik. |
| `api_key` (form di tab AI Reply) | provider-specific (mis. `sk-...` untuk OpenAI, `sk-ant-...` untuk Claude) | User input via dashboard form | Authentication ke LLM provider. Bisa di-update kapan saja via form. |

**Behavior form api_key:**

- Field kosong saat save → core **pertahankan** key yang sudah tersimpan (indicator masked `sk-v****Aw5A` di samping label menunjukkan key tersimpan).
- Field terisi saat save → core **ganti** dengan nilai baru, encrypt pakai `AI_ENCRYPTION_KEY` saat ini, simpan ke DB.
- Indicator masked otomatis update setelah save sukses.

Dashboard juga sekarang detect error pattern `AI_ENCRYPTION_KEY` dan tampilkan banner merah dengan instruksi fix, bukan toast generic.

#### ❌ Test Connection error: "upstream POST /aireply/config/test -> 502: config not set"

Endpoint Test di core membaca config dari **database**, bukan dari body request. Kalau Anda klik **Test Connection** sebelum pernah klik **Simpan**, core nolak dengan `config not set` karena memang belum ada row config untuk device tsb.

**Sudah ada auto-fix di dashboard** — klik Test Connection sekarang otomatis melakukan **save dulu** (ke device aktif) lalu test. Jadi alur:

1. Isi field provider, model, API key, dll.
2. Klik **Test Connection** → dashboard auto-save → kirim test request ke provider → tampilkan latensi + sample response.

Kalau masih dapat `config not set` setelah update dashboard:
- Pastikan **provider** dan **model** sudah terisi di form (Test akan refuse kalau dua field ini kosong).
- Cek log dashboard apakah auto-save gagal: `docker compose -f docker-compose.aapanel.yml logs --tail=30 dashboard`.
- Kalau auto-save dapat 503 `AI_REPLY_DISABLED`, ikuti instruksi di section troubleshoot AI Reply 404 di bawah.

#### 💡 Pause Semua Auto Reply (sementara, semua device)

Saat liburan / meeting / acara, Anda mungkin perlu **mematikan AI Reply + static auto-reply sementara** di seluruh device tanpa harus matikan via env var (yang butuh container restart).

Cara:

1. Buka tab **AI Reply** di dashboard. Banner kuning "Pause Semua Auto Reply" muncul di atas.
2. Pilih durasi di dropdown: 15 menit / 1 jam / 4 jam / 8 jam / 24 jam / **Indefinite**.
3. Klik **Pause [durasi]** → konfirmasi.
4. Banner berubah jadi **merah** dengan countdown sisa waktu + tombol **Resume Sekarang**.

Selama paused:
- AI Reply tidak fire di device manapun, untuk chat manapun.
- Static `WHATSAPP_AUTO_REPLY` juga **suppressed** (Service claim ownership via `IsPaused()` check).
- Pesan masuk tetap diterima & disimpan di chat history; cuma balasan otomatisnya yang ditahan.
- Klik **Resume Sekarang** untuk cabut pause kapan saja sebelum deadline.

Behavior aman:
- State pause disimpan **in-memory di core**. Container restart = auto-resume (sengaja, supaya tidak ada "pause selamanya yang terlupa").
- "Indefinite" sebenarnya 100 tahun (clamped) — kalau benar-benar perlu permanent disable, set `AI_REPLY_ENABLED=false` di `src/.env`.
- Polling pause status setiap 30 detik saat tab AI Reply terbuka — banner refresh otomatis kalau di-trigger dari client lain (mis. curl).

Dari curl:
```bash
# Pause 1 jam
curl -X POST -u hakz:ueuwop18cm \
  -H "X-Device-Id: device-alpha" -H "Content-Type: application/json" \
  -d '{"minutes":60}' \
  https://gowa.hafizridha.com/api/aireply/pause

# Cek status
curl -u hakz:ueuwop18cm -H "X-Device-Id: device-alpha" \
  https://gowa.hafizridha.com/api/aireply/pause-status
# → {"results":{"paused":true,"paused_until":"2026-05-16T15:30:00Z","remaining_seconds":3580}}

# Resume manual
curl -X POST -u hakz:ueuwop18cm -H "X-Device-Id: device-alpha" \
  https://gowa.hafizridha.com/api/aireply/resume
```

#### 💡 Disable AI untuk satu chat = silent total (tidak ada balasan apa-apa)

Mulai dari versi terbaru, chat-toggle punya **tiga state**:

| State | Behavior |
|---|---|
| **Tidak ada row** (chat tidak pernah didaftarkan di tab Chat Toggle) | AI Reply skip. Static auto-reply (`WHATSAPP_AUTO_REPLY` di `src/.env`) tetap fire. |
| **Row enabled = ON** | AI Reply jalan. Static auto-reply skip. |
| **Row enabled = OFF** | **AI dan static dua-duanya skip** — explicit opt-out. Pakai untuk silent total ke chat tertentu. |

Sebelumnya `OFF` cuma matikan AI tapi static `Auto reply message` tetap nyangkut — confusing. Sekarang `OFF` = "user explicitly told us no reply" sehingga dispatcher di [event_message_handler.go](src/infrastructure/whatsapp/event_message_handler.go) skip static.

Kalau Anda mau static auto-reply mati untuk SEMUA chat (bukan per-chat), kosongkan env:
```bash
# di src/.env
WHATSAPP_AUTO_REPLY=
# lalu
docker compose -f docker-compose.aapanel.yml restart whatsapp_go
```

#### 💡 Mau AI Auto-Reply jalan di semua nomor sekaligus

Sebelumnya AI Reply config & chat-toggle hanya tersimpan per-device (per nomor WhatsApp). Sekarang ada toggle **"Apply ke semua device terhubung"** di tab **AI Reply**:

1. **Config tab — toggle "Apply config & chat-toggles ke semua N device"** (recommended):
   Setelah edit provider/model/system prompt/dst, centang toggle kuning di bawah form. Klik **Simpan ke Semua Device** → dashboard akan **dua hal sekaligus**:
   - Save config (provider, model, API key, prompt) ke seluruh device logged-in
   - **Replicate semua chat-toggle** dari device sumber ke device lain
   
   Step kedua **wajib** supaya device 2-dst BENAR-BENAR balas otomatis. Tanpa chat-toggle row, core silent-skip (gating: "no chat-setting row for this chat" — lihat [CLAUDE.md](./CLAUDE.md)).

2. **Chat Toggle tab** — sama, ada toggle *"Apply ke semua N device terhubung"* untuk add/toggle satu chat JID di seluruh device sekaligus. Berguna untuk menambah chat baru tanpa harus pakai jalur Config sync.

Toggle ini cuma muncul kalau ada **≥ 2 device** logged-in (kalau cuma 1 device, tidak ada gunanya).

##### Kenapa device 2 tidak balas otomatis padahal "Apply to all" sudah dicentang?

**Cara tercepat: pakai Health Check** — di tab AI Reply → Config tab, klik tombol **"Cek Semua Device"** di panel "Multi-Device Health Check" (muncul kalau ≥2 device logged-in). Akan tampilkan tabel per-device dengan status: ✓ ready / no_config / no_api_key / no_chats. Setiap status punya hint actionable, jadi langsung tahu device mana yang misconfigured.

Gating AI Reply di core ada 5 layer ([CLAUDE.md](./CLAUDE.md) "AI Reply gating"). Yang paling sering kena di multi-device:

1. **Tidak ada chat-setting row untuk chat JID di device kedua**. Bug lama: Config apply-to-all hanya copy config, bukan chat-settings. **Sudah di-fix** — sekarang Config apply-to-all otomatis replicate chat-settings juga.
2. **API key encrypted empty di device kedua** (silent fail paling jahat). Core's `Decrypt([]byte{}) → ("", nil)` — tidak return error → service lanjut dengan empty key → LLM call 401 → log status `error` tanpa visible message. **Sudah di-fix dengan pre-flight check** + Health Check sekarang detect "no_api_key" status. Solusi: isi field API key sekali waktu apply-to-all.
3. **Rate limit 3s/chat hit**. Default rate limit `AI_RATE_LIMIT_SECONDS=3` per chat. Kalau pesan masuk berurutan cepat, ke-2 di-skip dengan status `rate_limited`. Cek tab AI Reply → Logs.

Health Check endpoint (untuk audit dari curl):

```bash
curl -u hakz:ueuwop18cm \
  -H "X-Device-Id: device-alpha" \
  https://gowa.hafizridha.com/api/aireply/multi-device-health
# Returns: {overall: "all_ready|partial|none_ready", ready_count, total_devices,
#           results: [{device_id, has_config, has_api_key, chat_enabled_count,
#                      provider, model, status, hint}, ...]}
```

Lihat juga alur lengkap di tab **AI Reply → Logs** — kalau device 2 ada event masuk tapi status `rate_limited`/`error` atau tidak ada log sama sekali, itu indikator masalahnya.

##### API key handling saat apply-to-all:
- Field API key **terisi** → key tersebut dipakai untuk semua device.
- Field API key **kosong** → tiap device mempertahankan key yg sudah tersimpan (gagal kalau device tsb belum pernah disimpan keynya).

##### Dari curl

```bash
# Config + chat-settings replication (default: with_chats=true)
curl -X POST https://gowa.hafizridha.com/api/aireply/config/apply-to-all \
  -u hakz:ueuwop18cm \
  -H "X-Device-Id: device-alpha" \
  -H "Content-Type: application/json" \
  -d '{"provider":"openai_compatible","model":"gpt-4o-mini","api_key":"sk-...",...}'

# Cuma config, skip chat-settings sync
curl -X POST "https://gowa.hafizridha.com/api/aireply/config/apply-to-all?with_chats=false" ...
```

Response body sekarang punya field `chat_sync` dengan ringkasan: `source_chats`, `target_devices`, `applied_ok`, `applied_fail`. Cek itu setelah panggil endpoint untuk verifikasi replikasi sukses.

#### ❌ Tab AI Reply error: "Gagal load AI config: upstream GET /aireply/config -> 404"

Itu **bukan** masalah reverse proxy — itu indikasi fitur AI Reply belum aktif di core. Dengan entrypoint baru, default seharusnya sudah `AI_REPLY_ENABLED=true` dan `AI_ENCRYPTION_KEY` auto-generated di `storages/.ai-encryption-key`. Kalau Anda masih dapat error ini, kemungkinan:

1. **Container core masih image lama** — rebuild:
   ```bash
   docker compose -f docker-compose.aapanel.yml up -d --build whatsapp_go
   ```

2. **`src/.env` Anda eksplisit set `AI_REPLY_ENABLED=false`** — edit dan ubah jadi `true` (atau hapus baris itu supaya entrypoint default kick-in), lalu restart:
   ```bash
   docker compose -f docker-compose.aapanel.yml restart whatsapp_go
   ```

3. **Cek key file ada di volume**:
   ```bash
   ls -la storages/.ai-encryption-key
   # kalau tidak ada, restart core supaya entrypoint generate ulang:
   docker compose -f docker-compose.aapanel.yml restart whatsapp_go
   docker compose -f docker-compose.aapanel.yml logs --tail=20 whatsapp_go | grep AI_ENCRYPTION
   ```

> **Penting backup**: file `storages/.ai-encryption-key` adalah **satu-satunya** cara men-decrypt API key provider yang tersimpan di SQLite. Hilang/diganti = semua API key tersimpan jadi unreadable. Backup terpisah!

Dashboard versi terbaru akan menampilkan **banner berwarna merah** dengan tombol "Sudah saya aktifkan, coba lagi" di tab AI Reply kalau core return `AI_REPLY_DISABLED`, jadi error UX-nya jauh lebih jelas dari sebelumnya yang cuma toast "Gagal load AI config".

#### ❌ Lupa password basic auth halaman QR

Reset:

```
htpasswd -c /www/server/panel/data/htpasswd-qr admin
```

Lalu reload nginx aaPanel via tombol UI atau `nginx -s reload`.

---

**Selamat memakai!** 🎉 Aplikasi ini dirancang supaya bisa Anda pakai sehari-hari tanpa harus jadi programmer. Kalau ada hal yang belum jelas di panduan ini, biasanya solusinya cuma sekali coba — jangan takut salah, file data Anda aman selama folder `storages\` dan `dashboard.db` tidak dihapus.
