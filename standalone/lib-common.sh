#!/bin/sh
# =====================================================================
# Fungsi bersama untuk install.sh (systemd) dan bootstrap.sh (docker)
# =====================================================================
#
# File ini di-source, bukan dijalankan langsung.
#
# ALASAN ADANYA FILE INI: logika basic-auth sempat ditulis dua kali —
# sekali di install.sh, sekali di mode docker bootstrap.sh. Keduanya langsung
# menyimpang: versi docker kehilangan prompt interaktif DAN kehilangan
# validasi format. Yang kedua berbahaya senyap, karena core hanya memasang
# middleware auth kalau nilainya memuat ':' (dashboard/main.go: SplitN lalu
# `if len(parts) == 2`). Nilai tanpa ':' membuat dashboard TERBUKA tanpa
# peringatan apa pun. Satu sumber kebenaran mencegah pengulangan itu.
#
# Pemanggil diharapkan sudah menyediakan: fail, red, green, yellow, info.

# Fallback ringan kalau pemanggil belum mendefinisikannya.
command -v fail   >/dev/null 2>&1 || fail()   { printf "GAGAL: %s\n" "$*" >&2; exit 1; }
command -v red    >/dev/null 2>&1 || red()    { printf "%s\n" "$*" >&2; }
command -v green  >/dev/null 2>&1 || green()  { printf "%s\n" "$*"; }
command -v yellow >/dev/null 2>&1 || yellow() { printf "%s\n" "$*"; }
command -v info   >/dev/null 2>&1 || info()   { printf "[*] %s\n" "$*"; }

# ---------------------------------------------------------------------
# set_env KEY VALUE FILE — tulis nilai ke file .env tanpa masalah escaping.
#
# Sengaja TIDAK memakai sed: password bisa memuat karakter yang punya arti
# khusus di sed (| / & \) dan akan merusak perintahnya. awk + ENVIRON
# meneruskan nilai apa adanya, tanpa interpretasi escape sama sekali.
# ---------------------------------------------------------------------
set_env() {
    _k="$1"; _v="$2"; _f="$3"
    SET_ENV_VAL="$_v" awk -v k="$_k" '
        index($0, k "=") == 1 { print k "=" ENVIRON["SET_ENV_VAL"]; found = 1; next }
        { print }
        END { if (!found) print k "=" ENVIRON["SET_ENV_VAL"] }
    ' "$_f" > "${_f}.tmp" && mv "${_f}.tmp" "$_f"
}

# ---------------------------------------------------------------------
# gen_password — 20 karakter alfanumerik (~119 bit entropi).
#
# Sengaja tanpa simbol: menghindari masalah escaping di .env dan di shell,
# sekaligus memudahkan salin-tempel ke dialog login browser.
# ---------------------------------------------------------------------
gen_password() {
    if [ -r /dev/urandom ]; then
        LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom 2>/dev/null | head -c 20
    elif command -v openssl >/dev/null 2>&1; then
        openssl rand -base64 24 2>/dev/null | LC_ALL=C tr -dc 'A-Za-z0-9' | head -c 20
    else
        red "PERINGATAN: /dev/urandom & openssl tidak ada — password memakai sumber acak lemah."
        printf '%s' "$(date +%s%N)$$" | md5sum 2>/dev/null | head -c 20
    fi
}

# ---------------------------------------------------------------------
# resolve_basic_auth — tentukan kredensial login dashboard.
#
# Menetapkan variabel: AUTH_USER, AUTH_PASS, AUTH_GENERATED, AUTH_DISABLED
#
# Urutan keputusan:
#   1. GOWA_BASIC_AUTH=none|off|disabled  -> sengaja tanpa login
#   2. GOWA_BASIC_AUTH=user:password      -> dipakai (divalidasi ketat)
#   3. Ada terminal (/dev/tty)            -> ditanyakan interaktif
#   4. Tidak keduanya                     -> password kuat dibuat otomatis
#
# Default-nya sengaja BUKAN "terbuka": dashboard ini bisa mengirim WhatsApp
# atas nama pemiliknya, jadi membiarkannya tanpa login saat terekspos
# internet berisiko tinggi.
# ---------------------------------------------------------------------
resolve_basic_auth() {
    AUTH_USER=""
    AUTH_PASS=""
    AUTH_GENERATED=0
    AUTH_DISABLED=0

    if [ -n "${GOWA_BASIC_AUTH:-}" ]; then
        case "$GOWA_BASIC_AUTH" in
            none|off|NONE|OFF|disabled|DISABLED)
                AUTH_DISABLED=1
                return 0
                ;;
            *:*)
                AUTH_USER="${GOWA_BASIC_AUTH%%:*}"
                AUTH_PASS="${GOWA_BASIC_AUTH#*:}"
                [ -n "$AUTH_USER" ] || fail "GOWA_BASIC_AUTH: username kosong. Format: user:password"
                [ -n "$AUTH_PASS" ] || fail "GOWA_BASIC_AUTH: password kosong. Format: user:password"
                info "Kredensial diambil dari GOWA_BASIC_AUTH (user: ${AUTH_USER})."
                return 0
                ;;
            *)
                # WAJIB ditolak keras. Nilai tanpa ':' akan diterima diam-diam
                # oleh dashboard TAPI middleware auth-nya tidak dipasang
                # (main.go: `if len(parts) == 2`), sehingga dashboard terbuka
                # sementara pemiliknya yakin sudah terproteksi.
                fail "GOWA_BASIC_AUTH tidak valid: '${GOWA_BASIC_AUTH}'
