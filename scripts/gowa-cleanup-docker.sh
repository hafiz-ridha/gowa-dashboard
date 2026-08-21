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
#   sh gowa-cleanup-docker.sh --all                  # SEMUA container gowa sekaligus
#   sh gowa-cleanup-docker.sh --list                 # daftar container gowa terdeteksi
#   sh gowa-cleanup-docker.sh --container gowa-core  # satu container tertentu
#   sh gowa-cleanup-docker.sh --media-days 14
#   sh gowa-cleanup-docker.sh --install-cron         # jadwal mingguan
#   sh gowa-cleanup-docker.sh --uninstall-cron
#
# BANYAK CONTAINER GOWA DI SATU SERVER:
#   Didukung penuh. `--all` memproses semuanya satu per satu, masing-masing
#   dengan mount & log-nya sendiri. Tanpa --all dan tanpa --container, kalau
#   terdeteksi lebih dari satu:
#     - ada terminal   -> ditampilkan daftar, Anda memilih
#     - tanpa terminal -> BERHENTI dan minta --all atau --container
#   Sengaja tidak menebak, supaya container yang salah tidak tersentuh.
#   Kegagalan pada satu container tidak menghentikan yang lain; ringkasan
#   di akhir menyebut berapa yang berhasil dan berapa yang gagal.
#
# ---------------------------------------------------------------------
# PENGAMAN (penting di aaPanel yang menjalankan banyak aplikasi):
#
#   * Path kerja diambil dari `docker inspect` container itu sendiri, lalu
#     diperiksa terhadap denylist (/, /etc, /var, /root, /www, dst) dan
#     wajib absolut + minimal 2 segmen + folder nyata. Gagal salah satu,
#     skrip menolak menyentuh apa pun.
#   * File .db TIDAK PERNAH dihapus — hanya media/qr/senditems.
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
ALL=0
LIST_ONLY=0
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
        --all)             ALL=1 ;;
        --list)            LIST_ONLY=1 ;;
        --no-vacuum)       DO_VACUUM=0 ;;
        --no-logs)         DO_LOGS=0 ;;
        --prune-dangling)  PRUNE_DANGLING=1 ;;
        --media-days)      shift; MEDIA_DAYS="${1:?butuh angka}" ;;
        --temp-days)       shift; TEMP_DAYS="${1:?butuh angka}" ;;
        --container)       shift; CONTAINER="${1:?butuh nama container}" ;;
        --install-cron)    ACTION=install ;;
        --uninstall-cron)  ACTION=uninstall ;;
        -h|--help)         sed -n '2,52p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
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
# Mengembalikan daftar (satu nama per baris). Pencocokan memakai IMAGE, bukan
# sekadar nama, supaya container lain yang kebetulan bernama mirip tidak ikut.
detect_containers() {
    _f=""
    for n in gowa-core whatsapp_go gowa_whatsapp_go_1 gowa-whatsapp_go-1; do
        if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$n"; then
            _f="${_f}${n}
"
        fi
    done
    _byimg="$(docker ps -a --format '{{.Names}}\t{{.Image}}' 2>/dev/null \
              | grep -iE 'go-whatsapp-web-multidevice|gowa' \
              | grep -viE 'dashboard' \
              | awk '{print $1}')"
    _f="${_f}${_byimg}"
    # Dedupe sambil mempertahankan urutan.
    printf '%s\n' "$_f" | grep -v '^$' | awk '!seen[$0]++' || true
}

describe() { # satu baris ringkas tentang container
    docker ps -a --filter "name=^${1}$" \
        --format '  {{.Names}}  |  {{.Image}}  |  {{.Status}}' 2>/dev/null || true
}

# ---------- tentukan daftar container yang akan diproses ----------
CANDIDATES="$(detect_containers)"
NCAND="$(printf '%s\n' "$CANDIDATES" | grep -c . || true)"
NCAND="${NCAND:-0}"

if [ "$LIST_ONLY" -eq 1 ]; then
    step "Container gowa terdeteksi (${NCAND})"
    if [ "$NCAND" -eq 0 ]; then
        echo "  (tidak ada)"
        echo ""
        echo "Lihat semua container:  docker ps -a --format '{{.Names}}\t{{.Image}}'"
    else
        printf '%s\n' "$CANDIDATES" | while IFS= read -r c; do
            [ -n "$c" ] && describe "$c"
        done
        echo ""
        echo "Bersihkan semuanya :  sh $0 --all --dry-run"
        echo "Satu saja          :  sh $0 --container NAMA --dry-run"
    fi
    exit 0
