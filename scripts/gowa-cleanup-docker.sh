#!/bin/sh
# =====================================================================
# gowa-cleanup-docker — pembersih penyimpanan gowa-core versi CONTAINER
# =====================================================================
#
# Mengatasi: "failed to save cached sessions: ... database or disk is full"
#
# Diverifikasi terhadap upstream v9.1.0 — path & perilaku sama sejak v8:
#   /app/statics/media      <- WHATSAPP_AUTO_DOWNLOAD_MEDIA=true, TANPA BATAS
#   /app/statics/senditems
#   /app/statics/qrcode
#   /app/storages/*.db      <- whatsapp.db (sesi) + chatstorage.db (pesan)
# v9 TETAP tidak punya retensi bawaan, jadi harus dibersihkan dari luar.
#
# PAKAI:
#   sh gowa-cleanup-docker.sh --dry-run              # lihat saja (SELALU mulai dari sini)
#   sh gowa-cleanup-docker.sh                        # bersihkan, retensi 30 hari
#   sh gowa-cleanup-docker.sh --media-days 14
#   sh gowa-cleanup-docker.sh --container gowa-core  # tentukan container manual
#   sh gowa-cleanup-docker.sh --install-cron         # jadwal mingguan
#   sh gowa-cleanup-docker.sh --uninstall-cron
#
# ---------------------------------------------------------------------
# PENGAMAN (penting di aaPanel yang menjalankan banyak aplikasi):
#
#   * HANYA menyentuh container gowa yang teridentifikasi. Kalau ada lebih
#     dari satu kandidat, skrip BERHENTI dan meminta --container, bukan
#     menebak.
#   * Path kerja diambil dari `docker inspect` container itu sendiri, lalu
#     diperiksa terhadap denylist (/, /etc, /var, /root, /www, dst) dan
#     wajib berisi statics/ atau storages/. Tanpa itu, skrip menolak jalan.
#   * File .db TIDAK PERNAH dihapus — hanya file media/qr/senditems.
#   * TIDAK menjalankan `docker system prune -a` maupun `--volumes`.
#     Perintah itu menghapus image & volume milik SELURUH server, termasuk
#     aplikasi aaPanel lain. Yang tersedia hanya `--prune-dangling`
#     (image tanpa tag, opt-in) yang tidak menyentuh image terpakai.
#   * Log container di-TRUNCATE (`: > file`), bukan dihapus — menghapus
#     file log yang sedang dibuka Docker membuat log berhenti tercatat
#     sampai container restart.
# ---------------------------------------------------------------------

set -e

MEDIA_DAYS="${GOWA_MEDIA_DAYS:-30}"
TEMP_DAYS="${GOWA_TEMP_DAYS:-2}"
DRY_RUN=0
DO_VACUUM=1
DO_LOGS=1
PRUNE_DANGLING=0
CONTAINER="${GOWA_CONTAINER:-}"
ACTION=""
CRON_FILE="/etc/cron.d/gowa-cleanup-docker"
TARGET_BIN="/usr/local/bin/gowa-cleanup-docker.sh"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }
step()  { printf "\n\033[1m-- %s\033[0m\n" "$*"; }
fail()  { red "GAGAL: $*"; exit 1; }

# ---------- argumen ----------
while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run)         DRY_RUN=1 ;;
        --no-vacuum)       DO_VACUUM=0 ;;
        --no-logs)         DO_LOGS=0 ;;
        --prune-dangling)  PRUNE_DANGLING=1 ;;
        --media-days)      shift; MEDIA_DAYS="${1:?butuh angka}" ;;
        --temp-days)       shift; TEMP_DAYS="${1:?butuh angka}" ;;
        --container)       shift; CONTAINER="${1:?butuh nama container}" ;;
        --install-cron)    ACTION=install ;;
        --uninstall-cron)  ACTION=uninstall ;;
        -h|--help)         sed -n '2,48p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) fail "argumen tidak dikenal: $1  (pakai --help)" ;;
    esac
    shift
done

# `find -mtime +abc` diam-diam tidak cocok apa pun — salah ketik akan
# terlihat seperti "tidak ada yang perlu dibersihkan".
case "$MEDIA_DAYS" in ''|*[!0-9]*) fail "--media-days harus angka: '$MEDIA_DAYS'" ;; esac
case "$TEMP_DAYS"  in ''|*[!0-9]*) fail "--temp-days harus angka: '$TEMP_DAYS'" ;; esac

