#!/bin/sh
# Verifikasi setup gowa-dashboard di aaPanel.
#
# Cek yang dilakukan (urut, berhenti di kegagalan pertama):
#   1. Kedua container (gowa-core + gowa-dashboard) Up
#   2. Dashboard merespons di 127.0.0.1:18088 (langsung, bypass nginx)
#      - GET  /api/_health  -> 200 JSON
#      - POST /api/devices  -> 200 (atau 502 ke core, JANGAN 404)
#   3. Public URL merespons sama dgn pengiriman langsung
#      - kalau berbeda (mis. POST 404 padahal langsung 200) -> nginx
#        mestinya pakai proxy_pass tanpa trailing slash. Lihat
#        docs/aapanel-nginx.conf.example.
#
# Pakai:
#   sh scripts/aapanel-check.sh                       # cek lokal saja
#   sh scripts/aapanel-check.sh https://DOMAIN-ANDA   # + cek via public URL
#   AUTH=user:pass sh scripts/aapanel-check.sh ...    # kalau dashboard basic auth aktif

set -e

COMPOSE_FILE="docker-compose.aapanel.yml"
LOCAL_URL="http://127.0.0.1:18088"
PUBLIC_URL="${1:-}"
AUTH_FLAG=""
if [ -n "${AUTH:-}" ]; then
    AUTH_FLAG="-u ${AUTH}"
fi

red()    { printf "\033[31m%s\033[0m\n" "$*" >&2; }
green()  { printf "\033[32m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }
section(){ printf "\n=== %s ===\n" "$*"; }

fail() { red "FAIL: $*"; exit 1; }
ok()   { green "OK:   $*"; }

# --- 1. Container Up? -----------------------------------------------------
section "1. Container status"
if ! command -v docker >/dev/null 2>&1; then
    fail "docker tidak ditemukan di PATH."
fi
if [ ! -f "$COMPOSE_FILE" ]; then
    fail "$COMPOSE_FILE tidak ada di working dir. Jalankan dari folder repo."
fi

ps_out=$(docker compose -f "$COMPOSE_FILE" ps --format "{{.Name}} {{.State}}" 2>/dev/null || true)
echo "$ps_out"
echo "$ps_out" | grep -q "gowa-core .*running"      || fail "gowa-core belum running. Jalankan: docker compose -f $COMPOSE_FILE up -d"
echo "$ps_out" | grep -q "gowa-dashboard .*running" || fail "gowa-dashboard belum running. Jalankan: docker compose -f $COMPOSE_FILE up -d"
ok "kedua container running"

# --- 2. Test langsung ke dashboard ---------------------------------------
section "2. Test langsung ke dashboard (bypass nginx) - ${LOCAL_URL}"

# 2a. GET /api/_health
status=$(curl -s -o /tmp/aapanel-check.body -w "%{http_code}" $AUTH_FLAG "${LOCAL_URL}/api/_health")
case "$status" in
    200)
        if grep -q '"ok":true' /tmp/aapanel-check.body 2>/dev/null; then
            ok "GET  ${LOCAL_URL}/api/_health -> 200 (dashboard sehat)"
        else
            fail "200 tapi body bukan JSON dashboard. Body: $(head -c 200 /tmp/aapanel-check.body)"
        fi
        ;;
    401)
        fail "401 unauthorized. Dashboard basic auth aktif. Set AUTH=user:pass lalu re-run."
        ;;
    000)
        fail "Tidak bisa connect ke ${LOCAL_URL}. Container dashboard mati / port tidak terbind?"
        ;;
    *)
        fail "GET ${LOCAL_URL}/api/_health -> HTTP $status (expected 200). Body: $(head -c 200 /tmp/aapanel-check.body)"
        ;;
esac

# 2b. POST /api/devices - test critical route yang sebelumnya bermasalah
TEST_NAME="aapanel-check-$(date +%s)"
status=$(curl -s -o /tmp/aapanel-check.body -w "%{http_code}" $AUTH_FLAG \
    -X POST "${LOCAL_URL}/api/devices" \
    -H "Content-Type: application/json" \
    -d "{\"device_id\":\"${TEST_NAME}\"}")
case "$status" in
    200|201)
        ok "POST ${LOCAL_URL}/api/devices -> $status (route handler ke-match)"
        # cleanup test device
        curl -s -o /dev/null $AUTH_FLAG -X DELETE "${LOCAL_URL}/api/devices/${TEST_NAME}" || true
        ;;
    502)
        yellow "POST ${LOCAL_URL}/api/devices -> 502 (dashboard OK, tapi core nolak)."
        yellow "  Cek log core: docker compose -f $COMPOSE_FILE logs --tail=50 whatsapp_go"
        ;;
    404)
        fail "POST 404 'Cannot POST' meskipun langsung ke dashboard - image lama. Rebuild: docker compose -f $COMPOSE_FILE build --no-cache dashboard"
        ;;
    *)
        fail "POST ${LOCAL_URL}/api/devices -> HTTP $status. Body: $(head -c 200 /tmp/aapanel-check.body)"
        ;;
esac

# --- 3. Test via public URL ----------------------------------------------
if [ -z "$PUBLIC_URL" ]; then
    section "3. Public URL test - DI-SKIP (tidak ada arg domain)"
    yellow "Re-run dengan arg domain utk verify nginx reverse proxy:"
    yellow "  sh scripts/aapanel-check.sh https://DOMAIN-ANDA"
    exit 0
