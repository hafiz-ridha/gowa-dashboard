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
#   sh gowa-disable-media.sh --list       # daftar container gowa terdeteksi
#   sh gowa-disable-media.sh --status     # lihat kondisi sekarang (aman)
#   sh gowa-disable-media.sh --dry-run    # lihat rencananya, tanpa mengubah
#   sh gowa-disable-media.sh              # matikan (minta konfirmasi)
#   sh gowa-disable-media.sh --all        # SEMUA container gowa sekaligus
#   sh gowa-disable-media.sh --yes        # tanpa tanya (untuk otomasi)
#   sh gowa-disable-media.sh --enable     # nyalakan kembali
#
# BANYAK CONTAINER GOWA DI SATU SERVER:
#   Didukung penuh. Tiap container diproses sendiri-sendiri, termasuk kalau
#   masing-masing berada di compose project berbeda (env file & service-nya
#   dicari terpisah lewat label compose container itu). `--status --all`
#   menampilkan tabel ringkas semua container sekaligus.
#   Tanpa --all dan tanpa --container, kalau terdeteksi lebih dari satu:
#     - ada terminal   -> ditampilkan daftar, Anda memilih
#     - tanpa terminal -> BERHENTI dan minta --all atau --container
#   Kegagalan satu container tidak menghentikan yang lain.
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
ALL=0
LIST_ONLY=0
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
        --all)        ALL=1 ;;
        --list)       LIST_ONLY=1 ;;
        --yes|-y)     ASSUME_YES=1 ;;
        --container)  shift; CONTAINER="${1:?butuh nama container}" ;;
        -h|--help)    sed -n '2,48p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) fail "argumen tidak dikenal: $1  (pakai --help)" ;;
    esac
    shift
done

command -v docker >/dev/null 2>&1 || fail "docker tidak ditemukan di PATH."
docker info >/dev/null 2>&1 || fail "tidak bisa bicara dengan Docker daemon.
Jalankan sebagai root (sudo), atau pastikan Docker berjalan."

# ---------- deteksi container ----------
detect_containers() {
    _f=""
    for n in gowa-core whatsapp_go gowa_whatsapp_go_1 gowa-whatsapp_go-1; do
        docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$n" && _f="${_f}${n}
"
    done
    _byimg="$(docker ps -a --format '{{.Names}}\t{{.Image}}' 2>/dev/null \
              | grep -iE 'go-whatsapp-web-multidevice|gowa' \
              | grep -viE 'dashboard' | awk '{print $1}')"
    _f="${_f}${_byimg}"
    printf '%s\n' "$_f" | grep -v '^$' | awk '!seen[$0]++' || true
}

describe() {
    docker ps -a --filter "name=^${1}$" \
        --format '  {{.Names}}  |  {{.Image}}  |  {{.Status}}' 2>/dev/null || true
}

# Nilai yang SEDANG BERLAKU di container — sumber kebenaran adalah env var
# container itu sendiri, bukan isi file di host (file bisa saja sudah diubah
# tapi container belum dibuat ulang).
current_value() {
    docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1" 2>/dev/null \
        | grep "^${VAR_NAME}=" | head -1 | cut -d= -f2- || true
}
effective_value() {
    _v="$(current_value "$1")"
    # Tidak di-set = memakai default gowa = true.
    [ -n "$_v" ] || _v="true"
    printf '%s' "$_v"
}

CANDIDATES="$(detect_containers)"
NCAND="$(printf '%s\n' "$CANDIDATES" | grep -c . || true)"
NCAND="${NCAND:-0}"

if [ "$LIST_ONLY" -eq 1 ]; then
    step "Container gowa terdeteksi (${NCAND})"
    if [ "$NCAND" -eq 0 ]; then
        echo "  (tidak ada)"
    else
        for c in $CANDIDATES; do
            [ -n "$c" ] || continue
            describe "$c"
            printf "      %s = %s\n" "$VAR_NAME" "$(effective_value "$c")"
        done
    fi
    exit 0
fi

# ---------- tentukan target ----------
TARGETS=""
if [ -n "$CONTAINER" ]; then
    docker inspect "$CONTAINER" >/dev/null 2>&1 || fail "container '$CONTAINER' tidak ada."
    TARGETS="$CONTAINER"
elif [ "$ALL" -eq 1 ]; then
    [ "$NCAND" -gt 0 ] || fail "tidak ada container gowa yang terdeteksi."
    TARGETS="$CANDIDATES"
elif [ "$NCAND" -eq 0 ]; then
    fail "container gowa-core tidak ditemukan.
Lihat daftar:  docker ps -a --format '{{.Names}}\t{{.Image}}'
Lalu:          sh $0 --container NAMA-CONTAINER"
elif [ "$NCAND" -eq 1 ]; then
    TARGETS="$CANDIDATES"
elif [ "$MODE" = "status" ]; then
    # --status hanya membaca, jadi aman menampilkan semuanya sekaligus.
    TARGETS="$CANDIDATES"
