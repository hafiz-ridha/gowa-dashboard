#!/bin/sh
# Bangun paket standalone GoWA Dashboard (tar.gz siap upload ke server).
#
# Menghasilkan: dist/gowa-dashboard-standalone.tar.gz
#
# Pakai:
#   sh scripts/build-standalone.sh
#
# Butuh: Go 1.23+ (untuk cross-compile). Tidak butuh Docker.
#
# Binary dibangun statis (CGO_ENABLED=0) untuk linux/amd64 dan linux/arm64,
# sehingga jalan di distro Linux apa pun tanpa dependensi libc.

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${ROOT}/dist"
STAGE="${OUT}/standalone"

green() { printf "\033[32m%s\033[0m\n" "$*"; }
info()  { printf "[*] %s\n" "$*"; }

command -v go >/dev/null 2>&1 || { printf "GAGAL: Go tidak ditemukan di PATH.\n" >&2; exit 1; }

info "Go: $(go version)"

rm -rf "$STAGE"
mkdir -p "${STAGE}/bin"

# ---- binary ----
cd "${ROOT}/dashboard"
info "Menyiapkan dependensi (go mod tidy)..."
go mod tidy

for pair in "amd64" "arm64"; do
    info "Build linux/${pair}..."
    CGO_ENABLED=0 GOOS=linux GOARCH="$pair" \
        go build -trimpath -ldflags="-w -s" \
        -o "${STAGE}/bin/whatsapp-dashboard-linux-${pair}" .
done

# Segarkan juga binary yang di-commit di standalone/bin/ + SHA256SUMS,
# karena itulah yang diambil bootstrap.sh dari GitHub. Kalau tidak
# disinkronkan, one-liner install akan memasang binary versi lama.
cd "$ROOT"
mkdir -p standalone/bin
cp "${STAGE}/bin/whatsapp-dashboard-linux-amd64" standalone/bin/
cp "${STAGE}/bin/whatsapp-dashboard-linux-arm64" standalone/bin/
( cd standalone && sha256sum bin/whatsapp-dashboard-linux-amd64 \
                             bin/whatsapp-dashboard-linux-arm64 > SHA256SUMS )
info "standalone/bin/ + SHA256SUMS disegarkan (dipakai bootstrap.sh)"

# ---- aset paket ----
# CR dibuang saat menyalin. .gitattributes sudah memaksa LF, tapi paket ini
# bisa juga dibangun dari working tree yang dicopy/di-zip lewat Windows —
# dan satu CR saja di install.sh membuat Linux menolak dengan
# "/bin/sh^M: bad interpreter". Pengaman lapis kedua, murah.
cd "$ROOT"
for f in install.sh setup-nginx.sh uninstall.sh bootstrap.sh gowa-dashboard.service \
         .env.example nginx-aapanel.conf.example SHA256SUMS \
         Dockerfile docker-entrypoint.sh docker-compose.yml README.md; do
    tr -d '\r' < "standalone/${f}" > "${STAGE}/${f}"
done

chmod +x "${STAGE}"/*.sh

# Verifikasi: tidak boleh ada CR yang lolos ke paket.
if grep -rlU "$(printf '\r')" "$STAGE" 2>/dev/null | grep -v '^.*/bin/' | grep . ; then
    printf "GAGAL: masih ada file ber-CRLF di paket (lihat daftar di atas).\n" >&2
    exit 1
fi

# ---- kemas ----
cd "$OUT"
TARBALL="gowa-dashboard-standalone.tar.gz"
rm -f "$TARBALL"
tar -czf "$TARBALL" standalone

green "OK: ${OUT}/${TARBALL}"
ls -lh "${OUT}/${TARBALL}"
echo ""
echo "Upload tarball itu ke server, lalu:"
echo "  tar -xzf ${TARBALL}"
echo "  cd standalone"
echo "  sudo sh install.sh DOMAIN-ANDA"
