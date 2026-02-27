FROM golang:1.19-alpine AS builder

LABEL maintainer="Yottaflux Developers"

RUN apk add --no-cache gcc musl-dev linux-headers git

# Cache Go module downloads
COPY go.mod /go-ethereum/
COPY go.sum /go-ethereum/
RUN cd /go-ethereum && go mod download

# Build yottaflux statically
COPY . /go-ethereum
RUN cd /go-ethereum && go run build/ci.go install -static ./cmd/yottaflux

# --- Final image ---
FROM alpine:latest

RUN apk add --no-cache ca-certificates curl

# Service account
RUN adduser -D -h /home/yottaflux yottaflux && \
    mkdir -p /var/lib/yottaflux /etc/yottaflux && \
    chown -R yottaflux:yottaflux /var/lib/yottaflux

# Copy binary and genesis
COPY --from=builder /go-ethereum/build/bin/yottaflux /usr/local/bin/yottaflux
COPY genesis_yottaflux.json /etc/yottaflux/genesis.json
COPY genesis_yottaflux_testnet.json /etc/yottaflux/genesis_testnet.json

# Copy and install entrypoint
COPY contrib/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Chain data volume
VOLUME /var/lib/yottaflux

# P2P (TCP + UDP discovery)
EXPOSE 30403/tcp
EXPOSE 30403/udp
# HTTP-RPC
EXPOSE 8645/tcp
# WebSocket
EXPOSE 8646/tcp

# Configurable via environment variables (see docker-entrypoint.sh)
ENV NETWORK=mainnet
ENV NETWORK_ID=7847
ENV P2P_PORT=30403
ENV MAX_PEERS=100
ENV NAT=any
ENV SYNC_MODE=full
ENV GC_MODE=archive
ENV ENABLE_RPC=false
ENV HTTP_PORT=8645
ENV ENABLE_WS=true
ENV WS_PORT=8646
ENV NODISCOVER=false

USER yottaflux
WORKDIR /home/yottaflux

# Healthcheck: IPC socket means the node is alive.
# When HTTP RPC is enabled, fall back to an RPC probe.
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=3 \
    CMD test -S /var/lib/yottaflux/yottaflux.ipc || \
        curl -sf -X POST http://127.0.0.1:${HTTP_PORT:-8645} \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}' \
            > /dev/null 2>&1

ENTRYPOINT ["docker-entrypoint.sh"]
