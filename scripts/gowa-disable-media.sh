#!/bin/sh
# =====================================================================
# gowa-disable-media — matikan auto-download media pada gowa-core (Docker)
# =====================================================================
#
# Menghentikan penyebab utama "database or disk is full":
# WHATSAPP_AUTO_DOWNLOAD_MEDIA default-nya `true`, sehingga setiap foto,
# video, voice note, dokumen, dan stiker yang masuk ditulis jadi file di
# statics/media SELAMANYA — gowa tidak punya retensi bawaan (masih berlaku
# di v9.1.0).
#
# Mematikannya TIDAK menghilangkan kemampuan mengambil media: media tetap
# bisa diunduh lewat REST API saat dibutuhkan. Yang berhenti hanyalah
# penyimpanan otomatis ke disk.
#
# PAKAI:
#   sh gowa-disable-media.sh --status     # lihat kondisi sekarang (aman)
#   sh gowa-disable-media.sh --dry-run    # lihat rencananya, tanpa mengubah
#   sh gowa-disable-media.sh              # matikan (minta konfirmasi)
#   sh gowa-disable-media.sh --yes        # matikan tanpa tanya (untuk skrip)
#   sh gowa-disable-media.sh --enable     # nyalakan kembali
#
# ---------------------------------------------------------------------
# KENAPA CONTAINER HARUS DIBUAT ULANG, BUKAN SEKADAR RESTART
#
# gowa membaca konfigurasi lewat viper.AutomaticEnv(), jadi yang dipakai
# adalah ENV VAR container. Docker membekukan env var saat container
# DIBUAT — `docker restart` memakai ulang env lama dan perubahan pada
# env_file TIDAK terbaca. Karena itu skrip ini menjalankan
# `docker compose up -d` yang membuat ulang container saat config berubah,
# lalu MEMVERIFIKASI nilainya dari dalam container yang sudah jalan.
#
# Sesi WhatsApp aman: kredensial tersimpan di storages/whatsapp.db (volume),
# bukan di dalam container. Setelah dibuat ulang, core menyambung sendiri
# tanpa perlu scan QR ulang.
# ---------------------------------------------------------------------

set -e

VAR_NAME="WHATSAPP_AUTO_DOWNLOAD_MEDIA"
TARGET_VALUE="false"
CONTAINER="${GOWA_CONTAINER:-}"
ASSUME_YES=0
DRY_RUN=0
MODE="disable"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }
step()  { printf "\n\033[1m-- %s\033[0m\n" "$*"; }
fail()  { red "GAGAL: $*"; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --status)     MODE="status" ;;
        --enable)     MODE="enable"; TARGET_VALUE="true" ;;
        --dry-run)    DRY_RUN=1 ;;
        --yes|-y)     ASSUME_YES=1 ;;
        --container)  shift; CONTAINER="${1:?butuh nama container}" ;;
        -h|--help)    sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) fail "argumen tidak dikenal: $1  (pakai --help)" ;;
    esac
    shift
done

command -v docker >/dev/null 2>&1 || fail "docker tidak ditemukan di PATH."
docker info >/dev/null 2>&1 || fail "tidak bisa bicara dengan Docker daemon.
Jalankan sebagai root (sudo), atau pastikan Docker berjalan."

# ---------- deteksi container (tidak menebak saat ambigu) ----------
detect_container() {
    _f=""
    for n in gowa-core whatsapp_go gowa_whatsapp_go_1 gowa-whatsapp_go-1; do
        docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$n" && _f="${_f}${n}
"
    done
    if [ -z "$_f" ]; then
        _f="$(docker ps -a --format '{{.Names}}\t{{.Image}}' 2>/dev/null \
              | grep -iE 'go-whatsapp-web-multidevice|gowa' \
              | grep -viE 'dashboard' | awk '{print $1}')"
    fi
    printf '%s' "$_f" | grep -v '^$' || true
}

if [ -z "$CONTAINER" ]; then
    CAND="$(detect_container)"
    _n="$(printf '%s\n' "$CAND" | grep -c . || true)"
    if [ "${_n:-0}" -eq 0 ]; then
        fail "container gowa-core tidak ditemukan.
Lihat daftar:  docker ps -a --format '{{.Names}}\t{{.Image}}'
Lalu:          sh $0 --container NAMA-CONTAINER"
    elif [ "${_n:-0}" -gt 1 ]; then
        red "Ditemukan lebih dari satu kandidat:"
        printf '%s\n' "$CAND" | sed 's/^/    /' >&2
        fail "ambigu — tentukan dengan --container NAMA
(sengaja tidak menebak supaya container lain tidak tersentuh)."
    fi
    CONTAINER="$(printf '%s\n' "$CAND" | head -1)"
fi
docker inspect "$CONTAINER" >/dev/null 2>&1 || fail "container '$CONTAINER' tidak ada."

