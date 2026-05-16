#!/bin/sh
set -e

# Bind mounts dari host biasanya root-owned. Dashboard jalan sebagai dashuser
# (uid 20001) dan butuh write access ke /data untuk SQLite (DB + WAL).
# Fix ownership saat startup (butuh container root) lalu drop ke dashuser.

for d in /data; do
    [ -d "$d" ] || mkdir -p "$d"
    chown -R dashuser:dash "$d" 2>/dev/null || true
done

exec su-exec dashuser /app/dashboard "$@"