else
    step "Terdeteksi ${NCAND} container gowa"
    for c in $CANDIDATES; do
        [ -n "$c" ] || continue
        describe "$c"
        printf "      %s = %s\n" "$VAR_NAME" "$(effective_value "$c")"
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
  sh $0 --all                # proses semuanya
  sh $0 --container NAMA     # satu container saja
  sh $0 --list               # lihat daftar + status masing-masing"
    fi
fi

NTARGET="$(printf '%s\n' "$TARGETS" | grep -c . || true)"
NTARGET="${NTARGET:-1}"

# ---------- MODE status: hanya laporkan ----------
if [ "$MODE" = "status" ]; then
    step "Status auto-download media (${NTARGET} container)"
    _need=0
    for c in $TARGETS; do
        [ -n "$c" ] || continue
        _e="$(effective_value "$c")"
        _raw="$(current_value "$c")"
        printf "\n"
        describe "$c"
        if [ -z "$_raw" ]; then
            printf "      %s tidak di-set -> pakai DEFAULT gowa = true\n" "$VAR_NAME"
        else
            printf "      %s=%s\n" "$VAR_NAME" "$_raw"
        fi
        if [ "$_e" = "true" ]; then
            yellow "      AKTIF — statics/media terus bertambah."
            _need=$((_need + 1))
        else
            green "      MATI — aman."
        fi
    done
    printf "\n"
    if [ "$_need" -gt 0 ]; then
        echo "${_need} container masih aktif. Matikan dengan:"
        [ "$NTARGET" -gt 1 ] && echo "  sh $0 --all" || echo "  sh $0"
    else
        green "Semua container sudah mati auto-download-nya."
    fi
    exit 0
fi

# ---------- proses satu container ----------
# Return 0 = berhasil/tidak perlu diubah, 1 = gagal.
process_one() {
    CUR="$1"
    printf "\n"
    printf "\033[1m========================================================\033[0m\n"
    printf "\033[1m Container: %s\033[0m\n" "$CUR"
    printf "\033[1m========================================================\033[0m\n"
    describe "$CUR"

    _eff="$(effective_value "$CUR")"
    _raw="$(current_value "$CUR")"
    if [ -z "$_raw" ]; then
        info "${VAR_NAME} tidak di-set -> default gowa = true (AKTIF)"
    else
        info "${VAR_NAME}=${_raw}"
    fi

    if [ "$_eff" = "$TARGET_VALUE" ]; then
        green "Sudah bernilai '${TARGET_VALUE}' — tidak ada yang perlu diubah."
        return 0
    fi

    # --- temukan file env lewat label compose container INI ---
    # Tiap container bisa berada di project berbeda, jadi ini harus dicari
    # ulang per container, bukan sekali di awal.
    _proj="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$CUR" 2>/dev/null || true)"
    _cfg="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$CUR" 2>/dev/null || true)"
    _svc="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$CUR" 2>/dev/null || true)"

    if [ -z "$_proj" ]; then
        red "[$CUR] TIDAK dikelola docker compose."
        echo "      Env var Docker hanya bisa diubah dengan membuat ulang container,"
        echo "      dan skrip ini tidak menebak cara container non-compose dibuat"
        echo "      (risiko kehilangan opsi port/volume/network Anda)."
        echo "      Buat ulang manual dengan menambahkan:  -e ${VAR_NAME}=${TARGET_VALUE}"
        return 1
    fi
    if [ ! -d "$_proj" ]; then
        red "[$CUR] folder project '${_proj}' tidak ada di server ini."
        return 1
    fi

    info "Project : ${_proj}"
    info "Service : ${_svc:-?}"

    _envfile=""
    for cand in "${_proj}/src/.env" "${_proj}/.env"; do
        [ -f "$cand" ] && { _envfile="$cand"; break; }
    done
    if [ -z "$_envfile" ]; then
        if [ -f "${_proj}/src/.env.example" ]; then
            _envfile="${_proj}/src/.env"
            yellow "  src/.env belum ada — dibuat dari src/.env.example"
            [ "$DRY_RUN" -eq 1 ] || cp "${_proj}/src/.env.example" "$_envfile"
        elif [ -d "${_proj}/src" ]; then
            _envfile="${_proj}/src/.env"
            yellow "  src/.env belum ada — dibuat baru"
            [ "$DRY_RUN" -eq 1 ] || : > "$_envfile"
        else
            red "[$CUR] tidak menemukan src/.env maupun .env di ${_proj}."
            return 1
        fi
    fi
    info "Env file: ${_envfile}"

    if [ "$DRY_RUN" -eq 1 ]; then
        yellow "  DRY-RUN: akan set ${VAR_NAME}=${TARGET_VALUE} lalu recreate '${_svc}'."
        return 0
    fi

    # --- tulis + recreate ---
    _backup="${_envfile}.bak.$(date +%Y%m%d-%H%M%S)"
    cp "$_envfile" "$_backup"
    info "Backup  : ${_backup}"

    SET_VAL="$TARGET_VALUE" awk -v k="$VAR_NAME" '
        index($0, k "=") == 1 { print k "=" ENVIRON["SET_VAL"]; found = 1; next }
        { print }
        END { if (!found) print k "=" ENVIRON["SET_VAL"] }
    ' "$_envfile" > "${_envfile}.tmp" && mv "${_envfile}.tmp" "$_envfile"

    if ! grep -qx "${VAR_NAME}=${TARGET_VALUE}" "$_envfile"; then
        cp "$_backup" "$_envfile"
        red "[$CUR] gagal menulis ${VAR_NAME} — file dikembalikan dari backup."
        return 1
    fi
    green "  ${VAR_NAME}=${TARGET_VALUE} ditulis."

    _dcargs=""
    if [ -n "$_cfg" ]; then
        _ifs="$IFS"; IFS=','
        for f in $_cfg; do
            [ -n "$f" ] && _dcargs="${_dcargs} -f ${f}"
        done
        IFS="$_ifs"
    fi

    info "Recreate: ${DC}${_dcargs} up -d ${_svc}"
    # shellcheck disable=SC2086
    if ! ( cd "$_proj" && $DC $_dcargs up -d ${_svc} ); then
        red "[$CUR] docker compose gagal. File env sudah diubah (backup: ${_backup})."
        echo "      Jalankan manual:  cd ${_proj} && ${DC} up -d ${_svc}"
        return 1
    fi

    # --- verifikasi dari dalam container yang sudah jalan ---
    _i=0
    while [ "$_i" -lt 15 ]; do
        docker inspect -f '{{.State.Running}}' "$CUR" 2>/dev/null | grep -qx true && break
        _i=$((_i + 1)); sleep 1
    done
    _new="$(current_value "$CUR")"
    if [ "$_new" = "$TARGET_VALUE" ]; then
        green "  Terverifikasi: container menjalankan ${VAR_NAME}=${_new}"
        printf "  Rollback bila perlu: cp %s %s && cd %s && %s up -d %s\n" \
               "$_backup" "$_envfile" "$_proj" "$DC" "$_svc"
        return 0
    fi
    red "[$CUR] nilai di container masih '${_new:-<kosong>}', bukan '${TARGET_VALUE}'."
    echo "      Kemungkinan ada blok 'environment:' di compose yang menimpa"
    echo "      env_file (environment: menang atas env_file). Cek ${_cfg:-docker-compose.yml}."
    return 1
}

