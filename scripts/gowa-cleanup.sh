#!/bin/sh
# =====================================================================
# gowa-cleanup — pembersih penyimpanan otomatis untuk gowa-core
# =====================================================================
#
# Mengatasi error:
#   failed to save cached sessions: ... database or disk is full
#
# Diverifikasi terhadap upstream v9.1.0 (path & perilaku sama sejak v8):
#   statics/media      <- auto-download media, TUMBUH TANPA BATAS
#   statics/senditems  <- file kiriman keluar
#   statics/qrcode     <- PNG QR tiap percobaan login
#   storages/*.db      <- whatsapp.db (sesi) & chatstorage.db (pesan)
#
# gowa TIDAK punya retensi bawaan (dicek di config/settings.go v9.1.0:
# tidak ada retention/cleanup/ttl), jadi pembersihan harus dari luar.
#
# PAKAI:
#   sh gowa-cleanup.sh --dry-run                 # lihat saja, tidak menghapus
#   sh gowa-cleanup.sh                           # bersihkan (default 30 hari)
#   sh gowa-cleanup.sh --media-days 14           # media > 14 hari
#   sh gowa-cleanup.sh --install-cron            # jadwalkan tiap minggu
#   sh gowa-cleanup.sh --uninstall-cron
#
# Lokasi gowa dideteksi otomatis; timpa dengan:  GOWA_DIR=/path/ke/gowa

set -e

# ---------- default ----------
MEDIA_DAYS="${GOWA_MEDIA_DAYS:-30}"   # media lama dihapus setelah N hari
TEMP_DAYS="${GOWA_TEMP_DAYS:-2}"      # qrcode & senditems (aman, umur pendek)
DRY_RUN=0
DO_VACUUM=1
MIN_FREE_PCT=10                        # peringatkan kalau sisa disk < 10%

LOG_TAG="gowa-cleanup"
CRON_FILE="/etc/cron.d/gowa-cleanup"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }
fail()  { red "GAGAL: $*"; exit 1; }

# ---------- argumen ----------
while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run)        DRY_RUN=1 ;;
        --no-vacuum)      DO_VACUUM=0 ;;
        --media-days)     shift; MEDIA_DAYS="${1:?butuh angka}" ;;
        --temp-days)      shift; TEMP_DAYS="${1:?butuh angka}" ;;
        --install-cron)   ACTION=install ;;
        --uninstall-cron) ACTION=uninstall ;;
        -h|--help)
            sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'
            exit 0 ;;
        *) fail "argumen tidak dikenal: $1  (pakai --help)" ;;
    esac
    shift
done

# Validasi angka — `find -mtime +abc` diam-diam tidak cocok apa pun,
# jadi salah ketik akan terlihat seperti "tidak ada yang dihapus".
case "$MEDIA_DAYS" in ''|*[!0-9]*) fail "--media-days harus angka: '$MEDIA_DAYS'" ;; esac
case "$TEMP_DAYS"  in ''|*[!0-9]*) fail "--temp-days harus angka: '$TEMP_DAYS'" ;; esac

# ---------- deteksi lokasi gowa ----------
find_gowa() {
    if [ -n "${GOWA_DIR:-}" ]; then
        printf '%s' "$GOWA_DIR"; return
    fi
    for d in /opt/gowa /opt/go-whatsapp-web-multidevice /root/gowa \
             /www/wwwroot/gowa "$(pwd)" "$(dirname "$0")/.."; do
        if [ -d "$d/storages" ] || [ -d "$d/statics" ]; then
            ( cd "$d" && pwd ); return
        fi
    done
    printf ''
}

GOWA_DIR="$(find_gowa)"
[ -n "$GOWA_DIR" ] || fail "folder gowa tidak ditemukan (yang berisi storages/ + statics/).
Tentukan manual:  GOWA_DIR=/path/ke/gowa sh $0"

# ---------- pasang / cabut cron ----------
if [ "${ACTION:-}" = "install" ]; then
    [ "$(id -u)" -eq 0 ] || fail "pasang cron harus root (pakai sudo)."
    SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
    TARGET="/usr/local/bin/gowa-cleanup.sh"
    [ "$SELF" = "$TARGET" ] || install -m 0755 "$SELF" "$TARGET"
    # Minggu 03:10 — sepi trafik. PATH diisi eksplisit karena cron punya
    # PATH minimal dan sqlite3/find bisa tidak terjangkau.
    cat > "$CRON_FILE" <<CRON