RUNNING="$(docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null || echo false)"

# ---------- baca nilai yang SEDANG BERLAKU di container ----------
# Sumber kebenaran: env var container itu sendiri, bukan isi file di host
# (file bisa saja sudah diubah tapi container belum dibuat ulang).
current_value() {
    docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$CONTAINER" 2>/dev/null \
        | grep "^${VAR_NAME}=" | head -1 | cut -d= -f2- || true
}
CURRENT="$(current_value)"

step "Kondisi sekarang"
info "Container : ${CONTAINER}  ($([ "$RUNNING" = "true" ] && echo running || echo stopped))"
if [ -z "$CURRENT" ]; then
    yellow "  ${VAR_NAME} tidak di-set di container."
    echo "  Artinya memakai DEFAULT gowa = true (auto-download AKTIF)."
    EFFECTIVE="true"
else
    info "  ${VAR_NAME}=${CURRENT}"
    EFFECTIVE="$CURRENT"
fi

if [ "$EFFECTIVE" = "true" ]; then
    yellow "  Status: auto-download media AKTIF — statics/media terus bertambah."
else
    green "  Status: auto-download media sudah MATI."
fi

if [ "$MODE" = "status" ]; then
    printf "\n"
    if [ "$EFFECTIVE" = "true" ]; then
        echo "Matikan dengan:  sh $0"
    fi
    exit 0
fi

if [ "$EFFECTIVE" = "$TARGET_VALUE" ]; then
    printf "\n"
    green "Sudah bernilai '${TARGET_VALUE}' — tidak ada yang perlu diubah."
    exit 0
fi

# ---------- temukan file env yang dipakai compose ----------
step "Mencari file konfigurasi"

PROJ_DIR="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$CONTAINER" 2>/dev/null || true)"
CFG_FILES="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$CONTAINER" 2>/dev/null || true)"
SERVICE="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$CONTAINER" 2>/dev/null || true)"

[ -n "$PROJ_DIR" ] || fail "container ini TIDAK dikelola docker compose.
Env var Docker hanya bisa diubah dengan membuat ulang container, dan skrip
ini tidak akan menebak cara container non-compose dibuat (risiko kehilangan
opsi port/volume/network Anda).

Cara aman untuk container non-compose:
  1. Catat konfigurasinya:  docker inspect ${CONTAINER}
  2. Buat ulang container dengan menambahkan:
       -e ${VAR_NAME}=${TARGET_VALUE}
  Atau kelola lewat docker-compose supaya perubahan seperti ini mudah."

info "Project dir : ${PROJ_DIR}"
info "Compose file: ${CFG_FILES:-（default）}"
info "Service     : ${SERVICE:-?}"

[ -d "$PROJ_DIR" ] || fail "folder project '${PROJ_DIR}' tidak ada di server ini.
Container mungkin dibuat di host/lokasi lain."

# Cari file env yang wajar: src/.env adalah lokasi standar gowa.
ENVFILE=""
for c in "${PROJ_DIR}/src/.env" "${PROJ_DIR}/.env"; do
    [ -f "$c" ] && { ENVFILE="$c"; break; }
done

if [ -z "$ENVFILE" ]; then
    # Belum ada .env — buat dari contoh kalau tersedia, kalau tidak buat baru.
    if [ -f "${PROJ_DIR}/src/.env.example" ]; then
        ENVFILE="${PROJ_DIR}/src/.env"
        yellow "  src/.env belum ada — akan dibuat dari src/.env.example"
        [ "$DRY_RUN" -eq 1 ] || cp "${PROJ_DIR}/src/.env.example" "$ENVFILE"
    elif [ -d "${PROJ_DIR}/src" ]; then
        ENVFILE="${PROJ_DIR}/src/.env"
        yellow "  src/.env belum ada — akan dibuat baru"
        [ "$DRY_RUN" -eq 1 ] || : > "$ENVFILE"
    else
        fail "tidak menemukan src/.env maupun .env di ${PROJ_DIR}."
    fi
fi
info "File env    : ${ENVFILE}"

# ---------- konfirmasi ----------
step "Rencana perubahan"
echo "  1. Backup   : ${ENVFILE}.bak.<timestamp>"
echo "  2. Set      : ${VAR_NAME}=${TARGET_VALUE}  di ${ENVFILE}"
echo "  3. Recreate : docker compose up -d ${SERVICE}"
echo "     (WAJIB — 'docker restart' tidak membaca ulang env_file)"
echo "  4. Verifikasi nilai dari DALAM container yang sudah jalan"
echo ""
yellow "  Container akan mati beberapa detik saat dibuat ulang."
echo "  Sesi WhatsApp AMAN — tersimpan di storages/whatsapp.db (volume),"
echo "  jadi tidak perlu scan QR ulang."

