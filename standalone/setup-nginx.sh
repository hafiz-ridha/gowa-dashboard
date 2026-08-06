#!/bin/sh
# =====================================================================
# Set reverse proxy nginx aaPanel -> GoWA Dashboard standalone
# =====================================================================
#
# Dipanggil otomatis oleh install.sh, tapi bisa dijalankan sendiri:
#   sudo sh setup-nginx.sh gowa.domainku.com [PORT]
#
# Menangani semua kondisi vhost:
#   (a) sudah pernah di-set skrip ini  -> blok lama dibuang, ditulis ulang
#   (b) ada blok proxy buatan aaPanel  -> DIGANTI dengan versi benar
#   (c) site masih "Pure static"       -> blok proxy DISISIPKAN ke server{}
#
# Kenapa perlu: UI "Add Reverse Proxy" aaPanel menghasilkan dua bug yang
# dua-duanya membuat dashboard 404 / "Cannot POST":
#
#   1. proxy_pass http://127.0.0.1:18088/;   <- trailing slash
#      nginx me-rewrite URI jadi kosong, sehingga POST /api/devices
#      sampai ke Fiber sebagai path kosong -> "Cannot POST ".
#
#   2. proxy_set_header Host http://127.0.0.1:18088;
#      Host diisi URL upstream, padahal harus hostname klien ($host).
#      fasthttp (engine Fiber) salah parse Host malformed -> routing miss.
#
# Config yang ditulis skrip ini benar untuk dua-duanya, TIDAK memakai
# variabel $connection_upgrade (dashboard tidak pakai WebSocket, dan
# variabel itu belum tentu terdefinisi -> `nginx -t` bisa gagal), dan
# mengecualikan path ACME supaya perpanjangan SSL tetap jalan.

set -e

DOMAIN="${1:-}"
PORT="${2:-18088}"
NGINX_VHOST_DIR="/www/server/panel/vhost/nginx"
CONF="${NGINX_VHOST_DIR}/${DOMAIN}.conf"

BEGIN_MARK="# >>> gowa-dashboard managed block BEGIN (setup-nginx.sh) <<<"
END_MARK="# >>> gowa-dashboard managed block END <<<"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }

fail() { red "GAGAL: $*"; exit 1; }

[ -n "$DOMAIN" ] || fail "pakai: sudo sh setup-nginx.sh DOMAIN [PORT]"
[ "$(id -u)" -eq 0 ] || fail "harus root (pakai sudo)."
[ -d "$NGINX_VHOST_DIR" ] || fail "$NGINX_VHOST_DIR tidak ada — aaPanel belum terpasang?"
[ -f "$CONF" ] || fail "site '$DOMAIN' belum ada.
Buat dulu di aaPanel -> Website -> Add site -> $DOMAIN (PHP: Pure static)."
command -v nginx >/dev/null 2>&1 || fail "binary nginx tidak ada di PATH."

# ---------- backup ----------
BACKUP="${CONF}.bak.$(date +%Y%m%d-%H%M%S)"
cp "$CONF" "$BACKUP"
info "Backup: $BACKUP"

WORK="${CONF}.work.$$"
BLOCK="${CONF}.block.$$"
cleanup() { rm -f "$WORK" "$BLOCK" "${WORK}.2"; }
trap cleanup EXIT INT TERM

