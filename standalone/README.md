# GoWA Dashboard — Paket Standalone untuk aaPanel

Paket ini **berdiri sendiri sepenuhnya**. Tidak butuh gowa-core di server yang
sama, tidak butuh Go, tidak butuh Node.js, tidak butuh Composer. Isinya binary
Linux statis yang jalan di distro apa pun (Debian, Ubuntu, CentOS, Alma, Rocky,
Alpine) tanpa dependensi library.

Update gowa-core **tidak akan pernah** mengganggu dashboard ini — keduanya
terpisah total dan hanya berbicara lewat HTTP.

---

## Isi paket

```
standalone/
├── bin/
│   ├── whatsapp-dashboard-linux-amd64    # Intel/AMD 64-bit (paling umum)
│   └── whatsapp-dashboard-linux-arm64    # ARM 64-bit (Ampere, Graviton, dll)
├── bootstrap.sh              # installer satu perintah, ambil dari GitHub
├── install.sh                # installer utama (systemd + nginx + verifikasi)
├── setup-nginx.sh            # khusus set reverse proxy nginx aaPanel
├── uninstall.sh              # hapus (database bisa dipertahankan)
├── gowa-dashboard.service    # template unit systemd
├── .env.example              # template konfigurasi
├── nginx-aapanel.conf.example# blok nginx untuk ditempel manual (kalau perlu)
├── SHA256SUMS                # checksum binary (diverifikasi bootstrap.sh)
├── Dockerfile                # jalur Docker (alternatif)
├── docker-compose.yml        # jalur Docker (alternatif)
└── README.md                 # file ini
```

---

## Cara A — Satu perintah dari GitHub (paling cepat)

Tanpa upload file, tanpa git clone, tanpa build. Cukup buat site-nya dulu
(langkah 1 di Cara B), lalu jalankan di **Terminal aaPanel**:

```bash
curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh | sudo sh -s -- gowa.domainku.com
```

Ganti `gowa.domainku.com` dengan domain Anda. Tanpa argumen domain juga boleh —
nginx dilewati, dashboard tetap jalan di `127.0.0.1:18088`:

```bash
curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh | sudo sh
```

Yang dilakukan `bootstrap.sh`:

1. Mengunduh paket dari GitHub (satu arsip, snapshot konsisten)
2. Memilih binary sesuai arsitektur CPU
3. **Memverifikasi SHA256** binary sebelum dijalankan — penting karena
   perintah ini mengeksekusi kode dari internet
4. Menjalankan `install.sh` (lihat rinciannya di Cara B langkah 3)