# gowa-cleanup — pembersih penyimpanan mingguan (dipasang otomatis)
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
MAILTO=""
10 3 * * 0 root GOWA_DIR="${GOWA_DIR}" ${TARGET} --media-days ${MEDIA_DAYS} --temp-days ${TEMP_DAYS} >> /var/log/gowa-cleanup.log 2>&1
CRON
    chmod 0644 "$CRON_FILE"
    # Beberapa distro perlu cron dimuat ulang untuk membaca /etc/cron.d baru.
    systemctl reload crond 2>/dev/null || systemctl reload cron 2>/dev/null || true
    green "OK: cron mingguan terpasang -> ${CRON_FILE}"
    echo "  Jadwal : setiap Minggu 03:10"
    echo "  Target : ${GOWA_DIR}"
    echo "  Retensi: media ${MEDIA_DAYS} hari, temp ${TEMP_DAYS} hari"
    echo "  Log    : /var/log/gowa-cleanup.log"
    echo ""
    echo "Uji sekarang tanpa menghapus:  sh ${TARGET} --dry-run"
    exit 0
fi

if [ "${ACTION:-}" = "uninstall" ]; then
    [ "$(id -u)" -eq 0 ] || fail "cabut cron harus root (pakai sudo)."
    rm -f "$CRON_FILE"
    systemctl reload crond 2>/dev/null || systemctl reload cron 2>/dev/null || true
    green "OK: cron mingguan dicabut. Binary di /usr/local/bin/gowa-cleanup.sh dibiarkan."
    exit 0
fi

# ---------- mulai ----------
printf "\n===== %s  %s =====\n" "$LOG_TAG" "$(date '+%Y-%m-%d %H:%M:%S')"
info "Folder gowa : ${GOWA_DIR}"
info "Retensi     : media ${MEDIA_DAYS} hari, temp ${TEMP_DAYS} hari"
[ "$DRY_RUN" -eq 1 ] && yellow "MODE DRY-RUN — tidak ada yang dihapus."

# ---------- kondisi disk SEBELUM ----------
DISK_USE_BEFORE="$(df -P "$GOWA_DIR" 2>/dev/null | awk 'NR==2{print $5}' | tr -d '%')"
INODE_USE="$(df -Pi "$GOWA_DIR" 2>/dev/null | awk 'NR==2{print $5}' | tr -d '%')"
printf "\n-- Kondisi awal --\n"
df -Ph "$GOWA_DIR" | awk 'NR==2{printf "  Disk  : %s dipakai dari %s (%s terpakai)\n", $3, $2, $5}'
df -Pi "$GOWA_DIR" | awk 'NR==2{printf "  Inode : %s dipakai dari %s (%s terpakai)\n", $3, $2, $5}'

# Inode habis adalah penyebab yang sering terlewat: auto-download media
# membuat ribuan file kecil, sehingga SQLite bisa balas "disk is full"
# padahal kapasitas byte masih lega.
if [ -n "$INODE_USE" ] && [ "$INODE_USE" -ge 90 ] 2>/dev/null; then
    yellow "  PERHATIAN: inode terpakai ${INODE_USE}% — ini bisa memicu"
    yellow "  'database or disk is full' walau disk masih lapang."
fi

printf "\n-- Ukuran sebelum --\n"
for d in storages statics/media statics/senditems statics/qrcode; do
    [ -d "${GOWA_DIR}/${d}" ] && du -sh "${GOWA_DIR}/${d}" 2>/dev/null | awk '{printf "  %-22s %s\n", $2, $1}'
done

