#!/bin/sh
# =====================================================================
# GoWA Dashboard — installer satu perintah, ambil langsung dari GitHub
# =====================================================================
#
# Tidak perlu upload apa pun, tidak perlu git clone, tidak perlu build.
# Semua diunduh dari GitHub lalu diverifikasi checksum-nya.
#
# PAKAI (jalankan di Terminal aaPanel sebagai root):
#
#   curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh | sudo sh -s -- gowa.domainku.com
#
# Tanpa domain (nginx dilewati, dashboard tetap jalan di 127.0.0.1:18088):
#
#   curl -fsSL https://raw.githubusercontent.com/hafiz-ridha/gowa-dashboard/main/standalone/bootstrap.sh | sudo sh
#
# Pilih branch/tag lain lewat GOWA_REF:
#
#   curl -fsSL .../bootstrap.sh | sudo GOWA_REF=v1.2.0 sh -s -- gowa.domainku.com
#
# LOGIN DASHBOARD (Basic Auth):
#
#   Tentukan sendiri:
#     curl -fsSL .../bootstrap.sh | sudo GOWA_BASIC_AUTH='admin:RahasiaKuat123' sh -s -- gowa.domainku.com
#
#   Biarkan dibuat otomatis (password kuat, ditampilkan sekali di akhir):
#     cukup jangan set GOWA_BASIC_AUTH
#
#   Sengaja tanpa login (TIDAK disarankan kalau bisa diakses dari internet —
#   dashboard ini dapat mengirim WhatsApp dari device Anda):
#     ... | sudo GOWA_BASIC_AUTH=none sh -s -- gowa.domainku.com
#
# CARA JALAN — pilih lewat GOWA_MODE:
#
#   GOWA_MODE=systemd  (DEFAULT)
#     Binary native + service systemd. TIDAK membuat container Docker.
#     Cek dengan: systemctl status gowa-dashboard
#
#   GOWA_MODE=docker
#     Membuat container Docker bernama `gowa-dashboard`.
#     Cek dengan: docker ps | grep gowa-dashboard
#
#     ... | sudo GOWA_MODE=docker sh -s -- gowa.domainku.com
#
# AMAN DIULANG: menjalankan ulang = upgrade. File .env dan database
# (data/dashboard.db) tidak pernah ditimpa — termasuk login yang sudah ada.

set -e

REPO="${GOWA_REPO:-hafiz-ridha/gowa-dashboard}"
REF="${GOWA_REF:-main}"
DOMAIN="${1:-}"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }
step()  { printf "\n\033[1m== %s\033[0m\n" "$*"; }

fail() { red "GAGAL: $*"; exit 1; }

# ---------- pilih alat unduh ----------
if command -v curl >/dev/null 2>&1; then
    DL="curl -fsSL -o"
    DLQ="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
    DL="wget -qO"
    DLQ="wget -qO-"
else
    fail "butuh curl atau wget, dua-duanya tidak ada.
Pasang salah satu:  yum install -y curl   |   apt-get install -y curl"
fi

step "GoWA Dashboard — install dari GitHub"
info "Repo   : ${REPO}"
info "Ref    : ${REF}"
[ -n "$DOMAIN" ] && info "Domain : ${DOMAIN}" || info "Domain : (tidak diberikan — nginx dilewati)"

[ "$(id -u)" -eq 0 ] || fail "harus root. Tambahkan sudo:
  curl -fsSL .../bootstrap.sh | sudo sh -s -- DOMAIN-ANDA"

command -v tar >/dev/null 2>&1 || fail "perintah 'tar' tidak ada."

# ---------- area kerja sementara ----------
WORK="$(mktemp -d 2>/dev/null || mktemp -d -t gowa)"
[ -d "$WORK" ] || fail "tidak bisa membuat folder sementara."
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

# ---------- unduh source ----------
step "1/4 Mengunduh paket dari GitHub"

ARCHIVE="${WORK}/src.tar.gz"
URL="https://codeload.github.com/${REPO}/tar.gz/${REF}"
info "$URL"
$DL "$ARCHIVE" "$URL" 2>/dev/null \
    || fail "gagal mengunduh.
Periksa:
  - koneksi internet server ini
  - nama branch/tag '${REF}' benar (coba: GOWA_REF=main)
  - repo '${REPO}' publik / bisa diakses"