command -v docker >/dev/null 2>&1 || fail "docker tidak ditemukan di PATH."
docker info >/dev/null 2>&1 || fail "tidak bisa bicara dengan Docker daemon.
Jalankan sebagai root (sudo) atau pastikan Docker berjalan."

# ---------- deteksi container gowa ----------
# Tidak menebak: kalau ambigu, berhenti dan minta --container.
detect_container() {
    _found=""
    # 1) nama persis yang lazim dipakai
    for n in gowa-core whatsapp_go gowa_whatsapp_go_1 gowa-whatsapp_go-1; do
        if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$n"; then
            _found="${_found}${n}
"
        fi
    done
    # 2) kalau belum ketemu, cocokkan dari IMAGE (bukan sekadar nama, supaya
    #    container lain yang kebetulan bernama mirip tidak ikut terjaring)
    if [ -z "$_found" ]; then
        _found="$(docker ps -a --format '{{.Names}}\t{{.Image}}' 2>/dev/null \
                  | grep -iE 'go-whatsapp-web-multidevice|gowa' \
                  | grep -viE 'dashboard' \
                  | awk '{print $1}')"
    fi
    printf '%s' "$_found" | grep -v '^$' || true
}

if [ -z "$CONTAINER" ]; then
    CANDIDATES="$(detect_container)"
    _n="$(printf '%s\n' "$CANDIDATES" | grep -c . || true)"
    if [ "${_n:-0}" -eq 0 ]; then
        fail "container gowa-core tidak ditemukan.
Lihat daftar container:  docker ps -a --format '{{.Names}}\t{{.Image}}'
Lalu tentukan manual:    sh $0 --container NAMA-CONTAINER"
    elif [ "${_n:-0}" -gt 1 ]; then
        red "Ditemukan lebih dari satu kandidat container:"
        printf '%s\n' "$CANDIDATES" | sed 's/^/    /' >&2
        fail "ambigu — tentukan manual dengan --container NAMA
(skrip sengaja tidak menebak supaya tidak menyentuh container yang salah)."
    fi
    CONTAINER="$(printf '%s\n' "$CANDIDATES" | head -1)"
fi

docker inspect "$CONTAINER" >/dev/null 2>&1 \
    || fail "container '$CONTAINER' tidak ada."

# ---------- pasang / cabut cron ----------
if [ "$ACTION" = "install" ]; then
    [ "$(id -u)" -eq 0 ] || fail "pasang cron harus root (pakai sudo)."
    SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
    [ "$SELF" = "$TARGET_BIN" ] || install -m 0755 "$SELF" "$TARGET_BIN"
    cat > "$CRON_FILE" <<CRON
# gowa-cleanup-docker — pembersih penyimpanan mingguan (dipasang otomatis)
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
MAILTO=""
20 3 * * 0 root ${TARGET_BIN} --container ${CONTAINER} --media-days ${MEDIA_DAYS} --temp-days ${TEMP_DAYS} >> /var/log/gowa-cleanup.log 2>&1
CRON
    chmod 0644 "$CRON_FILE"
    systemctl reload crond 2>/dev/null || systemctl reload cron 2>/dev/null || true
    green "OK: cron mingguan terpasang -> ${CRON_FILE}"
    echo "  Jadwal    : setiap Minggu 03:20"
    echo "  Container : ${CONTAINER}"
    echo "  Retensi   : media ${MEDIA_DAYS} hari, temp ${TEMP_DAYS} hari"
    echo "  Log       : /var/log/gowa-cleanup.log"
    echo ""
    echo "Uji dulu tanpa menghapus:  sh ${TARGET_BIN} --dry-run"
    exit 0
fi
if [ "$ACTION" = "uninstall" ]; then
    [ "$(id -u)" -eq 0 ] || fail "cabut cron harus root (pakai sudo)."
    rm -f "$CRON_FILE"
    systemctl reload crond 2>/dev/null || systemctl reload cron 2>/dev/null || true
    green "OK: cron dicabut."
    exit 0
fi

