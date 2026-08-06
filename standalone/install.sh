#!/bin/sh
# =====================================================================
# Installer standalone GoWA Dashboard untuk aaPanel (dan VPS Linux lain)
# =====================================================================
#
# Paket ini BERDIRI SENDIRI — tidak butuh gowa-core di server yang sama,
# tidak butuh Go, tidak butuh Docker, tidak butuh Node. Isinya binary
# statis (pure-Go, tanpa CGO) yang jalan di distro Linux apa pun.
#
# PAKAI:
#   sudo sh install.sh                      # install saja (port 127.0.0.1:18088)
#   sudo sh install.sh gowa.domainku.com    # install + sekaligus set nginx aaPanel
#
# AMAN DIULANG (idempoten): menjalankan ulang = upgrade binary saja.
# File .env dan database (data/dashboard.db) TIDAK PERNAH ditimpa.
#
# Setelah install, buka dashboard -> tab "Pengaturan" untuk mengisi
# URL + credential gowa-core. Tidak perlu edit file / restart apa pun.

set -e

APP_NAME="gowa-dashboard"
APP_DIR="/opt/${APP_NAME}"
APP_USER="gowadash"
SERVICE="/etc/systemd/system/${APP_NAME}.service"
PORT="18088"
DOMAIN="${1:-}"
NGINX_VHOST_DIR="/www/server/panel/vhost/nginx"

SRC_DIR="$(cd "$(dirname "$0")" && pwd)"

# ---------- output helpers ----------
red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }
step()  { printf "\n\033[1m== %s\033[0m\n" "$*"; }

fail() { red "GAGAL: $*"; exit 1; }

# ---------- pre-flight ----------
step "1/7 Pemeriksaan awal"

[ "$(id -u)" -eq 0 ] || fail "harus dijalankan sebagai root. Pakai: sudo sh install.sh"

command -v systemctl >/dev/null 2>&1 || fail "systemd (systemctl) tidak ditemukan.
Server ini tidak pakai systemd, jadi service tidak bisa dipasang otomatis.
Alternatif: pakai jalur Docker (lihat README.md bagian 'Cara B')."

# Pilih binary sesuai arsitektur CPU.
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)   BIN_SRC="${SRC_DIR}/bin/whatsapp-dashboard-linux-amd64" ;;
    aarch64|arm64)  BIN_SRC="${SRC_DIR}/bin/whatsapp-dashboard-linux-arm64" ;;
    *)              fail "arsitektur '$ARCH' tidak didukung paket ini (tersedia: x86_64, aarch64)." ;;
esac
[ -f "$BIN_SRC" ] || fail "binary tidak ada: $BIN_SRC
Paket rusak atau belum lengkap. Pastikan folder bin/ ikut ter-upload."

info "OS arch      : $ARCH"
info "Binary dipakai: $(basename "$BIN_SRC")"

# Cek port belum dipakai proses lain (selain service kita sendiri).
if command -v ss >/dev/null 2>&1; then
    if ss -ltn 2>/dev/null | grep -q ":${PORT}[[:space:]]"; then
        if systemctl is-active --quiet "${APP_NAME}" 2>/dev/null; then
            info "Port ${PORT} dipakai service ${APP_NAME} yang sudah ada (akan di-upgrade)."
        else
            fail "port ${PORT} sudah dipakai proses lain.
Cek dengan: ss -ltnp | grep ${PORT}
Lalu hentikan proses itu, atau ubah nilai PORT di bagian atas install.sh."
        fi
    fi
fi

green "OK"

# ---------- user & direktori ----------
step "2/7 Menyiapkan user & folder"

if ! id "$APP_USER" >/dev/null 2>&1; then
    # -r = system account, tanpa login shell, tanpa home terpisah.
    useradd -r -s /usr/sbin/nologin -d "$APP_DIR" "$APP_USER" 2>/dev/null \
        || useradd -r -s /sbin/nologin -d "$APP_DIR" "$APP_USER" 2>/dev/null \
        || fail "tidak bisa membuat user sistem '$APP_USER'."
    info "User sistem '$APP_USER' dibuat."
else
    info "User sistem '$APP_USER' sudah ada."
fi

mkdir -p "${APP_DIR}/data"
green "OK: ${APP_DIR}"

# ---------- binary ----------
step "3/7 Memasang binary"

# Service dihentikan dulu supaya file binary tidak 'busy' saat ditimpa.
if systemctl is-active --quiet "${APP_NAME}" 2>/dev/null; then
    info "Menghentikan service lama untuk upgrade..."
    systemctl stop "${APP_NAME}"
fi

install -m 0755 "$BIN_SRC" "${APP_DIR}/whatsapp-dashboard"
green "OK: ${APP_DIR}/whatsapp-dashboard"

# ---------- konfigurasi ----------
step "4/7 Konfigurasi (.env)"

if [ -f "${APP_DIR}/.env" ]; then
    yellow "SKIP: ${APP_DIR}/.env sudah ada — tidak ditimpa (konfigurasi Anda aman)."