[ -s "$ARCHIVE" ] || fail "file unduhan kosong."

tar -xzf "$ARCHIVE" -C "$WORK" || fail "arsip rusak / gagal diekstrak."

# Folder hasil ekstrak bernama <repo>-<ref-dengan-slash-jadi-dash>.
SRC="$(find "$WORK" -maxdepth 2 -type d -name standalone 2>/dev/null | head -1)"
if [ -z "$SRC" ]; then
    red "GAGAL: branch/tag '${REF}' tidak berisi folder 'standalone/'."
    echo "" >&2
    echo "Paket standalone belum ada di ref itu. Pilih ref yang benar dengan" >&2
    echo "GOWA_REF, contoh:" >&2
    echo "" >&2
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/BRANCH/standalone/bootstrap.sh \\" >&2
    echo "    | sudo GOWA_REF=BRANCH sh -s -- ${DOMAIN:-DOMAIN-ANDA}" >&2
    echo "" >&2
    echo "Daftar branch yang tersedia:" >&2
    $DLQ "https://api.github.com/repos/${REPO}/branches" 2>/dev/null \
        | grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' \
        | sed 's/.*"\([^"]*\)"$/  - \1/' >&2 || echo "  (gagal mengambil daftar branch)" >&2
    exit 1
fi

info "Paket: $SRC"
green "OK"

# ---------- siapkan binary ----------
step "2/4 Menyiapkan binary"

ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)   BIN_NAME="whatsapp-dashboard-linux-amd64" ;;
    aarch64|arm64)  BIN_NAME="whatsapp-dashboard-linux-arm64" ;;
    *)              fail "arsitektur '$ARCH' tidak didukung (tersedia: x86_64, aarch64)." ;;
esac
info "Arsitektur: ${ARCH} -> ${BIN_NAME}"

# Binary ikut di dalam repo. Kalau tidak ada (mis. dipindah ke GitHub
# Releases), ambil dari release terbaru sebagai cadangan.
if [ ! -f "${SRC}/bin/${BIN_NAME}" ]; then
    info "Binary tidak ada di arsip — mencoba GitHub Releases..."
    mkdir -p "${SRC}/bin"
    REL_URL="https://github.com/${REPO}/releases/latest/download/${BIN_NAME}"
    if $DL "${SRC}/bin/${BIN_NAME}" "$REL_URL" 2>/dev/null && [ -s "${SRC}/bin/${BIN_NAME}" ]; then
        info "Berhasil dari Releases."
        # Checksum dari release (kalau disediakan) untuk diverifikasi di bawah.
        $DL "${SRC}/SHA256SUMS" "https://github.com/${REPO}/releases/latest/download/SHA256SUMS" 2>/dev/null || true
    else
        rm -f "${SRC}/bin/${BIN_NAME}"
        fail "binary '${BIN_NAME}' tidak ditemukan, baik di arsip maupun di Releases.
Alternatif: bangun sendiri di mesin dev lalu upload paketnya —
  sh scripts/build-standalone.sh"
    fi
fi

green "OK"

# ---------- verifikasi checksum ----------
step "3/4 Verifikasi integritas (SHA256)"