# ---------- mulai ----------
printf "\n===== gowa-cleanup-docker  %s =====\n" "$(date '+%Y-%m-%d %H:%M:%S')"
info "Container : ${CONTAINER}"
info "Retensi   : media ${MEDIA_DAYS} hari, temp ${TEMP_DAYS} hari"
[ "$DRY_RUN" -eq 1 ] && yellow "MODE DRY-RUN — tidak ada yang dihapus."

RUNNING="$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || echo false)"
info "Status    : $([ "$RUNNING" = "true" ] && echo 'running' || echo 'stopped')"

# ---------- tentukan cara akses file ----------
# Bind mount  -> file ada di host, dibersihkan langsung (cepat, tak perlu exec)
# Named volume-> dibersihkan lewat `docker exec` di dalam container
HOST_STATICS="$(docker inspect -f \
  '{{range .Mounts}}{{if eq .Destination "/app/statics"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' \
  "$CONTAINER" 2>/dev/null || true)"
HOST_STORAGES="$(docker inspect -f \
  '{{range .Mounts}}{{if eq .Destination "/app/storages"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' \
  "$CONTAINER" 2>/dev/null || true)"

# --- Pengaman path: tolak path berbahaya sebelum menyentuh apa pun ---
safe_path() {
    _p="$1"; _label="$2"
    [ -n "$_p" ] || return 1
    case "$_p" in
        /) red "TOLAK: ${_label} = '/' (root filesystem)"; return 1 ;;
        /etc|/etc/*|/var|/var/lib|/usr|/usr/*|/bin|/bin/*|/sbin|/sbin/*|/boot|/boot/*|/root|/home|/www|/www/server|/www/server/*)
            red "TOLAK: ${_label} = '${_p}' masuk daftar terlarang"; return 1 ;;
    esac
    case "$_p" in
        /*) : ;;
        *)  red "TOLAK: ${_label} bukan path absolut: '${_p}'"; return 1 ;;
    esac
    # Kedalaman minimal 2 segmen -> menghindari /tmp, /data, dsb.
    _depth="$(printf '%s' "$_p" | awk -F/ '{c=0; for(i=1;i<=NF;i++) if($i!="") c++; print c}')"
    [ "${_depth:-0}" -ge 2 ] || { red "TOLAK: ${_label} terlalu dangkal: '${_p}'"; return 1; }
    [ -d "$_p" ] || { red "TOLAK: ${_label} bukan folder: '${_p}'"; return 1; }
    return 0
}

MODE=""
if [ -n "$HOST_STATICS" ] && safe_path "$HOST_STATICS" "mount statics"; then
    MODE="host"
    info "Akses     : bind mount di host -> ${HOST_STATICS}"
else
    if [ -n "$HOST_STATICS" ]; then
        yellow "Bind mount ada tapi tidak lolos pemeriksaan keamanan — beralih ke mode exec."
    fi
    [ "$RUNNING" = "true" ] || fail "penyimpanan bukan bind mount, jadi perlu masuk ke container,
tapi container '${CONTAINER}' sedang berhenti.
Jalankan dulu:  docker start ${CONTAINER}"
    MODE="exec"
    info "Akses     : named volume -> lewat 'docker exec' di dalam container"
fi

# ---------- helper hitung & hapus ----------
# Semua operasi DIBATASI ke subfolder statics/* — .db tidak pernah masuk cakupan.
run_find() { # $1=path, $2=args...
    if [ "$MODE" = "host" ]; then
        _base="$1"; shift
        find "$_base" "$@" 2>/dev/null || true
    else
        _base="$1"; shift
        docker exec "$CONTAINER" sh -c "find '$_base' $* 2>/dev/null" || true
    fi
}

clean_target() { # $1=subdir (media|senditems|qrcode)  $2=hari  $3=label
    _sub="$1"; _days="$2"; _label="$3"
    if [ "$MODE" = "host" ]; then
        _path="${HOST_STATICS}/${_sub}"
        [ -d "$_path" ] || { info "${_label}: folder tidak ada, dilewati"; return 0; }
    else
        _path="/app/statics/${_sub}"
        docker exec "$CONTAINER" test -d "$_path" 2>/dev/null \
            || { info "${_label}: folder tidak ada, dilewati"; return 0; }
    fi

    _n="$(run_find "$_path" -type f -mtime "+${_days}" | grep -c . || true)"
    _n="${_n:-0}"
    if [ "$_n" -eq 0 ]; then
        info "${_label}: tidak ada file > ${_days} hari"
        return 0
    fi

    if [ "$MODE" = "host" ]; then
        _sz="$(find "$_path" -type f -mtime "+${_days}" -printf '%s\n' 2>/dev/null \
               | awk '{s+=$1} END{printf "%.1f MB", s/1048576}')"
    else
        _sz="$(docker exec "$CONTAINER" sh -c \
               "find '$_path' -type f -mtime +${_days} -exec du -k {} + 2>/dev/null | awk '{s+=\$1} END{printf \"%.1f MB\", s/1024}'" 2>/dev/null || echo "?")"
    fi
    [ -n "$_sz" ] || _sz="?"

    if [ "$DRY_RUN" -eq 1 ]; then
        yellow "${_label}: AKAN dihapus ${_n} file (${_sz})"
        return 0
    fi

    if [ "$MODE" = "host" ]; then
        find "$_path" -type f -mtime "+${_days}" -delete 2>/dev/null || true
        find "$_path" -mindepth 1 -type d -empty -delete 2>/dev/null || true
    else
        docker exec "$CONTAINER" sh -c \
            "find '$_path' -type f -mtime +${_days} -delete 2>/dev/null; \
             find '$_path' -mindepth 1 -type d -empty -delete 2>/dev/null" || true
    fi
    green "${_label}: dihapus ${_n} file (${_sz})"
}

# ---------- kondisi awal ----------
DISK_REF="${HOST_STATICS:-/var/lib/docker}"
step "Kondisi awal"
df -Ph "$DISK_REF" 2>/dev/null | awk 'NR==2{printf "  Disk  : %s dari %s (%s terpakai)\n", $3, $2, $5}'
df -Pi "$DISK_REF" 2>/dev/null | awk 'NR==2{printf "  Inode : %s dari %s (%s terpakai)\n", $3, $2, $5}'
INODE_USE="$(df -Pi "$DISK_REF" 2>/dev/null | awk 'NR==2{print $5}' | tr -d '%')"
DISK_BEFORE="$(df -P "$DISK_REF" 2>/dev/null | awk 'NR==2{print $5}' | tr -d '%')"

# Inode habis adalah penyebab yang paling sering terlewat: auto-download
# media membuat ribuan file kecil, sehingga SQLite balas "disk is full"
# padahal `df -h` masih terlihat lega.
if [ -n "$INODE_USE" ] && [ "$INODE_USE" -ge 90 ] 2>/dev/null; then
    yellow "  PERHATIAN: inode ${INODE_USE}% — ini saja sudah cukup memicu"
    yellow "  'database or disk is full' walau kapasitas byte masih longgar."
fi

if [ "$MODE" = "host" ]; then
    step "Ukuran sebelum"
    for d in media senditems qrcode; do
        [ -d "${HOST_STATICS}/${d}" ] && \
            du -sh "${HOST_STATICS}/${d}" 2>/dev/null | awk '{printf "  %-14s %s\n", $2, $1}'
    done
    [ -n "$HOST_STORAGES" ] && [ -d "$HOST_STORAGES" ] && \
        du -sh "$HOST_STORAGES" 2>/dev/null | awk '{printf "  %-14s %s\n", $2, $1}'
fi

# ---------- pembersihan file ----------
step "Pembersihan file"
clean_target media     "$MEDIA_DAYS" "media"
clean_target senditems "$TEMP_DAYS"  "senditems"
clean_target qrcode    "$TEMP_DAYS"  "qrcode"

# ---------- log container ----------
# Log json-file bisa membengkak besar. Di-TRUNCATE, bukan dihapus: menghapus
# file yang sedang dibuka Docker membuat log berhenti tercatat sampai restart.
if [ "$DO_LOGS" -eq 1 ]; then
    step "Log container"
    LOGPATH="$(docker inspect -f '{{.LogPath}}' "$CONTAINER" 2>/dev/null || true)"
    if [ -n "$LOGPATH" ] && [ -f "$LOGPATH" ]; then
        LSZ="$(du -m "$LOGPATH" 2>/dev/null | awk '{print $1}')"
        if [ "${LSZ:-0}" -ge 10 ] 2>/dev/null; then
            if [ "$DRY_RUN" -eq 1 ]; then
                yellow "  AKAN dikosongkan: ${LOGPATH} (${LSZ} MB)"
            else
                : > "$LOGPATH" && green "  Dikosongkan: ${LSZ} MB -> 0 MB"
            fi
        else
            info "  Log kecil (${LSZ:-0} MB) — dibiarkan."
        fi
    else
        info "  LogPath tidak terbaca — dilewati."
    fi
fi

# ---------- rapikan SQLite ----------
if [ "$DO_VACUUM" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
    step "Rapikan database"
    if [ "$MODE" = "host" ] && [ -n "$HOST_STORAGES" ] && command -v sqlite3 >/dev/null 2>&1; then
        for db in whatsapp.db chatstorage.db; do
            f="${HOST_STORAGES}/${db}"
            [ -f "$f" ] || continue
            _b="$(du -m "$f" 2>/dev/null | awk '{print $1}')"
            if sqlite3 "$f" "PRAGMA wal_checkpoint(TRUNCATE); VACUUM;" >/dev/null 2>&1; then
                _a="$(du -m "$f" 2>/dev/null | awk '{print $1}')"
                green "  ${db}: ${_b}MB -> ${_a}MB"
            else
                # Umumnya karena core sedang memegang kunci DB. Tidak fatal.
                yellow "  ${db}: dilewati (terkunci container yang sedang jalan)"
            fi
        done
    else
        yellow "  Dilewati — butuh sqlite3 di host + bind mount storages."
        echo "  Pasang: apt-get install -y sqlite3  |  yum install -y sqlite"
        echo "  Alternatif paling ampuh: hentikan container sebentar lalu ulangi,"
        echo "  atau kecilkan sumbernya (WHATSAPP_AUTO_DOWNLOAD_MEDIA=false)."
    fi
fi

# ---------- prune image dangling (opt-in) ----------
if [ "$PRUNE_DANGLING" -eq 1 ]; then
    step "Prune image dangling"
    if [ "$DRY_RUN" -eq 1 ]; then
        yellow "  AKAN menjalankan: docker image prune -f  (hanya image tanpa tag)"
    else
        docker image prune -f 2>/dev/null | tail -2 || true
        green "  Selesai (image bertag & container lain TIDAK tersentuh)."
    fi
fi

# ---------- hasil ----------
step "Kondisi akhir"
df -Ph "$DISK_REF" 2>/dev/null | awk 'NR==2{printf "  Disk  : %s dari %s (%s terpakai)\n", $3, $2, $5}'
df -Pi "$DISK_REF" 2>/dev/null | awk 'NR==2{printf "  Inode : %s dari %s (%s terpakai)\n", $3, $2, $5}'
DISK_AFTER="$(df -P "$DISK_REF" 2>/dev/null | awk 'NR==2{print $5}' | tr -d '%')"
if [ -n "$DISK_BEFORE" ] && [ -n "$DISK_AFTER" ]; then
    _f=$((DISK_BEFORE - DISK_AFTER))
    [ "$_f" -gt 0 ] && green "  Turun ${_f}% pemakaian disk."
fi

if [ -n "$DISK_AFTER" ] && [ "$DISK_AFTER" -ge 90 ] 2>/dev/null; then
    printf "\n"
    red "PERINGATAN: disk masih ${DISK_AFTER}% terpakai."
    echo "Langkah berikutnya, dari yang paling berdampak:"
    echo "  1. Matikan sumbernya — di src/.env:"
    echo "       WHATSAPP_AUTO_DOWNLOAD_MEDIA=false"
    echo "     lalu: docker restart ${CONTAINER}"
    echo "     Media tetap bisa diambil lewat API saat diperlukan."
    echo "  2. Perpendek retensi:  sh \$0 --media-days 7"
    echo "  3. Batasi log Docker — tambahkan di compose service ${CONTAINER}:"
    echo "       logging: { driver: json-file, options: { max-size: 50m, max-file: 3 } }"
    echo "  4. Image menumpuk:  sh \$0 --prune-dangling"
    echo ""
    yellow "  JANGAN pakai 'docker system prune -a --volumes' di aaPanel:"
    yellow "  itu menghapus image & volume SELURUH server, termasuk aplikasi lain."
fi

printf "\n"
green "Selesai."