# ---------- blok yang akan ditulis (satu sumber kebenaran) ----------
cat > "$BLOCK" <<NGINXBLOCK
    ${BEGIN_MARK}
    # CATATAN: 'location ^~ /' membuat nginx BERHENTI mengevaluasi seluruh
    # location regex (termasuk blok .well-known bawaan aaPanel). Tanpa
    # pengecualian di bawah, validasi ACME ikut ter-proxy ke dashboard dan
    # perpanjangan otomatis sertifikat Let's Encrypt GAGAL — gejalanya baru
    # terasa 60-90 hari kemudian saat sertifikat kedaluwarsa. Prefix ini
    # lebih panjang daripada '/', jadi ia menang dan challenge dilayani
    # langsung dari disk.
    location ^~ /.well-known/acme-challenge/
    {
        allow all;
        try_files \$uri =404;
    }

    location ^~ /
    {
        # TANPA trailing slash — dengan slash, nginx me-rewrite URI sehingga
        # path "/api/devices" hilang dan Fiber balas "Cannot POST ".
        proxy_pass http://127.0.0.1:${PORT};

        proxy_http_version 1.1;
        # Host WAJIB \$host (hostname klien), bukan URL upstream.
        proxy_set_header Host              \$host;
        proxy_set_header X-Real-IP         \$remote_addr;
        proxy_set_header X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # Upload dokumen knowledgebase AI Reply (default core 10MB).
        client_max_body_size 50m;

        # Login QR menunggu pairing — jangan diputus cepat.
        proxy_read_timeout    300s;
        proxy_send_timeout    300s;
        proxy_connect_timeout 30s;

        # QR PNG & response streaming langsung diteruskan.
        proxy_buffering off;
    }
    ${END_MARK}
NGINXBLOCK

# ---------- Tahap 1: buang blok managed lama (idempoten) ----------
# Tanpa ini, install ulang akan menduplikasi location dan nginx menolak
# dengan "duplicate location".
awk -v b="$BEGIN_MARK" -v e="$END_MARK" '
    index($0, b) { drop = 1; next }
    index($0, e) { drop = 0; next }
    !drop        { print }
' "$CONF" > "$WORK"

[ -s "$WORK" ] || fail "hasil pembersihan blok lama kosong — dibatalkan, config asli aman."