# Penting: skrip ini dijalankan lewat pipe dari internet. Memverifikasi
# checksum binary memastikan yang dieksekusi memang file yang dimaksud,
# bukan hasil unduhan yang rusak atau termodifikasi di tengah jalan.
if [ -f "${SRC}/SHA256SUMS" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
        ( cd "$SRC" && grep -F "$BIN_NAME" SHA256SUMS | sha256sum -c - ) \
            || fail "checksum TIDAK cocok untuk ${BIN_NAME}.
Unduhan rusak atau file berubah. Ulangi, atau laporkan kalau terus terjadi."
        green "OK: checksum cocok."
    elif command -v shasum >/dev/null 2>&1; then
        ( cd "$SRC" && grep -F "$BIN_NAME" SHA256SUMS | shasum -a 256 -c - ) \
            || fail "checksum TIDAK cocok untuk ${BIN_NAME}."
        green "OK: checksum cocok."
    else
        info "sha256sum/shasum tidak ada — verifikasi dilewati."
    fi
else
    info "File SHA256SUMS tidak ada — verifikasi dilewati."
fi

# ---------- jalankan installer ----------
step "4/4 Menjalankan installer"

chmod +x "${SRC}"/*.sh 2>/dev/null || true

# Buang CR kalau ada (pengaman kalau arsip pernah lewat tooling Windows):
# satu CR di install.sh membuat sh menolak dengan "bad interpreter".
for f in "${SRC}"/*.sh; do
    if head -1 "$f" 2>/dev/null | grep -q "$(printf '\r')"; then
        info "Membersihkan CRLF: $(basename "$f")"
        tr -d '\r' < "$f" > "${f}.lf" && mv "${f}.lf" "$f" && chmod +x "$f"
    fi
done

cd "$SRC"

# Teruskan pilihan login ke install.sh. `sudo VAR=x sh` sudah menaruh VAR di
# environment, tapi di-export eksplisit supaya perilakunya sama kalau skrip
# ini dipanggil dengan cara lain (mis. `sh bootstrap.sh` setelah `export`).
if [ -n "${GOWA_BASIC_AUTH:-}" ]; then
    export GOWA_BASIC_AUTH
fi

MODE="${GOWA_MODE:-systemd}"

case "$MODE" in
    systemd)
        info "Mode: systemd (binary native — TIDAK membuat container Docker)"
        if [ -n "$DOMAIN" ]; then
            sh ./install.sh "$DOMAIN"
        else
            sh ./install.sh
        fi
        ;;

    docker)
        info "Mode: docker (membuat container 'gowa-dashboard')"

        command -v docker >/dev/null 2>&1 || fail "docker tidak terpasang di server ini.
Pasang Docker dulu (aaPanel -> App Store -> Docker), atau pakai mode default:
  hilangkan GOWA_MODE=docker  (memakai systemd, tanpa Docker)"

        # `docker compose` (v2, plugin) vs `docker-compose` (v1, terpisah).
        if docker compose version >/dev/null 2>&1; then
            DC="docker compose"
        elif command -v docker-compose >/dev/null 2>&1; then
            DC="docker-compose"
        else
            fail "docker ada, tapi Compose tidak.
Pasang plugin compose:  yum install -y docker-compose-plugin
atau:                   apt-get install -y docker-compose-plugin"
        fi
        info "Compose: ${DC}"

        # PENTING: pindahkan paket ke lokasi TETAP sebelum menjalankan compose.
        # $WORK adalah folder sementara yang dihapus trap EXIT, sedangkan
        # docker-compose.yml memakai bind mount `./data:/data` — kalau compose
        # dijalankan dari folder sementara, database ikut terhapus begitu skrip
        # selesai, dan perintah logs/restart/down juga kehilangan project dir.
        DOCKER_DIR="/opt/gowa-dashboard-docker"
        info "Memasang paket ke ${DOCKER_DIR}"
        mkdir -p "$DOCKER_DIR"
        # Salin semuanya KECUALI data/ dan .env supaya install ulang tidak
        # menimpa database maupun konfigurasi yang sudah ada.
        for item in bin Dockerfile docker-compose.yml docker-entrypoint.sh \
                    setup-nginx.sh uninstall.sh lib-common.sh \
                    .env.example README.md SHA256SUMS; do
            [ -e "$item" ] && cp -r "$item" "$DOCKER_DIR/" 2>/dev/null || true
        done
        cd "$DOCKER_DIR"
        mkdir -p data
        SRC="$DOCKER_DIR"   # supaya pesan di akhir menunjuk lokasi yang benar

        # Fungsi bersama dengan install.sh — termasuk prompt interaktif dan
        # validasi format GOWA_BASIC_AUTH. Sebelumnya logika ini diduplikasi
        # di sini dan langsung menyimpang: mode docker kehilangan prompt DAN
        # kehilangan validasi, sehingga nilai tanpa ':' diterima diam-diam
        # padahal membuat dashboard TERBUKA (main.go hanya memasang middleware
        # kalau `len(parts) == 2`).
        [ -f lib-common.sh ] || fail "lib-common.sh tidak ada — paket tidak lengkap."
        # shellcheck source=lib-common.sh
        . ./lib-common.sh

        # Siapkan .env untuk container (compose membacanya lewat env_file).
        #
        # Kalau .env sudah ada TAPI login-nya kosong/rusak, jangan dilewati
        # diam-diam: dulu skrip ini hanya bilang "dipakai apa adanya" sehingga
        # user tidak pernah ditanya password DAN tidak tahu login apa yang
        # berlaku — persis penyebab "tidak bisa login" pada install ulang.
        if [ -f .env ]; then
            _cur="$(grep '^DASHBOARD_BASIC_AUTH=' .env 2>/dev/null | head -1 | cut -d= -f2- || true)"
            case "$_cur" in
                *:*)
                    info ".env sudah ada — login dipertahankan (user: ${_cur%%:*})."
                    info "Ganti login: sudo sh set-password.sh"
                    AUTH_GENERATED=0; AUTH_DISABLED=0; AUTH_USER="${_cur%%:*}"
                    ;;
                "")
                    yellow ".env sudah ada tapi login BELUM diatur (dashboard terbuka)."
                    info "Mengatur login sekarang..."
                    resolve_basic_auth
                    apply_basic_auth .env
                    ;;
                *)
                    red ".env sudah ada tapi nilai login '${_cur}' TIDAK VALID (tanpa ':')."
                    red "Nilai seperti itu diabaikan dashboard, jadi jalan TANPA proteksi."
                    info "Memperbaiki sekarang..."
                    resolve_basic_auth
                    apply_basic_auth .env
                    ;;
            esac
        else
            cp .env.example .env
            # Di dalam container, bind ke semua interface: isolasi dilakukan
            # oleh port mapping compose (127.0.0.1:18088), bukan oleh app.
            set_env DASHBOARD_HOST "0.0.0.0"             .env
            set_env DASHBOARD_PORT "8088"                .env
            set_env DASHBOARD_DB   "/data/dashboard.db"  .env

            resolve_basic_auth
            apply_basic_auth .env
            chmod 0600 .env
        fi

        info "Build image + start container..."
        $DC up -d --build || fail "docker compose gagal.
Lihat detailnya:  cd ${SRC} && ${DC} logs --tail=50"

        # Buktikan container benar-benar JALAN, bukan cuma 'created' lalu mati.
        sleep 3
        if ! docker ps --filter 'name=gowa-dashboard' --filter 'status=running' \
             --format '{{.Names}}' 2>/dev/null | grep -q gowa-dashboard; then
            red "Container tidak dalam status running. Log terakhir:"
            $DC logs --tail=30 2>&1 | tail -30 >&2 || true
            fail "container gagal jalan (lihat log di atas)."
        fi
        green "OK: container 'gowa-dashboard' running."
        docker ps --filter 'name=gowa-dashboard' \
            --format '  {{.Names}}  {{.Status}}  {{.Ports}}' 2>/dev/null || true

        # Reverse proxy: container di-bind ke 127.0.0.1:18088 oleh compose.
        if [ -n "$DOMAIN" ]; then
            sh ./setup-nginx.sh "$DOMAIN" 18088 || fail "konfigurasi nginx gagal."
        else
            info "Domain tidak diberikan — nginx dilewati."
            info "Set nanti:  sudo sh setup-nginx.sh DOMAIN-ANDA 18088"
        fi

        echo ""
        green "=============================================="
        green " GoWA Dashboard (Docker) berhasil dijalankan"
        green "=============================================="
        echo "  Container : gowa-dashboard"
        echo "  Data      : ${SRC}/data  (bind mount ke /data)"
        echo "  Log       : cd ${SRC} && ${DC} logs -f"
        echo "  Restart   : cd ${SRC} && ${DC} restart"
        echo "  Stop      : cd ${SRC} && ${DC} down"
        echo "  Akses     : http://127.0.0.1:18088"
        [ -n "$DOMAIN" ] && echo "  Publik    : https://${DOMAIN}"
        echo ""
        print_auth_summary "${DOCKER_DIR}/.env" "cd ${DOCKER_DIR} && ${DC} restart"

        yellow "LANGKAH TERAKHIR — hubungkan ke gowa-core:"
        echo "  Buka dashboard -> tab \"Pengaturan\" -> isi Core URL -> Simpan."
        echo ""
        ;;

    *)
        fail "GOWA_MODE tidak dikenal: '${MODE}'
Pilihan: systemd (default) atau docker"
        ;;
esac