fi

TARGETS=""
if [ -n "$CONTAINER" ]; then
    docker inspect "$CONTAINER" >/dev/null 2>&1 || fail "container '$CONTAINER' tidak ada."
    TARGETS="$CONTAINER"
elif [ "$ALL" -eq 1 ]; then
    [ "$NCAND" -gt 0 ] || fail "tidak ada container gowa yang terdeteksi.
Lihat daftar:  docker ps -a --format '{{.Names}}\t{{.Image}}'"
    TARGETS="$CANDIDATES"
elif [ "$NCAND" -eq 0 ]; then
    fail "container gowa-core tidak ditemukan.
Lihat daftar:  docker ps -a --format '{{.Names}}\t{{.Image}}'
Lalu:          sh $0 --container NAMA-CONTAINER"
elif [ "$NCAND" -eq 1 ]; then
    TARGETS="$CANDIDATES"
else
    # Lebih dari satu. Kalau ada terminal, tawarkan pilihan; kalau tidak,
    # berhenti — menebak berisiko menyentuh container yang salah.
    step "Terdeteksi ${NCAND} container gowa"
    _i=0
    printf '%s\n' "$CANDIDATES" | while IFS= read -r c; do
        [ -n "$c" ] || continue
        describe "$c"
    done
    if [ -r /dev/tty ] && [ -w /dev/tty ]; then
        printf "\n" > /dev/tty
        printf "Pilih: [a]=semua, [nomor]=satu container, [q]=batal\n" > /dev/tty
        _i=0
        for c in $CANDIDATES; do
            [ -n "$c" ] || continue
            _i=$((_i + 1))
            printf "  %d) %s\n" "$_i" "$c" > /dev/tty
        done
        printf "Pilihan [a]: " > /dev/tty
        read -r pick < /dev/tty || pick="a"
        [ -n "$pick" ] || pick="a"
        case "$pick" in
            a|A|semua|all) TARGETS="$CANDIDATES" ;;
            q|Q|batal)     yellow "Dibatalkan."; exit 0 ;;
            ''|*[!0-9]*)   fail "pilihan tidak valid: '$pick'" ;;
            *)
                TARGETS="$(printf '%s\n' "$CANDIDATES" | grep -v '^$' | sed -n "${pick}p")"
                [ -n "$TARGETS" ] || fail "nomor '$pick' di luar daftar."
                ;;
        esac
    else
        fail "ada ${NCAND} container gowa dan tidak ada terminal untuk memilih.
Tentukan salah satu:
  sh $0 --all                    # proses semuanya
  sh $0 --container NAMA         # satu container saja
  sh $0 --list                   # lihat daftarnya dulu"
    fi
fi

NTARGET="$(printf '%s\n' "$TARGETS" | grep -c . || true)"
NTARGET="${NTARGET:-1}"

# ---------- pasang / cabut cron ----------
if [ "$ACTION" = "install" ]; then
    [ "$(id -u)" -eq 0 ] || fail "pasang cron harus root (pakai sudo)."
    SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
    [ "$SELF" = "$TARGET_BIN" ] || install -m 0755 "$SELF" "$TARGET_BIN"
    # Kalau target lebih dari satu, cron memakai --all supaya container yang
    # ditambahkan kemudian ikut terbersihkan tanpa perlu pasang ulang.
    if [ "$ALL" -eq 1 ] || [ "$NTARGET" -gt 1 ]; then
        SEL="--all"
    else
        SEL="--container $(printf '%s\n' "$TARGETS" | head -1)"
    fi
    cat > "$CRON_FILE" <<CRON
# gowa-cleanup-docker — pembersih penyimpanan mingguan (dipasang otomatis)
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
MAILTO=""
20 3 * * 0 root ${TARGET_BIN} ${SEL} --media-days ${MEDIA_DAYS} --temp-days ${TEMP_DAYS} >> /var/log/gowa-cleanup.log 2>&1
CRON
    chmod 0644 "$CRON_FILE"
    systemctl reload crond 2>/dev/null || systemctl reload cron 2>/dev/null || true
    green "OK: cron mingguan terpasang -> ${CRON_FILE}"
    echo "  Jadwal  : setiap Minggu 03:20"
    echo "  Cakupan : ${SEL}"
    echo "  Retensi : media ${MEDIA_DAYS} hari, temp ${TEMP_DAYS} hari"
    echo "  Log     : /var/log/gowa-cleanup.log"
    echo ""
    echo "Uji dulu tanpa menghapus:  sh ${TARGET_BIN} ${SEL} --dry-run"
    exit 0
