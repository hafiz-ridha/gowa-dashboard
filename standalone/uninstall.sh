#!/bin/sh
# Uninstall GoWA Dashboard standalone.
#
#   sudo sh uninstall.sh          # hapus service + binary, DATABASE DIPERTAHANKAN
#   sudo sh uninstall.sh --purge  # hapus semuanya termasuk database & .env
#
# Config nginx TIDAK disentuh (bisa dipakai lagi kalau install ulang).
# Untuk mengembalikan config nginx, pakai file .bak.* yang dibuat setup-nginx.sh.

set -e

APP_NAME="gowa-dashboard"
APP_DIR="/opt/${APP_NAME}"
APP_USER="gowadash"
SERVICE="/etc/systemd/system/${APP_NAME}.service"
PURGE=0

[ "${1:-}" = "--purge" ] && PURGE=1

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }

[ "$(id -u)" -eq 0 ] || { red "Harus root (pakai sudo)."; exit 1; }

if systemctl list-unit-files 2>/dev/null | grep -q "^${APP_NAME}.service"; then
    info "Menghentikan & menonaktifkan service..."
    systemctl stop "${APP_NAME}" 2>/dev/null || true
    systemctl disable "${APP_NAME}" 2>/dev/null || true
    rm -f "$SERVICE"
    systemctl daemon-reload
    green "Service dihapus."
else
    info "Service ${APP_NAME} tidak terpasang."
fi

if [ "$PURGE" -eq 1 ]; then
    info "--purge: menghapus SELURUH ${APP_DIR} (termasuk database & .env)..."
    rm -rf "$APP_DIR"
    if id "$APP_USER" >/dev/null 2>&1; then
        userdel "$APP_USER" 2>/dev/null || true
        info "User sistem ${APP_USER} dihapus."
    fi
    green "Terhapus total."
else
    rm -f "${APP_DIR}/whatsapp-dashboard"
    green "Binary dihapus. Data DIPERTAHANKAN di:"
    echo "   ${APP_DIR}/data/dashboard.db   (jadwal, broadcast, log, setting core)"
    echo "   ${APP_DIR}/.env"
    echo ""
    echo "Hapus juga data tersebut dengan: sudo sh uninstall.sh --purge"
fi

echo ""
echo "Catatan: config nginx tidak diubah. Kalau mau dikembalikan, pakai backup:"
echo "  ls /www/server/panel/vhost/nginx/*.bak.*"