Tidak ada tanda ':' — formatnya harus  user:password
Contoh:  GOWA_BASIC_AUTH='admin:RahasiaKuat123'
Untuk sengaja tanpa login:  GOWA_BASIC_AUTH=none"
                ;;
        esac
    fi

    if [ -r /dev/tty ] && [ -w /dev/tty ]; then
        # Dibaca dari /dev/tty, BUKAN stdin. Saat dijalankan lewat
        # `curl ... | sh`, stdin adalah isi skrip itu sendiri — `read` biasa
        # akan menelan baris skrip berikutnya sebagai "jawaban" user.
        printf "\n" > /dev/tty
        printf "Atur login dashboard (Basic Auth).\n" > /dev/tty
        printf "Kosongkan password untuk dibuat otomatis.\n\n" > /dev/tty

        printf "  Username [admin]: " > /dev/tty
        read -r AUTH_USER < /dev/tty || AUTH_USER=""
        [ -n "$AUTH_USER" ] || AUTH_USER="admin"

        # Username tidak boleh memuat ':' — core memisah pada ':' pertama,
        # jadi sisanya akan dianggap bagian password (membingungkan).
        case "$AUTH_USER" in
            *:*) fail "username tidak boleh memuat ':' — hanya password yang boleh." ;;
        esac

        stty -echo 2>/dev/null < /dev/tty || true
        printf "  Password (kosong = otomatis): " > /dev/tty
        read -r AUTH_PASS < /dev/tty || AUTH_PASS=""
        printf "\n" > /dev/tty
        AUTH_PASS2=""
        if [ -n "$AUTH_PASS" ]; then
            printf "  Ulangi password: " > /dev/tty
            read -r AUTH_PASS2 < /dev/tty || AUTH_PASS2=""
            printf "\n" > /dev/tty
        fi
        stty echo 2>/dev/null < /dev/tty || true

        if [ -n "$AUTH_PASS" ] && [ "$AUTH_PASS" != "$AUTH_PASS2" ]; then
            fail "password tidak sama. Jalankan ulang installer."
        fi
        if [ -z "$AUTH_PASS" ]; then
            AUTH_PASS="$(gen_password)"
            AUTH_GENERATED=1
        fi
        return 0
    fi

    # Non-interaktif tanpa GOWA_BASIC_AUTH (mis. curl | sh dari cron):
    # buat otomatis, jangan pernah tinggalkan terbuka.
    AUTH_USER="admin"
    AUTH_PASS="$(gen_password)"
    AUTH_GENERATED=1
}

# ---------------------------------------------------------------------
# apply_basic_auth ENVFILE — tulis hasil resolve_basic_auth ke .env.
# ---------------------------------------------------------------------
apply_basic_auth() {
    _envfile="$1"
    if [ "${AUTH_DISABLED:-0}" -eq 1 ]; then
        set_env DASHBOARD_BASIC_AUTH "" "$_envfile"
        red "PERINGATAN: login dashboard DIMATIKAN (GOWA_BASIC_AUTH=none)."
        red "Siapa pun yang bisa membuka URL ini dapat mengirim WhatsApp dari device Anda."
        return 0
    fi
    [ -n "${AUTH_PASS:-}" ] || fail "gagal menyiapkan password."
    set_env DASHBOARD_BASIC_AUTH "${AUTH_USER}:${AUTH_PASS}" "$_envfile"
    green "OK: login dashboard aktif (user: ${AUTH_USER})."
}

# ---------------------------------------------------------------------
# print_auth_summary ENVPATH RESTART_HINT — tampilkan kredensial di akhir.
#
# Hanya menampilkan password yang DIBUAT pada instalasi ini. Password pilihan
# user sendiri atau milik instalasi lama tidak pernah dicetak, supaya tidak
# bocor ke log/rekaman terminal tanpa alasan.
# ---------------------------------------------------------------------
print_auth_summary() {
    _envpath="$1"
    _restart="$2"
    if [ "${AUTH_GENERATED:-0}" -eq 1 ]; then
        yellow "=============================================="
        yellow " LOGIN DASHBOARD — CATAT SEKARANG"
        yellow "=============================================="
        echo "  Username : ${AUTH_USER}"
        echo "  Password : ${AUTH_PASS}"
        echo ""
        echo "  Password ini dibuat otomatis dan hanya ditampilkan sekali di sini."
        echo "  Tersimpan juga di: ${_envpath}  (baris DASHBOARD_BASIC_AUTH)"
        echo "  Ganti kapan saja: ubah baris itu lalu '${_restart}'"
        echo ""
    elif [ "${AUTH_DISABLED:-0}" -eq 1 ]; then
        red "  Login dashboard: TIDAK AKTIF (dashboard terbuka untuk siapa saja)"
        echo "  Aktifkan: isi DASHBOARD_BASIC_AUTH=user:password di ${_envpath}"
        echo "            lalu '${_restart}'"
        echo ""
    elif [ -n "${AUTH_USER:-}" ]; then
        echo "  Login dashboard: user '${AUTH_USER}' (password sesuai yang Anda isi)"
        echo ""
    fi
}
