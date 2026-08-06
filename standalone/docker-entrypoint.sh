#!/bin/sh
set -e

# Bind mount dari host datang milik root, sedangkan dashboard jalan sebagai
# dashuser (uid 20001) dan butuh tulis /data untuk SQLite (DB + WAL + SHM).
# Perbaiki ownership dulu (butuh root), lalu turun ke dashuser.

for d in /data; do
    [ -d "$d" ] || mkdir -p "$d"
    chown -R dashuser:dash "$d" 2>/dev/null || true
done

# Pilih binary sesuai arsitektur container saat RUNTIME, bukan saat build.
# Alasannya: ARG TARGETARCH hanya terisi otomatis oleh BuildKit/buildx.
# Dengan classic builder (Docker Manager aaPanel, `docker compose build`
# tanpa BuildKit) nilainya kosong; kalau binary dipilih saat build, image
# di server ARM akan berisi binary amd64 dan container langsung mati
# "exec format error". Deteksi runtime selalu benar apa pun builder-nya.
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)  BIN=/app/bin/whatsapp-dashboard-linux-amd64 ;;
    aarch64|arm64) BIN=/app/bin/whatsapp-dashboard-linux-arm64 ;;
    *)
        echo "ERROR: arsitektur container '$ARCH' tidak didukung." >&2
        echo "Tersedia: x86_64 (amd64) dan aarch64 (arm64)." >&2
        exit 1
        ;;
esac

if [ ! -x "$BIN" ]; then
    echo "ERROR: binary $BIN tidak ada / tidak executable di dalam image." >&2
    echo "Image kemungkinan ter-build tidak lengkap — rebuild dengan:" >&2
    echo "  docker compose build --no-cache" >&2
    exit 1
fi

exec su-exec dashuser "$BIN" "$@"