# ---------- compose runner ----------
if docker compose version >/dev/null 2>&1; then
    DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
    DC="docker-compose"
else
    fail "Docker Compose tidak ditemukan (butuh 'docker compose' atau 'docker-compose')."
fi

# ---------- ringkasan rencana + konfirmasi ----------
step "Rencana"
echo "  Aksi      : set ${VAR_NAME}=${TARGET_VALUE}"
echo "  Container : ${NTARGET}"
for c in $TARGETS; do
    [ -n "$c" ] && printf "    - %s (sekarang: %s)\n" "$c" "$(effective_value "$c")"
done
echo ""
echo "  Tiap container: backup .env -> tulis -> 'compose up -d' (recreate) -> verifikasi"
echo "  'docker restart' TIDAK dipakai karena tidak membaca ulang env_file."
yellow "  Container akan mati beberapa detik saat dibuat ulang."
echo "  Sesi WhatsApp AMAN — tersimpan di storages/whatsapp.db (volume)."

if [ "$DRY_RUN" -eq 1 ]; then
    printf "\n"
    yellow "MODE DRY-RUN — tidak ada yang diubah."
fi

if [ "$DRY_RUN" -eq 0 ] && [ "$ASSUME_YES" -ne 1 ]; then
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

# ---------- loop ----------
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

step "Ringkasan"
echo "  ${OK_COUNT} berhasil, ${FAIL_COUNT} gagal (dari ${NTARGET} container)"
if [ "$FAIL_COUNT" -gt 0 ]; then
    red "  Gagal:${FAILED_NAMES}"
    echo "  Lihat pesan di atas untuk sebabnya."
fi

printf "\n"
if [ "$DRY_RUN" -eq 1 ]; then
    green "Dry-run selesai — tidak ada perubahan."
    exit 0
fi
if [ "$MODE" = "disable" ]; then
    green "Auto-download media MATI pada ${OK_COUNT} container."
    echo "  Media masuk tidak lagi ditulis ke statics/media."
    echo "  Media tetap bisa diambil lewat REST API saat diperlukan."
    echo ""
    echo "Bersihkan sisa file lama:"
    echo "  sh gowa-cleanup-docker.sh --all --dry-run"
else
    green "Auto-download media AKTIF kembali pada ${OK_COUNT} container."
    yellow "  Ingat: statics/media akan tumbuh lagi tanpa batas."
    echo "  Pasang pembersih mingguan:  sh gowa-cleanup-docker.sh --all --install-cron"
fi

[ "$FAIL_COUNT" -eq 0 ] || exit 1