fi

PUBLIC_URL="${PUBLIC_URL%/}"
section "3. Test via public URL - ${PUBLIC_URL}"

# 3a. GET /api/_health public
status=$(curl -s -o /tmp/aapanel-check.body -w "%{http_code}" $AUTH_FLAG -k "${PUBLIC_URL}/api/_health")
case "$status" in
    200)
        ok "GET  ${PUBLIC_URL}/api/_health -> 200"
        ;;
    401)
        fail "401. Set AUTH=user:pass lalu re-run."
        ;;
    000)
        fail "Tidak bisa connect ke ${PUBLIC_URL}. Domain belum mengarah ke server? DNS / firewall?"
        ;;
    404)
        red   "GET 404 - nginx reverse proxy belum aktif untuk domain ini."
        red   "  Buka aaPanel -> Website -> ${PUBLIC_URL} -> Reverse proxy -> Add reverse proxy"
        red   "  Target URL: http://127.0.0.1:18088  (TANPA / di akhir)"
        red   "  Atau replace blok location pakai docs/aapanel-nginx.conf.example"
        exit 1
        ;;
    *)
        fail "GET ${PUBLIC_URL}/api/_health -> HTTP $status. Body: $(head -c 200 /tmp/aapanel-check.body)"
        ;;
esac

# 3b. POST /api/devices public - SMOKE TEST untuk trailing-slash bug
TEST_NAME="aapanel-check-pub-$(date +%s)"
status=$(curl -s -o /tmp/aapanel-check.body -w "%{http_code}" $AUTH_FLAG -k \
    -X POST "${PUBLIC_URL}/api/devices" \
    -H "Content-Type: application/json" \
    -d "{\"device_id\":\"${TEST_NAME}\"}")
case "$status" in
    200|201)
        ok "POST ${PUBLIC_URL}/api/devices -> $status (reverse proxy lulus full test)"
        curl -s -o /dev/null $AUTH_FLAG -k -X DELETE "${PUBLIC_URL}/api/devices/${TEST_NAME}" || true
        ;;
    502)
        yellow "POST public -> 502 (proxy OK, core nolak). Cek log core."
        ;;
    404)
        body=$(head -c 200 /tmp/aapanel-check.body)
        red ""
        red "==============================================================="
        red " DETEKSI: nginx reverse proxy aaPanel salah konfigurasi"
        red "==============================================================="
        red ""
        red "POST langsung ke dashboard sukses, tapi via nginx return 404."
        red "Path 'POST /api/devices' ke-strip jadi kosong di upstream."
        red "Body response: $body"
        red ""
        red "Dua bug yang biasanya jadi penyebab di aaPanel UI:"
        red ""
        red "  1) proxy_pass http://127.0.0.1:18088/;"
        red "     ^ trailing slash bikin nginx me-rewrite URI"
        red ""
        red "  2) proxy_set_header Host http://127.0.0.1:18088;"
        red "     ^ Host header di-set ke upstream URL, bukan \$host"
        red ""
        red "FIX (paling cepat, satu perintah, perbaiki dua-duanya):"
        red ""
        red "    sudo sh scripts/aapanel-install-nginx.sh ${PUBLIC_URL#https://}"
        red ""
        red "Atau manual: edit /www/server/panel/vhost/nginx/<domain>.conf,"
        red "pastikan blok proxy match dengan docs/aapanel-nginx.conf.example."
        red ""
        red "Setelah fix, re-run script ini untuk verifikasi."
        exit 1
        ;;
    *)
        fail "POST ${PUBLIC_URL}/api/devices -> HTTP $status. Body: $(head -c 200 /tmp/aapanel-check.body)"
        ;;
esac

# 3c. Cek AI Reply feature (info-only — bukan fail) ----------------------
section "4. AI Reply feature (info-only)"
status=$(curl -s -o /tmp/aapanel-check.body -w "%{http_code}" $AUTH_FLAG -k \
    -H "X-Device-Id: __aapanel_check__" \
    "${PUBLIC_URL}/api/aireply/config")
case "$status" in
    200|400|401)
        ok "AI Reply endpoint /aireply/* terdaftar di core (HTTP $status)"
        ;;
    503)
        if grep -q AI_REPLY_DISABLED /tmp/aapanel-check.body 2>/dev/null; then
            yellow "AI Reply DISABLED di core. Default-nya entrypoint set AI_REPLY_ENABLED=true,"
            yellow "  tapi ${PUBLIC_URL} masih nge-return AI_REPLY_DISABLED -> kemungkinan:"
            yellow "  - core image lama tanpa entrypoint baru. Rebuild:"
            yellow "    docker compose -f $COMPOSE_FILE up -d --build whatsapp_go"
            yellow "  - atau src/.env eksplisit set AI_REPLY_ENABLED=false. Hapus baris itu."
        else
            yellow "AI Reply unreachable. Periksa log core: docker compose -f $COMPOSE_FILE logs --tail=50 whatsapp_go"
        fi
        ;;
    502)
        yellow "AI Reply 502 - core ada masalah lain. Periksa log core."
        ;;
    *)
        yellow "AI Reply check HTTP $status (info-only, abaikan kalau Anda memang nonaktifkan fitur ini)"
        ;;
esac

section "DONE"
green "Semua test lulus. Dashboard siap dipakai dari ${PUBLIC_URL}"