else
    cp "${SRC_DIR}/.env.example" "${APP_DIR}/.env"
    # Paksa bind ke loopback: dashboard diakses lewat nginx, tidak langsung.
    sed -i "s|^DASHBOARD_HOST=.*|DASHBOARD_HOST=127.0.0.1|" "${APP_DIR}/.env"
    sed -i "s|^DASHBOARD_PORT=.*|DASHBOARD_PORT=${PORT}|"    "${APP_DIR}/.env"
    sed -i "s|^DASHBOARD_DB=.*|DASHBOARD_DB=${APP_DIR}/data/dashboard.db|" "${APP_DIR}/.env"
    green "OK: ${APP_DIR}/.env dibuat dari template."
fi

# Kepemilikan: user service harus bisa tulis DB + WAL di data/.
chown -R "${APP_USER}:${APP_USER}" "$APP_DIR"
chmod 0640 "${APP_DIR}/.env"

# ---------- systemd ----------
step "5/7 Memasang service systemd"

sed -e "s|@APP_DIR@|${APP_DIR}|g" \
    -e "s|@APP_USER@|${APP_USER}|g" \
    "${SRC_DIR}/gowa-dashboard.service" > "$SERVICE"

systemctl daemon-reload
systemctl enable "${APP_NAME}" >/dev/null 2>&1
systemctl restart "${APP_NAME}"

# Beri waktu proses naik sebelum diperiksa.
i=0
while [ "$i" -lt 15 ]; do
    if systemctl is-active --quiet "${APP_NAME}"; then break; fi
    i=$((i + 1))
    sleep 1
done

if ! systemctl is-active --quiet "${APP_NAME}"; then
    red "Service gagal start. 20 baris log terakhir:"
    journalctl -u "${APP_NAME}" -n 20 --no-pager || true
    fail "service ${APP_NAME} tidak jalan."
fi
green "OK: service ${APP_NAME} aktif dan enabled (auto-start saat reboot)."

# ---------- verifikasi lokal ----------
step "6/7 Verifikasi dashboard merespons"

OK_LOCAL=0
i=0
while [ "$i" -lt 10 ]; do
    if command -v curl >/dev/null 2>&1; then
        CODE="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/api/_health" 2>/dev/null || echo 000)"
        # 200 = terbuka, 401 = hidup tapi diproteksi basic auth. Dua-duanya sehat.
        case "$CODE" in
            200|401) OK_LOCAL=1; break ;;
        esac
    else
        # curl tidak ada — cukup andalkan status systemd yang sudah aktif.
        OK_LOCAL=2; break
    fi
    i=$((i + 1))
    sleep 1
done

case "$OK_LOCAL" in
    1) green "OK: http://127.0.0.1:${PORT}/api/_health menjawab (HTTP ${CODE})." ;;
    2) yellow "curl tidak terpasang — verifikasi HTTP dilewati (service tetap aktif)." ;;
    *) red "Dashboard tidak menjawab di port ${PORT}."
       journalctl -u "${APP_NAME}" -n 20 --no-pager || true
       fail "verifikasi lokal gagal." ;;
esac

# ---------- nginx (opsional) ----------
step "7/7 Reverse proxy nginx"

if [ -z "$DOMAIN" ]; then
    yellow "Domain tidak diberikan — konfigurasi nginx dilewati."
    echo "   Untuk set otomatis nanti:  sudo sh install.sh DOMAIN-ANDA"
    echo "   Atau manual: pakai isi file nginx-aapanel.conf.example"
elif [ ! -d "$NGINX_VHOST_DIR" ]; then
    yellow "Folder vhost aaPanel ($NGINX_VHOST_DIR) tidak ada — sepertinya bukan aaPanel."
    echo "   Set reverse proxy manual pakai nginx-aapanel.conf.example"
elif [ ! -f "${NGINX_VHOST_DIR}/${DOMAIN}.conf" ]; then
    yellow "Site '${DOMAIN}' belum ada di aaPanel."
    echo "   Buat dulu: aaPanel -> Website -> Add site -> ${DOMAIN} (PHP: Pure static)"
    echo "   Lalu jalankan ulang: sudo sh install.sh ${DOMAIN}"
else
    sh "${SRC_DIR}/setup-nginx.sh" "$DOMAIN" "$PORT" || fail "konfigurasi nginx gagal."
fi

# ---------- ringkasan ----------
echo ""
green "=============================================="
green " GoWA Dashboard standalone berhasil dipasang"
green "=============================================="
echo ""
echo "  Lokasi     : ${APP_DIR}"
echo "  Config     : ${APP_DIR}/.env"
echo "  Database   : ${APP_DIR}/data/dashboard.db"
echo "  Service    : systemctl status ${APP_NAME}"
echo "  Log        : journalctl -u ${APP_NAME} -f"
echo "  Akses lokal: http://127.0.0.1:${PORT}"
if [ -n "$DOMAIN" ]; then
    echo "  Akses publik: https://${DOMAIN}"
fi
echo ""
yellow "LANGKAH TERAKHIR — hubungkan ke gowa-core:"
echo "  Buka dashboard -> tab \"Pengaturan\" -> isi Core URL"
echo "  (+ username/password kalau core pakai basic auth) -> Simpan."
echo "  Tidak perlu edit file atau restart apa pun."
echo ""
echo "Perintah berguna:"
echo "  systemctl restart ${APP_NAME}    # restart"
echo "  systemctl stop ${APP_NAME}       # stop"
echo "  sudo sh uninstall.sh             # hapus (DB bisa dipertahankan)"
echo ""