Login dashboard dibuat otomatis dan ditampilkan di akhir. Untuk menentukan
sendiri, tambahkan `GOWA_BASIC_AUTH='user:password'` — lihat
[Login dashboard](#login-dashboard-basic-auth).

Memasang dari branch atau tag lain:

```bash
curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh \
  | sudo GOWA_REF=nama-branch sh -s -- gowa.domainku.com
```

> Kalau paket standalone belum ter-merge ke `main`, ganti `main` pada URL di
> atas dengan nama branch-nya, dan tambahkan `GOWA_REF=nama-branch`.

Upgrade ke versi terbaru: jalankan perintah yang sama lagi. `.env` dan
database tidak pernah ditimpa.

---

## Cara B — Upload paket manual

Kalau server tidak punya akses internet keluar, atau Anda ingin memeriksa
isi paket dulu sebelum menjalankannya.

### 1. Buat site di aaPanel

aaPanel → **Website** → **Add site**

- Domain: `gowa.domainku.com` (ganti sesuai milik Anda)
- PHP version: **Pure static**
- Sisanya biarkan default

> Kalau ingin HTTPS: setelah site dibuat, buka tab **SSL** → **Let's Encrypt** →
> Apply. Lakukan ini **sebelum** langkah 3 supaya uji otomatis di akhir
> installer bisa lolos.

### 2. Upload & ekstrak paket

Upload `gowa-dashboard-standalone.tar.gz` lewat aaPanel **Files**, misal ke
`/root`. Lalu buka **Terminal** di aaPanel:

```bash
cd /root
tar -xzf gowa-dashboard-standalone.tar.gz
cd standalone
```

### 3. Jalankan installer

```bash
sudo sh install.sh gowa.domainku.com
```

Installer akan:

1. Memeriksa arsitektur CPU dan memilih binary yang tepat
2. Memastikan port 18088 belum dipakai proses lain
3. Membuat user sistem `gowadash` (tanpa login, non-root)
4. Memasang binary ke `/opt/gowa-dashboard`
5. Membuat `.env` (**tidak menimpa** kalau sudah ada) dan memasang
   login dashboard — lihat [Login dashboard](#login-dashboard-basic-auth)
6. Memasang + menyalakan service systemd (auto-start saat reboot)
7. Menunggu sampai dashboard benar-benar menjawab HTTP
8. Menulis config nginx yang benar, `nginx -t`, lalu reload
9. Menguji lewat URL publik — **termasuk POST**, karena dua bug aaPanel
   di bawah hanya muncul pada POST

Kalau ada tahap yang gagal, installer berhenti dengan pesan spesifik dan
config nginx dikembalikan ke kondisi semula (ada backup `.bak.*`).

Tanpa argumen domain juga boleh — nginx dilewati, dashboard tetap jalan di
`127.0.0.1:18088`:

```bash
sudo sh install.sh
```

### 4. Hubungkan ke gowa-core

Buka `https://gowa.domainku.com` → tab **Pengaturan** → isi:

- **Core URL** — misal `http://127.0.0.1:3000` (core di server yang sama)
  atau `https://api.domainku.com` (core di server lain)
- **Username / Password** — hanya kalau core memakai `APP_BASIC_AUTH`

Klik **Simpan**. Badge di kanan atas berubah menjadi hijau
**"API Core Connected"**. Tidak perlu edit file atau restart apa pun —
perubahan langsung berlaku.

---

## Cara C — Install sebagai container Docker

> **Cara A dan B TIDAK membuat container Docker.** Keduanya memasang binary
> native + service systemd, jadi `docker ps` akan kosong — itu memang
> perilakunya, bukan kegagalan. Cek dengan `systemctl status gowa-dashboard`.
> Kalau Anda memang menginginkan container, pakai cara ini.

### Lewat one-liner

```bash
curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh \
  | sudo GOWA_MODE=docker sh -s -- gowa.domainku.com
```

Paket dipasang ke `/opt/gowa-dashboard-docker` (lokasi tetap, supaya database
di `./data` tidak ikut terhapus), image di-build, container dijalankan, lalu
nginx diatur. Skrip memastikan container benar-benar berstatus **running** —
kalau mati seketika, log-nya langsung ditampilkan.

### Atau manual dari paket

```bash
cd standalone
docker compose up -d --build
sudo sh setup-nginx.sh gowa.domainku.com 18088
```

Build cepat karena binary sudah ada — tidak ada kompilasi Go di server.

### Mengelola container

```bash
cd /opt/gowa-dashboard-docker
docker compose ps          # status
docker compose logs -f     # log berjalan
docker compose restart     # restart
docker compose down        # stop & hapus container (data tetap di ./data)
```

Container bernama `gowa-dashboard`, bind ke `127.0.0.1:18088` (tidak terbuka
langsung ke internet — akses lewat nginx).

### Kenapa binary dipilih saat container start, bukan saat build

Image menyertakan binary amd64 **dan** arm64; `docker-entrypoint.sh` memilih
sesuai `uname -m` saat container dijalankan.

Sebelumnya pemilihan dilakukan saat build lewat `ARG TARGETARCH`. Itu bug:
`TARGETARCH` hanya diisi otomatis oleh BuildKit/buildx. Dengan classic builder
— yang masih dipakai Docker Manager aaPanel dan `docker compose build` tanpa
BuildKit — nilainya kosong sehingga jatuh ke default `amd64`. Di server ARM,
container ter-build dengan binary amd64 lalu **langsung mati** dengan
`exec format error`, sehingga tampak seperti "container tidak terbuat".
Memilih saat runtime menghilangkan seluruh kelas masalah itu.

---

## Login dashboard (Basic Auth)

Installer **selalu** memasang login pada instalasi baru. Ini disengaja:
dashboard ini bisa mengirim pesan WhatsApp atas nama Anda, jadi membiarkannya
terbuka saat bisa diakses dari internet berisiko tinggi.

Ada tiga cara mengaturnya saat instalasi:

### 1. Tentukan sendiri lewat `GOWA_BASIC_AUTH`

```bash
curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh \
  | sudo GOWA_BASIC_AUTH='admin:RahasiaKuat123' sh -s -- gowa.domainku.com
```

Atau kalau memakai paket manual:

```bash
sudo GOWA_BASIC_AUTH='admin:RahasiaKuat123' sh install.sh gowa.domainku.com
```

Password boleh memuat `:` (pemisah hanya `:` pertama), juga boleh memuat
karakter seperti `| / & $ " '` — semuanya ditulis apa adanya ke `.env`.
Username tidak boleh memuat `:`.

### 2. Ditanyakan interaktif

Jalankan `install.sh` langsung dari terminal tanpa `GOWA_BASIC_AUTH`:

```bash
sudo sh install.sh gowa.domainku.com
```

Installer menanyakan username (default `admin`) dan password (input
disembunyikan, diminta dua kali). Kosongkan password untuk dibuat otomatis.

### 3. Dibuat otomatis (default)

Kalau tidak ada `GOWA_BASIC_AUTH` dan tidak ada terminal interaktif — misal
lewat `curl | sh` — installer membuat password acak 20 karakter dan
**menampilkannya sekali** di akhir:

```
==============================================
 LOGIN DASHBOARD — CATAT SEKARANG
==============================================
  Username : admin
  Password : Xy9Zq2Lm8Kt4Rw7Bn1Vc
```

Password juga tersimpan di `/opt/gowa-dashboard/.env` kalau terlewat dicatat.

### Sengaja tanpa login

Hanya kalau dashboard benar-benar tidak bisa dijangkau dari internet:

```bash
... | sudo GOWA_BASIC_AUTH=none sh -s -- gowa.domainku.com
```

### Mengganti login setelah terpasang

```bash
sudo nano /opt/gowa-dashboard/.env      # ubah baris DASHBOARD_BASIC_AUTH
sudo systemctl restart gowa-dashboard
```

Install ulang / upgrade **tidak** mengubah login yang sudah ada — `.env` tidak
pernah ditimpa.

> Verifikasi cepat: `curl -i https://gowa.domainku.com/api/_health` harus
> menjawab **401** kalau login aktif. Kalau menjawab 200 tanpa kredensial,
> berarti login belum aktif.

---

## Perintah harian

```bash
systemctl status gowa-dashboard      # status
systemctl restart gowa-dashboard     # restart
systemctl stop gowa-dashboard        # stop
journalctl -u gowa-dashboard -f      # lihat log berjalan
```

Upgrade ke versi baru: ekstrak paket baru, lalu jalankan `sudo sh install.sh`
lagi. Binary diganti; `.env` dan database **tidak** disentuh.

Hapus:

```bash
sudo sh uninstall.sh            # hapus service+binary, data dipertahankan
sudo sh uninstall.sh --purge    # hapus semuanya termasuk database
```

---

## Kalau ada masalah

### Halaman terbuka, tapi "Tambah Device" gagal / 404

Ini **bug config nginx aaPanel**, bukan bug dashboard. Uji:

```bash
curl -i -X POST -H 'Content-Type: application/json' -d '{}' \
     https://gowa.domainku.com/api/_cleanup
```

- Dapat **200 / 400 / 401** → routing benar, masalahnya di tempat lain
- Dapat **404** → config nginx masih salah, jalankan:

```bash
sudo sh setup-nginx.sh gowa.domainku.com
```

Penyebabnya salah satu dari dua bug UI "Add Reverse Proxy" aaPanel:

| Bug | Config salah dari aaPanel | Yang benar |
|-----|---------------------------|------------|
| 1 | `proxy_pass http://127.0.0.1:18088/;` (ada `/` di akhir) | `proxy_pass http://127.0.0.1:18088;` |
| 2 | `proxy_set_header Host http://127.0.0.1:18088;` | `proxy_set_header Host $host;` |

Bug 1: trailing slash membuat nginx me-rewrite URI jadi kosong, sehingga
`POST /api/devices` sampai ke server sebagai path kosong.
Bug 2: `Host` diisi URL upstream padahal harus hostname klien — RFC 7230 §5.4.
Satu saja dari keduanya sudah cukup membuat dashboard tidak berfungsi.

> **Kenapa harus diuji dengan POST?** GET sering tetap jalan meski config
> salah, jadi bug ini lolos kalau hanya membuka halaman di browser.

### Browser: "Cannot use import statement outside a module"

Reverse proxy nyasar ke **gowa-core** (yang menyajikan ES module Vue), bukan ke
dashboard. Pastikan `proxy_pass` menunjuk port dashboard (`18088`), bukan port
core (`3000`). Jalankan `sudo sh setup-nginx.sh DOMAIN` untuk memperbaiki.

### Container Docker tidak ada / `docker ps` kosong

Cek dulu Anda memakai cara yang mana:

```bash
systemctl status gowa-dashboard     # Cara A/B (systemd) — ini yang default
docker ps -a | grep gowa-dashboard  # Cara C (Docker)
```

- **`systemctl` menunjukkan `active (running)`** → dashboard **sudah jalan
  normal** sebagai service systemd. Memang tidak ada container: Cara A dan B
  tidak memakai Docker sama sekali. Tidak ada yang perlu diperbaiki.
- **Ingin container** → pasang ulang dengan `GOWA_MODE=docker` (lihat Cara C).
- **Container ada di `docker ps -a` tapi tidak di `docker ps`** → container
  terbuat lalu mati. Lihat sebabnya:

```bash
cd /opt/gowa-dashboard-docker && docker compose logs --tail=50
```

  `exec format error` berarti arsitektur binary tidak cocok — rebuild tanpa
  cache: `docker compose build --no-cache && docker compose up -d`.

### Service tidak mau start

```bash
journalctl -u gowa-dashboard -n 50 --no-pager
```

Penyebab yang paling sering:

| Pesan log | Sebab & solusi |
|-----------|----------------|
| `address already in use` | Port 18088 dipakai proses lain. Cek: `ss -ltnp \| grep 18088`. Hentikan proses itu atau ubah `DASHBOARD_PORT` di `.env` **dan** port di config nginx. |
| `unable to open database file` | Folder data bermasalah. Perbaiki: `sudo chown -R gowadash:gowadash /opt/gowa-dashboard` |
| `permission denied` | Sama seperti di atas. |

### Lupa password login dashboard

Password tersimpan apa adanya di `.env`, jadi bisa dilihat kembali:

```bash
sudo grep DASHBOARD_BASIC_AUTH /opt/gowa-dashboard/.env
```

Ganti dengan yang baru:

```bash
sudo sed -i 's|^DASHBOARD_BASIC_AUTH=.*|DASHBOARD_BASIC_AUTH=admin:PasswordBaru|' \
     /opt/gowa-dashboard/.env
sudo systemctl restart gowa-dashboard
```

### Browser terus meminta login / login ditolak terus

- Pastikan tidak ada spasi tak sengaja di sekitar `:` pada `.env`
  (formatnya harus `user:password`, tanpa spasi)
- Username tidak boleh memuat `:` — hanya password yang boleh
- Setelah mengubah `.env`, service **wajib** di-restart:
  `sudo systemctl restart gowa-dashboard`
- Cek nilai yang benar-benar terbaca: `sudo systemctl show gowa-dashboard | grep -i basic`

### Badge "API Core Disconnected" (merah)

Dashboard sehat, tapi tidak bisa menghubungi core. Periksa:

1. Core benar-benar jalan: `curl -i http://127.0.0.1:3000/app/devices`
2. Core URL di tab **Pengaturan** sudah benar (termasuk `http://` atau `https://`)
3. Kalau core pakai basic auth, username/password sudah diisi
4. Kalau core di server lain: firewall mengizinkan koneksi dari server ini
5. Arahkan mouse ke badge — tooltip menampilkan pesan error persisnya

### Ingin mengembalikan config nginx

Setiap perubahan membuat backup:

```bash
ls /www/server/panel/vhost/nginx/*.bak.*
sudo cp /www/server/panel/vhost/nginx/DOMAIN.conf.bak.YYYYMMDD-HHMMSS \
        /www/server/panel/vhost/nginx/DOMAIN.conf
sudo nginx -t && sudo nginx -s reload
```

---

## Catatan teknis

- **Port**: dashboard `18088` (loopback), core `3000`. Angka berbeda supaya
  tidak bentrok dan mudah dibedakan saat debug.
- **Database**: SQLite di `/opt/gowa-dashboard/data/dashboard.db`
  (pure-Go, tanpa CGO). Isinya jadwal, broadcast, log eksekusi, dan setting
  koneksi core. Backup = cukup salin file ini (sertakan `-wal` dan `-shm`
  kalau ada, atau stop service dulu supaya konsisten).
- **Password core** disimpan apa adanya di `dashboard.db` (sama seperti seluruh
  isi database itu yang juga tidak terenkripsi) dan **selalu di-mask** di
  respons API maupun UI. Amankan file DB-nya lewat permission — sudah diatur
  agar hanya `gowadash` yang bisa membacanya.
- **Service hardening**: systemd unit memakai `ProtectSystem=strict`,
  `NoNewPrivileges`, `PrivateTmp`, dan hanya `data/` yang writable.
- **Konfigurasi core**: nilai `WHATSAPP_API_*` di `.env` hanya dipakai sebagai
  nilai awal saat boot pertama. Setelah disimpan lewat tab **Pengaturan**,
  nilai di database yang menang.