fi
if [ "$ACTION" = "uninstall" ]; then
    [ "$(id -u)" -eq 0 ] || fail "cabut cron harus root (pakai sudo)."
    rm -f "$CRON_FILE"
    systemctl reload crond 2>/dev/null || systemctl reload cron 2>/dev/null || true
    green "OK: cron dicabut."
    exit 0
fi

# ---------- pengaman path (dipakai per container) ----------
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
    _depth="$(printf '%s' "$_p" | awk -F/ '{c=0; for(i=1;i<=NF;i++) if($i!="") c++; print c}')"
    [ "${_depth:-0}" -ge 2 ] || { red "TOLAK: ${_label} terlalu dangkal: '${_p}'"; return 1; }
    [ -d "$_p" ] || { red "TOLAK: ${_label} bukan folder: '${_p}'"; return 1; }
    return 0
}

# ---------- helper per container ----------
run_find() {
    if [ "$MODE" = "host" ]; then
        _base="$1"; shift
        find "$_base" "$@" 2>/dev/null || true
    else
        _base="$1"; shift
        docker exec "$CUR" sh -c "find '$_base' $* 2>/dev/null" || true
    fi
}

clean_target() { # $1=subdir  $2=hari  $3=label
    _sub="$1"; _days="$2"; _label="$3"
    if [ "$MODE" = "host" ]; then
        _path="${HOST_STATICS}/${_sub}"
        [ -d "$_path" ] || { info "${_label}: folder tidak ada, dilewati"; return 0; }
    else
        _path="/app/statics/${_sub}"
        docker exec "$CUR" test -d "$_path" 2>/dev/null \
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
        _sz="$(docker exec "$CUR" sh -c \
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
        docker exec "$CUR" sh -c \
            "find '$_path' -type f -mtime +${_days} -delete 2>/dev/null; \
             find '$_path' -mindepth 1 -type d -empty -delete 2>/dev/null" || true
    fi
    green "${_label}: dihapus ${_n} file (${_sz})"
}

