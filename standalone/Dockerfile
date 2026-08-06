# Image GoWA Dashboard standalone dari binary yang SUDAH dikompilasi.
#
# Tidak ada tahap build Go di sini — binary statis (CGO_ENABLED=0) sudah
# ikut dalam paket, jadi build image ini cepat (beberapa detik) dan tidak
# butuh koneksi ke proxy modul Go. Satu sumber error hilang.
#
# Build context = folder paket standalone ini.

FROM alpine:3.23

# ca-certificates: dashboard memanggil core lewat HTTPS.
# tzdata: jadwal memakai zona waktu IANA (mis. Asia/Jakarta).
# su-exec: turun hak akses ke user non-root setelah /data di-chown.
RUN apk add --no-cache ca-certificates tzdata su-exec

ARG APP_UID=20001
ARG APP_GID=20000
RUN addgroup -g "${APP_GID}" dash && \
    adduser -D -u "${APP_UID}" -G dash -h /app dashuser

WORKDIR /app

# TARGETARCH diisi otomatis oleh buildkit (amd64 / arm64).
ARG TARGETARCH=amd64
COPY bin/whatsapp-dashboard-linux-${TARGETARCH} /app/dashboard
COPY docker-entrypoint.sh /entrypoint.sh

RUN sed -i 's/\r$//' /entrypoint.sh && \
    chmod +x /entrypoint.sh /app/dashboard && \
    mkdir -p /data && \
    chown -R dashuser:dash /app /data

ENV DASHBOARD_HOST=0.0.0.0 \
    DASHBOARD_PORT=8088 \
    DASHBOARD_DB=/data/dashboard.db \
    DASHBOARD_TZ=Asia/Jakarta \
    TZ=Asia/Jakarta

# Mulai sebagai root supaya entrypoint bisa chown /data (bind mount dari
# host datang milik root), lalu langsung drop ke dashuser.
USER root
EXPOSE 8088
ENTRYPOINT ["/entrypoint.sh"]
