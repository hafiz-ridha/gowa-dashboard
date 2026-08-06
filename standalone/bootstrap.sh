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
# AMAN DIULANG: menjalankan ulang = upgrade. File .env dan database
# (data/dashboard.db) tidak pernah ditimpa.

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
if [ -n "$DOMAIN" ]; then
    sh ./install.sh "$DOMAIN"
else
    sh ./install.sh
fi
