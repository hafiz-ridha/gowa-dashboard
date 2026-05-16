# Build context expected: project root (so we can reach ./dashboard).
# Builds the WhatsApp Dashboard companion app as a small static Linux binary.

############################
# Step 1: build executable
############################
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build

COPY dashboard/go.mod ./
# go.sum is optional on first build -- the wildcard prevents failure if absent.
COPY dashboard/go.sum* ./
RUN go mod download || true

COPY dashboard/ .

# Pure-Go SQLite (modernc.org/sqlite) -> no CGO needed -> tiny static binary.
RUN CGO_ENABLED=0 go mod tidy && \
    CGO_ENABLED=0 go build -ldflags="-w -s" -o /out/dashboard

############################
# Step 2: minimal runtime
############################
FROM alpine:3.23
# su-exec lets root drop to an unprivileged user AFTER fixing /data ownership
# (bind mounts from the host arrive root-owned).
RUN apk add --no-cache ca-certificates tzdata su-exec

ARG APP_UID=20001
ARG APP_GID=20000
RUN addgroup -g "${APP_GID}" dash && \
    adduser -D -u "${APP_UID}" -G dash -h /app dashuser

WORKDIR /app
COPY --from=builder /out/dashboard /app/dashboard
COPY dashboard/entrypoint.sh /entrypoint.sh
RUN sed -i 's/\r$//' /entrypoint.sh && \
    chmod +x /entrypoint.sh && \
    mkdir -p /data && \
    chown -R dashuser:dash /app /data

# Sensible defaults for in-container use; override in compose / env.
ENV DASHBOARD_HOST=0.0.0.0 \
    DASHBOARD_PORT=8088 \
    DASHBOARD_DB=/data/dashboard.db \
    DASHBOARD_TZ=Asia/Jakarta \
    WHATSAPP_API_URL=http://whatsapp_go:3000 \
    TZ=Asia/Jakarta

# Start as root so entrypoint can chown /data; entrypoint then drops to dashuser.
USER root
EXPOSE 8088
ENTRYPOINT ["/entrypoint.sh"]