# process_one CONTAINER — seluruh pekerjaan untuk SATU container.
# Return 0 = sukses, 1 = gagal (pemanggil lanjut ke container berikutnya).
process_one() {
    CUR="$1"
    printf "\n"
    printf "\033[1m========================================================\033[0m\n"
    printf "\033[1m Container: %s\033[0m\n" "$CUR"
    printf "\033[1m========================================================\033[0m\n"
    describe "$CUR"

    _running="$(docker inspect -f '{{.State.Running}}' "$CUR" 2>/dev/null || echo false)"

    HOST_STATICS="$(docker inspect -f \
      '{{range .Mounts}}{{if eq .Destination "/app/statics"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' \
      "$CUR" 2>/dev/null || true)"
    HOST_STORAGES="$(docker inspect -f \
      '{{range .Mounts}}{{if eq .Destination "/app/storages"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' \
      "$CUR" 2>/dev/null || true)"

    MODE=""
    if [ -n "$HOST_STATICS" ] && safe_path "$HOST_STATICS" "mount statics [$CUR]"; then
        MODE="host"
        info "Akses : bind mount -> ${HOST_STATICS}"
    else
        [ -z "$HOST_STATICS" ] || yellow "Bind mount tidak lolos pemeriksaan — beralih ke mode exec."
        if [ "$_running" != "true" ]; then
            red "[$CUR] penyimpanan bukan bind mount dan container sedang berhenti."
            echo "      Jalankan dulu:  docker start $CUR"
            return 1
        fi
        MODE="exec"
        info "Akses : named volume -> lewat 'docker exec'"
    fi

    step "Pembersihan file [$CUR]"
    clean_target media     "$MEDIA_DAYS" "media"
    clean_target senditems "$TEMP_DAYS"  "senditems"
    clean_target qrcode    "$TEMP_DAYS"  "qrcode"

    # Log json-file bisa membengkak. Di-TRUNCATE, bukan dihapus: menghapus
    # file yang sedang dibuka Docker membuat log berhenti tercatat.
    if [ "$DO_LOGS" -eq 1 ]; then
        step "Log container [$CUR]"
        _lp="$(docker inspect -f '{{.LogPath}}' "$CUR" 2>/dev/null || true)"
        if [ -n "$_lp" ] && [ -f "$_lp" ]; then
            _lsz="$(du -m "$_lp" 2>/dev/null | awk '{print $1}')"
            if [ "${_lsz:-0}" -ge 10 ] 2>/dev/null; then
                if [ "$DRY_RUN" -eq 1 ]; then
                    yellow "  AKAN dikosongkan: ${_lsz} MB"
                else
                    : > "$_lp" && green "  Dikosongkan: ${_lsz} MB -> 0 MB"
                fi
            else
                info "  Log kecil (${_lsz:-0} MB) — dibiarkan."
            fi
        else
            info "  LogPath tidak terbaca — dilewati."
        fi
    fi

    if [ "$DO_VACUUM" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
        step "Rapikan database [$CUR]"
        if [ "$MODE" = "host" ] && [ -n "$HOST_STORAGES" ] && command -v sqlite3 >/dev/null 2>&1; then
            for db in whatsapp.db chatstorage.db; do
                f="${HOST_STORAGES}/${db}"
                [ -f "$f" ] || continue
                _b="$(du -m "$f" 2>/dev/null | awk '{print $1}')"
                if sqlite3 "$f" "PRAGMA wal_checkpoint(TRUNCATE); VACUUM;" >/dev/null 2>&1; then
                    _a="$(du -m "$f" 2>/dev/null | awk '{print $1}')"
                    green "  ${db}: ${_b}MB -> ${_a}MB"
                else
                    yellow "  ${db}: dilewati (terkunci container yang sedang jalan)"
                fi
            done
        else
            yellow "  Dilewati — butuh sqlite3 di host + bind mount storages."
        fi
    fi
    return 0
}

# ---------- mulai ----------
printf "\n===== gowa-cleanup-docker  %s =====\n" "$(date '+%Y-%m-%d %H:%M:%S')"
info "Target  : ${NTARGET} container"
info "Retensi : media ${MEDIA_DAYS} hari, temp ${TEMP_DAYS} hari"
[ "$DRY_RUN" -eq 1 ] && yellow "MODE DRY-RUN — tidak ada yang dihapus."

# Referensi disk: pakai mount container pertama kalau ada, kalau tidak jatuh
# ke /var/lib/docker.
FIRST="$(printf '%s\n' "$TARGETS" | grep -v '^$' | head -1)"
DISK_REF="$(docker inspect -f \
  '{{range .Mounts}}{{if eq .Destination "/app/statics"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' \
  "$FIRST" 2>/dev/null || true)"
[ -n "$DISK_REF" ] && [ -d "$DISK_REF" ] || DISK_REF="/var/lib/docker"

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

# ---------- loop semua container ----------
# Kegagalan satu container TIDAK menghentikan yang lain; hasilnya diringkas
# di akhir supaya jelas mana yang perlu ditindaklanjuti.
OK_COUNT=0
FAIL_COUNT=0
FAILED_NAMES=""
for c in $TARGETS; do
    [ -n "$c" ] || continue
    if process_one "$c"; then
        OK_COUNT=$((OK_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
        FAILED_NAMES="${FAILED_NAMES} ${c}"
    fi
done

# ---------- prune image dangling (opt-in, global) ----------
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
step "Ringkasan"
echo "  Container diproses : ${OK_COUNT} berhasil, ${FAIL_COUNT} gagal"
if [ "$FAIL_COUNT" -gt 0 ]; then
    red "  Gagal:${FAILED_NAMES}"
    echo "  Lihat pesan di atas untuk sebabnya."
fi
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
    echo "  1. Matikan sumbernya:  sh gowa-disable-media.sh --all"
    echo "     Media tetap bisa diambil lewat API saat diperlukan."
    echo "  2. Perpendek retensi:  sh \$0 --all --media-days 7"
    echo "  3. Batasi log Docker di compose:"
    echo "       logging: { driver: json-file, options: { max-size: 50m, max-file: 3 } }"
    echo "  4. Image menumpuk:  sh \$0 --prune-dangling"
    echo ""
    yellow "  JANGAN pakai 'docker system prune -a --volumes' di aaPanel:"
    yellow "  itu menghapus image & volume SELURUH server, termasuk aplikasi lain."
fi

printf "\n"
if [ "$FAIL_COUNT" -gt 0 ]; then
    yellow "Selesai dengan ${FAIL_COUNT} container gagal."
    exit 1
fi
green "Selesai."