# ---------- pembersihan ----------
# clean_dir DIR HARI LABEL
clean_dir() {
    _dir="$1"; _days="$2"; _label="$3"
    [ -d "$_dir" ] || { info "lewati ${_label} (folder tidak ada)"; return 0; }

    _n="$(find "$_dir" -type f -mtime "+${_days}" 2>/dev/null | wc -l | tr -d ' ')"
    if [ "$_n" -eq 0 ]; then
        info "${_label}: tidak ada file > ${_days} hari"
        return 0
    fi
    _sz="$(find "$_dir" -type f -mtime "+${_days}" -printf '%s\n' 2>/dev/null \
           | awk '{s+=$1} END{printf "%.1f MB", s/1048576}')"
    [ -n "$_sz" ] || _sz="?"

    if [ "$DRY_RUN" -eq 1 ]; then
        yellow "${_label}: AKAN dihapus ${_n} file (${_sz})"
    else
        find "$_dir" -type f -mtime "+${_days}" -delete 2>/dev/null || true
        # Buang folder tanggal/kosong yang tertinggal setelah file dihapus.
        find "$_dir" -mindepth 1 -type d -empty -delete 2>/dev/null || true
        green "${_label}: dihapus ${_n} file (${_sz})"
    fi
}

printf "\n-- Pembersihan file --\n"
clean_dir "${GOWA_DIR}/statics/media"     "$MEDIA_DAYS" "media"
clean_dir "${GOWA_DIR}/statics/senditems" "$TEMP_DAYS"  "senditems"
clean_dir "${GOWA_DIR}/statics/qrcode"    "$TEMP_DAYS"  "qrcode"

# ---------- rapikan SQLite ----------
# WAL yang membengkak ikut memakan tempat. Checkpoint TRUNCATE mengecilkannya
# tanpa mengganggu core yang sedang jalan; VACUUM merebut kembali halaman
# kosong bekas penghapusan.
if [ "$DO_VACUUM" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
    printf "\n-- Rapikan database --\n"
    if command -v sqlite3 >/dev/null 2>&1; then
        for db in whatsapp.db chatstorage.db; do
            f="${GOWA_DIR}/storages/${db}"
            [ -f "$f" ] || continue
            _before="$(du -m "$f" 2>/dev/null | awk '{print $1}')"
            if sqlite3 "$f" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 \
               && sqlite3 "$f" "VACUUM;" >/dev/null 2>&1; then
                _after="$(du -m "$f" 2>/dev/null | awk '{print $1}')"
                green "  ${db}: ${_before}MB -> ${_after}MB"
            else
                # Umumnya karena file terkunci proses core. Tidak fatal —
                # pembersihan file di atas sudah memberi ruang.
                yellow "  ${db}: dilewati (terkunci / sedang dipakai)"
            fi
        done
    else
        yellow "  sqlite3 tidak terpasang — VACUUM dilewati."
        echo "  Pasang:  apt-get install -y sqlite3   |   yum install -y sqlite"
    fi
fi

# ---------- hasil ----------
printf "\n-- Kondisi akhir --\n"
df -Ph "$GOWA_DIR" | awk 'NR==2{printf "  Disk  : %s dipakai dari %s (%s terpakai)\n", $3, $2, $5}'
df -Pi "$GOWA_DIR" | awk 'NR==2{printf "  Inode : %s dipakai dari %s (%s terpakai)\n", $3, $2, $5}'

DISK_USE_AFTER="$(df -P "$GOWA_DIR" 2>/dev/null | awk 'NR==2{print $5}' | tr -d '%')"
if [ -n "$DISK_USE_BEFORE" ] && [ -n "$DISK_USE_AFTER" ]; then
    _freed=$((DISK_USE_BEFORE - DISK_USE_AFTER))
    [ "$_freed" -gt 0 ] && green "  Turun ${_freed}% pemakaian disk."
fi

# Masih kritis? Beri saran konkret, jangan diam.
if [ -n "$DISK_USE_AFTER" ] && [ "$DISK_USE_AFTER" -ge $((100 - MIN_FREE_PCT)) ] 2>/dev/null; then
    printf "\n"
    red "PERINGATAN: disk masih ${DISK_USE_AFTER}% terpakai."
    echo "Langkah berikutnya:"
    echo "  1. Perpendek retensi:  sh $0 --media-days 7"
    echo "  2. Matikan sumbernya di src/.env (paling berdampak):"
    echo "       WHATSAPP_AUTO_DOWNLOAD_MEDIA=false"
    echo "     lalu restart core. Media tetap bisa diambil lewat API saat perlu."
    echo "  3. Kalau pakai Docker:  docker system prune -a --volumes"
    echo "  4. Volume besar: pindah ke PostgreSQL lewat DB_URI=postgres://..."
fi

printf "\n"
green "Selesai."
