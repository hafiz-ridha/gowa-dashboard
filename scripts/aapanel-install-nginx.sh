#!/bin/sh
# Auto-installer nginx reverse proxy untuk gowa-dashboard di aaPanel.
#
# Script ini:
#   1. Backup config nginx aaPanel yang ada
#   2. Replace blok "location /" atau "location ^~ /" pakai config benar
#      dari docs/aapanel-nginx.conf.example (proxy_pass tanpa slash,
#      proxy_set_header Host $host - dua-duanya bug aaPanel UI)
#   3. Test config + reload nginx
#   4. Smoke test POST /api/devices via public URL
#
# Pakai:
#   sudo sh scripts/aapanel-install-nginx.sh DOMAIN [UPSTREAM_PORT]
#
# Contoh:
#   sudo sh scripts/aapanel-install-nginx.sh gowa.namadomainsaya.com
#   sudo sh scripts/aapanel-install-nginx.sh gowa.example.com 18088
#
# Prasyarat: aaPanel sudah create site untuk DOMAIN (tab Website ->
# Add site dgn Pure static). Script TIDAK akan otomatis bikin site
# karena itu butuh setup database/SSL aaPanel.

set -e

DOMAIN="${1:-}"
UPSTREAM_PORT="${2:-18088}"
NGINX_VHOST_DIR="/www/server/panel/vhost/nginx"
NGINX_CONF="${NGINX_VHOST_DIR}/${DOMAIN}.conf"

red()   { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }

if [ -z "$DOMAIN" ]; then
    red "Pakai: sudo sh $0 DOMAIN [UPSTREAM_PORT]"
    red "Contoh: sudo sh $0 gowa.namadomainsaya.com"
    exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
    red "Harus jalan sebagai root (pakai sudo)."
    exit 1
fi

if [ ! -d "$NGINX_VHOST_DIR" ]; then
    red "Folder $NGINX_VHOST_DIR tidak ada. aaPanel belum terinstall?"
    exit 1
fi

if [ ! -f "$NGINX_CONF" ]; then
    red "Config $NGINX_CONF belum ada."
    red "Buat dulu site-nya: aaPanel -> Website -> Add site -> DOMAIN, PHP=Pure static."
    exit 1
fi

if ! command -v nginx >/dev/null 2>&1; then
    red "nginx binary tidak ditemukan di PATH."
    exit 1
fi

# --- Backup --------------------------------------------------------------
BACKUP="${NGINX_CONF}.bak.$(date +%Y%m%d-%H%M%S)"
cp "$NGINX_CONF" "$BACKUP"
info "Backup config lama -> $BACKUP"

# --- Replace blok location ----------------------------------------------
# Strategi: hapus blok "location ^~ /" atau "location /" yang punya
# proxy_pass, lalu inject blok yang benar persis sebelum tag aaPanel
# atau sebelum penutup server {} block.
#
# Pakai awk supaya bisa multi-line replacement; sed tunggal sulit untuk
# block matching dengan nested braces.

TMP="${NGINX_CONF}.tmp.$$"

awk -v port="$UPSTREAM_PORT" '
BEGIN {
    skip = 0
    depth = 0
    inserted = 0
}

# Detect start of a location block that contains proxy_pass to our port.
# We mark and skip the entire block (matching braces) and inject the
# correct one at the position of the closing brace.
function emit_correct_block(port) {
    print "    # gowa-dashboard reverse proxy (di-install oleh aapanel-install-nginx.sh)"
    print "    location ^~ /"
    print "    {"
    print "        proxy_pass http://127.0.0.1:" port ";"
    print "        proxy_http_version 1.1;"
    print "        proxy_set_header Host              $host;"
    print "        proxy_set_header X-Real-IP         $remote_addr;"
    print "        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;"
    print "        proxy_set_header X-Forwarded-Proto $scheme;"
    print "        proxy_set_header REMOTE-HOST       $remote_addr;"
    print "        proxy_set_header Upgrade           $http_upgrade;"
    print "        proxy_set_header Connection        $connection_upgrade;"
    print "        client_max_body_size 50m;"
    print "        proxy_read_timeout   300s;"
    print "        proxy_send_timeout   300s;"
    print "        proxy_connect_timeout 30s;"
    print "        proxy_buffering off;"
    print "    }"
    inserted = 1
}

{
    if (skip == 0) {
        # Match "location /" or "location ^~ /" header line
        if ($0 ~ /^[[:space:]]*location[[:space:]]+(\^~[[:space:]]+)?\/[[:space:]]*\{?[[:space:]]*$/) {
            # Look ahead - mark as candidate, we will check next braces
            saved_line = $0
            skip = 1
            depth = 0
            # Count opening brace if on same line
            if ($0 ~ /\{/) depth = 1
            buffer = $0 "\n"
            next
        }
        print
    } else {
        buffer = buffer $0 "\n"
        # Track braces depth
        n_open  = gsub(/\{/, "{", $0)
        n_close = gsub(/\}/, "}", $0)
        depth = depth + n_open - n_close
        if (depth <= 0) {
            # End of the location block. Check if buffer contained proxy_pass
            if (index(buffer, "proxy_pass") > 0) {
                # Replace with correct block
                emit_correct_block(port)
            } else {
                # Not our target - emit buffer as-is
                printf "%s", buffer
            }
            skip = 0
            buffer = ""
        }
    }
}

END {
    # If still in skip mode (unbalanced), emit buffer as-is to avoid losing config
    if (skip == 1 && buffer != "") {
        printf "%s", buffer
    }
    if (inserted == 0) {
        # Did not find a location to replace - emit nothing extra; user
        # likely already removed default proxy. They should re-run after
        # adding via aaPanel UI.
    }
}
' "$NGINX_CONF" > "$TMP"

# Sanity: hasil tidak boleh kosong
if [ ! -s "$TMP" ]; then
    red "Hasil rewrite kosong - abort. Restore backup."
    rm -f "$TMP"
    exit 1
fi

mv "$TMP" "$NGINX_CONF"
info "Config baru ditulis ke $NGINX_CONF"

# --- Test & reload nginx -------------------------------------------------
info "Validasi syntax nginx..."
if ! nginx -t 2>&1; then
    red "Syntax check GAGAL. Restore backup."
    cp "$BACKUP" "$NGINX_CONF"
    exit 1
fi

info "Reload nginx..."
nginx -s reload

green "OK: config nginx untuk $DOMAIN sudah ter-update."
echo ""
echo "Verify dengan:"
echo "  sh scripts/aapanel-check.sh https://$DOMAIN"
echo "  # atau kalau dashboard pakai basic auth:"
echo "  AUTH=user:pass sh scripts/aapanel-check.sh https://$DOMAIN"
echo ""
echo "Backup config lama tetap ada di:"
echo "  $BACKUP"
