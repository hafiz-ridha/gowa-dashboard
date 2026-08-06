#!/bin/sh
set -e

# Bind mount dari host datang milik root, sedangkan dashboard jalan sebagai
# dashuser (uid 20001) dan butuh tulis /data untuk SQLite (DB + WAL + SHM).
# Perbaiki ownership dulu (butuh root), lalu turun ke dashuser.

for d in /data; do
    [ -d "$d" ] || mkdir -p "$d"
    chown -R dashuser:dash "$d" 2>/dev/null || true
done

exec su-exec dashuser /app/dashboard "$@"
