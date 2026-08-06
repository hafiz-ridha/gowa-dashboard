#!/bin/sh
# =====================================================================
# Lihat / ganti login dashboard (Basic Auth) — systemd maupun Docker
# =====================================================================
#
# PAKAI:
#   sudo sh set-password.sh                 # tanya interaktif
#   sudo sh set-password.sh --show          # tampilkan login saat ini
#   sudo GOWA_BASIC_AUTH='admin:Baru123' sudo sh set-password.sh
#   sudo sh set-password.sh --disable       # matikan login (TIDAK disarankan)
#
# Otomatis mendeteksi Anda memakai instalasi systemd atau Docker, mengubah
# .env yang tepat, lalu me-restart layanan yang tepat.

set -e

SYSTEMD_ENV="/opt/gowa-dashboard/.env"
DOCKER_DIR="/opt/gowa-dashboard-docker"
DOCKER_ENV="${DOCKER_DIR}/.env"
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }
fail()  { red "GAGAL: $*"; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "harus root (pakai sudo)."

# ---------- deteksi instalasi ----------
MODE=""
ENVFILE=""
RESTART=""

if [ -f "$SYSTEMD_ENV" ]; then
    MODE="systemd"; ENVFILE="$SYSTEMD_ENV"
    RESTART="systemctl restart gowa-dashboard"
fi
if [ -f "$DOCKER_ENV" ]; then
    if [ -n "$MODE" ]; then
        yellow "Terdeteksi DUA instalasi (systemd dan Docker)."
        echo "  1) systemd -> $SYSTEMD_ENV"
        echo "  2) docker  -> $DOCKER_ENV"
        printf "Pilih [1/2]: "
        read -r pick < /dev/tty 2>/dev/null || pick=1
        [ "$pick" = "2" ] && { MODE="docker"; ENVFILE="$DOCKER_ENV"; }
    else
        MODE="docker"; ENVFILE="$DOCKER_ENV"
    fi
fi
if [ "$MODE" = "docker" ]; then
    if docker compose version >/dev/null 2>&1; then
        RESTART="cd ${DOCKER_DIR} && docker compose up -d"
    else
        RESTART="cd ${DOCKER_DIR} && docker-compose up -d"
    fi
fi

[ -n "$MODE" ] || fail "instalasi GoWA Dashboard tidak ditemukan.
Dicari di:
  ${SYSTEMD_ENV}
  ${DOCKER_ENV}
Belum terpasang? Jalankan install.sh / bootstrap.sh dulu."

info "Instalasi terdeteksi: ${MODE}"
info "File config        : ${ENVFILE}"

CURRENT="$(grep '^DASHBOARD_BASIC_AUTH=' "$ENVFILE" 2>/dev/null | head -1 | cut -d= -f2- || true)"

# ---------- --show ----------
if [ "${1:-}" = "--show" ]; then
    echo ""
    if [ -z "$CURRENT" ]; then
        red "Login TIDAK AKTIF — dashboard terbuka untuk siapa saja."
        echo "Pasang sekarang:  sudo sh $0"
    else
        case "$CURRENT" in
            *:*)
                green "Login aktif."
                echo "  Username : ${CURRENT%%:*}"
                echo "  Password : ${CURRENT#*:}"
                ;;
            *)
                red "NILAI TIDAK VALID: '${CURRENT}' (tidak ada tanda ':')."
                red "Dashboard JALAN TANPA LOGIN karena format ini diabaikan."
                echo "Perbaiki:  sudo sh $0"
                ;;
        esac
    fi
    exit 0
fi

# ---------- --disable ----------
if [ "${1:-}" = "--disable" ]; then
    GOWA_BASIC_AUTH=none
    export GOWA_BASIC_AUTH
fi

# ---------- peringatan kalau nilai lama tidak valid ----------
if [ -n "$CURRENT" ]; then
    case "$CURRENT" in
        *:*) info "Login saat ini: user '${CURRENT%%:*}'" ;;
        *)   red "PERHATIAN: nilai lama '${CURRENT}' tidak memuat ':'."
             red "Itu sebabnya login tidak berfungsi — dashboard mengabaikannya"
             red "dan jalan TANPA proteksi. Akan diperbaiki sekarang." ;;
    esac
else
    yellow "Login saat ini: TIDAK AKTIF (dashboard terbuka)."
fi

# ---------- tentukan kredensial baru ----------
[ -f "${SRC_DIR}/lib-common.sh" ] || fail "lib-common.sh tidak ada di ${SRC_DIR}.
Jalankan skrip ini dari dalam folder paket standalone."
# shellcheck source=lib-common.sh
. "${SRC_DIR}/lib-common.sh"

resolve_basic_auth
apply_basic_auth "$ENVFILE"

# ---------- terapkan ----------
echo ""
info "Menerapkan perubahan..."
if [ "$MODE" = "systemd" ]; then
    systemctl restart gowa-dashboard || fail "restart service gagal."
else
    ( cd "$DOCKER_DIR" && { docker compose up -d 2>/dev/null || docker-compose up -d; } ) \
        || fail "restart container gagal."
fi

# Beri waktu naik lalu buktikan auth benar-benar aktif.
sleep 3
PORT=18088
if command -v curl >/dev/null 2>&1; then
    CODE="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/api/_health" 2>/dev/null || echo 000)"
    case "$CODE" in
        401) green "OK: dashboard membalas 401 — login AKTIF." ;;
        200) if [ "${AUTH_DISABLED:-0}" -eq 1 ]; then
                 yellow "Dashboard membalas 200 — login memang dimatikan sesuai permintaan."
             else
                 red "Dashboard membalas 200 padahal login seharusnya aktif."
                 red "Cek isi ${ENVFILE} baris DASHBOARD_BASIC_AUTH."
             fi ;;
        000) yellow "Tidak bisa menghubungi 127.0.0.1:${PORT} — cek status layanan." ;;
        *)   yellow "Dashboard membalas HTTP ${CODE}." ;;
    esac
fi

echo ""
print_auth_summary "$ENVFILE" "$RESTART"
green "Selesai."
