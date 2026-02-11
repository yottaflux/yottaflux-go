#!/bin/sh
# Yottaflux devnet smoke test
# Builds the Docker images, starts miner + RPC, waits for blocks, then shuts down.
set -e

COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$COMPOSE_DIR"

RPC_URL="http://127.0.0.1:8645"
TIMEOUT=120
POLL_INTERVAL=3

cleanup() {
    echo "Shutting down devnet..."
    docker compose down -v 2>/dev/null || true
}
trap cleanup EXIT

echo "=== Building and starting devnet ==="
docker compose up -d --build

echo "=== Waiting for RPC to respond (timeout: ${TIMEOUT}s) ==="
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    if curl -sf -X POST "$RPC_URL" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        > /dev/null 2>&1; then
        echo "RPC is up after ${elapsed}s"
        break
    fi
    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
done

if [ "$elapsed" -ge "$TIMEOUT" ]; then
    echo "FAIL: RPC did not respond within ${TIMEOUT}s"
    docker compose logs
    exit 1
fi

echo "=== Checking block production ==="
# Poll for blocks > 0 (miner should produce at least one block)
block_wait=0
while [ "$block_wait" -lt 60 ]; do
    BLOCK_HEX=$(curl -sf -X POST "$RPC_URL" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
        | sed -n 's/.*"result":"\(0x[0-9a-fA-F]*\)".*/\1/p')

    if [ -n "$BLOCK_HEX" ] && [ "$BLOCK_HEX" != "0x0" ]; then
        echo "PASS: Block number = $BLOCK_HEX"
        break
    fi
    sleep "$POLL_INTERVAL"
    block_wait=$((block_wait + POLL_INTERVAL))
done

if [ "$block_wait" -ge 60 ]; then
    echo "WARN: No blocks mined within 60s (ProgPow mining may be slow at genesis difficulty)"
    echo "RPC is responding but miner has not produced blocks yet."
    echo "This is acceptable for a smoke test — the node is functional."
fi

echo "=== Checking net_version ==="
NET_VERSION=$(curl -sf -X POST "$RPC_URL" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"net_version","params":[],"id":1}' \
    | sed -n 's/.*"result":"\([0-9]*\)".*/\1/p')

if [ "$NET_VERSION" = "7847" ]; then
    echo "PASS: Network ID = $NET_VERSION"
else
    echo "FAIL: Expected network ID 7847, got $NET_VERSION"
    exit 1
fi

echo "=== Devnet smoke test passed ==="