# ---------- Tahap 2: ganti blok proxy buatan aaPanel kalau ada ----------
awk -v blockfile="$BLOCK" '
BEGIN { skip = 0; depth = 0; replaced = 0; buffer = "" }
{
    if (skip == 0) {
        # Kandidat: baris "location /" atau "location ^~ /" yang berdiri
        # sendiri (bukan prefix lain seperti /.well-known/...).
        if ($0 ~ /^[[:space:]]*location[[:space:]]+(\^~[[:space:]]+)?\/[[:space:]]*\{?[[:space:]]*$/) {
            skip = 1
            depth = ($0 ~ /\{/) ? 1 : 0
            buffer = $0 "\n"
            next
        }
        print
    } else {
        buffer = buffer $0 "\n"
        line = $0
        n_open  = gsub(/\{/, "{", line)
        n_close = gsub(/\}/, "}", line)
        depth = depth + n_open - n_close
        if (depth <= 0) {
            # Hanya blok yang benar-benar mem-proxy yang kita ganti; blok
            # "location /" lain (mis. try_files statis) dibiarkan utuh.
            if (index(buffer, "proxy_pass") > 0) {
                while ((getline l < blockfile) > 0) print l
                close(blockfile)
                replaced = 1
            } else {
                printf "%s", buffer
            }
            skip = 0
            buffer = ""
        }
    }
}
END {
    if (skip == 1 && buffer != "") printf "%s", buffer
    exit (replaced ? 0 : 9)
}
' "$WORK" > "${WORK}.2" && REPLACED=1 || REPLACED=0

if [ -s "${WORK}.2" ]; then
    mv "${WORK}.2" "$WORK"
else
    fail "hasil rewrite kosong — dibatalkan, config asli aman."
fi

# ---------- Tahap 3: sisipkan kalau belum ada blok proxy ----------
# Kasus site "Pure static" yang baru dibuat. Installer aaPanel bawaan tidak
# menangani ini: tidak ada yang diganti = tidak ada yang ditulis = tetap 404.
if [ "$REPLACED" -eq 0 ]; then
    info "Belum ada blok proxy — menyisipkan blok baru ke dalam server{}."
    awk -v blockfile="$BLOCK" '
    BEGIN { in_server = 0; depth = 0; done = 0 }
    {
        if (!done && !in_server && $0 ~ /^[[:space:]]*server[[:space:]]*\{?[[:space:]]*$/) {
            in_server = 1
            depth = ($0 ~ /\{/) ? 1 : 0
            print
            next
        }
        if (in_server && !done) {
            line = $0
            n_open  = gsub(/\{/, "{", line)
            n_close = gsub(/\}/, "}", line)
            new_depth = depth + n_open - n_close
            # Kurung tutup yang mengembalikan depth ke 0 = penutup server{}.
            if (new_depth <= 0 && depth > 0) {
                while ((getline l < blockfile) > 0) print l
                close(blockfile)
                print
                done = 1
                in_server = 0
                next
            }
            depth = new_depth
            print
            next
        }
        print
    }
    END { exit (done ? 0 : 9) }
    ' "$WORK" > "${WORK}.2" || fail "tidak menemukan blok server{} di $CONF.
Set manual pakai isi nginx-aapanel.conf.example."

    if [ -s "${WORK}.2" ]; then
        mv "${WORK}.2" "$WORK"
    else
        fail "hasil penyisipan kosong — dibatalkan, config asli aman."
    fi
fi

# ---------- Sanity check sebelum menimpa ----------
grep -q "proxy_pass http://127.0.0.1:${PORT};" "$WORK" \
    || fail "blok proxy tidak terpasang di hasil — dibatalkan, config asli aman."

# Jumlah location harus tepat: 1 ACME + 1 root, tidak boleh ganda.
N_ROOT="$(grep -c '^[[:space:]]*location \^~ /$' "$WORK" || true)"
N_ACME="$(grep -c 'location \^~ /\.well-known/acme-challenge/' "$WORK" || true)"
[ "$N_ROOT" = "1" ] || fail "jumlah 'location ^~ /' = ${N_ROOT} (harus 1) — dibatalkan."
[ "$N_ACME" = "1" ] || fail "jumlah location ACME = ${N_ACME} (harus 1) — dibatalkan."

cp "$WORK" "$CONF"
info "Config ditulis: $CONF"

# ---------- validasi + reload (rollback kalau gagal) ----------
info "Validasi syntax nginx..."
if ! nginx -t >/dev/null 2>&1; then
    red "Syntax nginx TIDAK valid. Mengembalikan backup..."
    cp "$BACKUP" "$CONF"
    nginx -t 2>&1 | head -10 >&2
    fail "config sudah dikembalikan ke kondisi semula (tidak ada perubahan permanen)."
fi

nginx -s reload 2>/dev/null || systemctl reload nginx 2>/dev/null || fail "reload nginx gagal."
green "OK: nginx untuk $DOMAIN sudah diarahkan ke 127.0.0.1:${PORT}"

# ---------- smoke test lewat URL publik ----------
# POST sengaja diuji: dua bug aaPanel di atas hanya terlihat pada POST.
# GET sering tetap jalan, sehingga bug-nya lolos kalau cuma buka browser.
if command -v curl >/dev/null 2>&1; then
    info "Uji cepat lewat https://${DOMAIN} ..."
    G="$(curl -sk -m 15 -o /dev/null -w '%{http_code}' "https://${DOMAIN}/api/_health" 2>/dev/null || echo 000)"
    P="$(curl -sk -m 15 -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
         -d '{}' "https://${DOMAIN}/api/_cleanup" 2>/dev/null || echo 000)"
    echo "    GET  /api/_health  -> HTTP ${G}"
    echo "    POST /api/_cleanup -> HTTP ${P}"
    case "$G" in
        200|401) green "    GET OK." ;;
        000)     red   "    GET tidak konek — cek DNS/SSL domain ini." ;;
        404)     red   "    GET 404 — ada blok location lain yang menang. Cek $CONF." ;;
        *)       printf "    GET balas %s (bukan 404, biasanya aman).\n" "$G" ;;
    esac
    case "$P" in
        # 400 = handler jalan tapi menolak body (retention belum diset) -> routing BENAR.
        200|400|401) green "    POST OK — bug trailing-slash & Host tidak terjadi." ;;
        404)         red   "    POST 404 — routing masih salah. Cek ulang $CONF." ;;
        000)         red   "    POST tidak konek." ;;
        *)           printf "    POST balas %s.\n" "$P" ;;
    esac
fi

echo ""
echo "Backup config lama: $BACKUP"