if [ "$DRY_RUN" -eq 1 ]; then
    printf "\n"
    yellow "MODE DRY-RUN — tidak ada yang diubah."
    exit 0
fi

if [ "$ASSUME_YES" -ne 1 ]; then
    printf "\nLanjutkan? [y/N]: "
    if [ -r /dev/tty ]; then
        read -r ans < /dev/tty || ans=""
    else
        read -r ans || ans=""
    fi
    case "$ans" in
        y|Y|ya|YA|yes|YES) : ;;
        *) yellow "Dibatalkan — tidak ada yang diubah."; exit 0 ;;
    esac
fi

# ---------- tulis perubahan ----------
step "Menulis konfigurasi"
BACKUP="${ENVFILE}.bak.$(date +%Y%m%d-%H%M%S)"
cp "$ENVFILE" "$BACKUP"
info "Backup: ${BACKUP}"

# awk + ENVIRON dipakai agar nilai ditulis apa adanya tanpa interpretasi
# escape — konsisten dengan skrip lain di repo ini.
SET_VAL="$TARGET_VALUE" awk -v k="$VAR_NAME" '
    index($0, k "=") == 1 { print k "=" ENVIRON["SET_VAL"]; found = 1; next }
    { print }
    END { if (!found) print k "=" ENVIRON["SET_VAL"] }
' "$ENVFILE" > "${ENVFILE}.tmp" && mv "${ENVFILE}.tmp" "$ENVFILE"

grep -qx "${VAR_NAME}=${TARGET_VALUE}" "$ENVFILE" \
    || { cp "$BACKUP" "$ENVFILE"; fail "gagal menulis ${VAR_NAME} — file dikembalikan dari backup."; }
green "OK: ${VAR_NAME}=${TARGET_VALUE}"

# ---------- buat ulang container ----------
step "Membuat ulang container"
if docker compose version >/dev/null 2>&1; then
    DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
    DC="docker-compose"
else
    red "Docker Compose tidak ditemukan."
    yellow "File env sudah diubah. Buat ulang container secara manual:"
    echo "  cd ${PROJ_DIR} && docker compose up -d ${SERVICE}"
    exit 1
fi

# -f dipakai kalau label menyebut file compose non-default; kalau berisi
# beberapa file (dipisah koma), semuanya diteruskan sesuai urutan.
DC_ARGS=""
if [ -n "$CFG_FILES" ]; then
    _ifs="$IFS"; IFS=','
    for f in $CFG_FILES; do
        [ -n "$f" ] && DC_ARGS="${DC_ARGS} -f ${f}"
    done
    IFS="$_ifs"
fi

info "Menjalankan: ${DC}${DC_ARGS} up -d ${SERVICE}"
# shellcheck disable=SC2086
if ! ( cd "$PROJ_DIR" && $DC $DC_ARGS up -d ${SERVICE} ); then
    red "docker compose gagal. File env sudah diubah (backup: ${BACKUP})."
    fail "jalankan manual:  cd ${PROJ_DIR} && ${DC} up -d ${SERVICE}"
fi

# ---------- verifikasi ----------
step "Verifikasi"
i=0
while [ "$i" -lt 15 ]; do
    docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -qx true && break
    i=$((i + 1)); sleep 1
done

NEW="$(current_value)"
if [ "$NEW" = "$TARGET_VALUE" ]; then
    green "OK: container sekarang menjalankan ${VAR_NAME}=${NEW}"
else
    red "Nilai di container masih '${NEW:-<kosong>}', bukan '${TARGET_VALUE}'."
    echo "Kemungkinan penyebab:"
    echo "  - compose memakai env_file lain (cek ${CFG_FILES:-docker-compose.yml})"
    echo "  - ada blok 'environment:' di compose yang menimpa env_file"
    echo "    (environment: menang atas env_file — hapus/ubah di sana)"
    fail "perubahan belum berlaku."
fi

if [ "$RUNNING" = "true" ]; then
    docker ps --filter "name=${CONTAINER}" --format '  {{.Names}}  {{.Status}}' 2>/dev/null || true
fi

printf "\n"
if [ "$MODE" = "disable" ]; then
    green "Auto-download media MATI."
    echo "  Media masuk tidak lagi ditulis ke statics/media."
    echo "  Media tetap bisa diambil lewat REST API saat diperlukan."
    echo ""
    echo "Bersihkan sisa file lama:"
    echo "  sh gowa-cleanup-docker.sh --dry-run"
else
    green "Auto-download media AKTIF kembali."
    yellow "  Ingat: statics/media akan tumbuh lagi tanpa batas."
    echo "  Pasang pembersih mingguan:  sh gowa-cleanup-docker.sh --install-cron"
fi
echo ""
echo "Kembalikan config lama kalau perlu:"
echo "  cp ${BACKUP} ${ENVFILE} && cd ${PROJ_DIR} && ${DC} up -d ${SERVICE}"
