#!/bin/sh
set -e

DATADIR="/var/lib/yottaflux"
GENESIS="/etc/yottaflux/genesis.json"

# Network mode: "mainnet" (default) or "testnet"
NETWORK="${NETWORK:-mainnet}"

# Initialize the chain on first run (chaindata directory absent).
# When --datadir is explicit (as in Docker), both mainnet and testnet use the
# same directory structure — the "testnet" subdirectory only applies to the
# default data dir (~/.yottaflux).
if [ ! -d "$DATADIR/yottaflux/chaindata" ]; then
    echo "First run detected ($NETWORK) — initializing genesis..."
    if [ "$NETWORK" = "testnet" ]; then
        yottaflux --testnet --datadir "$DATADIR" init "$GENESIS"
    else
        yottaflux --datadir "$DATADIR" init "$GENESIS"
    fi
    echo "Genesis initialized."
fi

# Build seed node flags.
# A seed node's job is to be publicly reachable, accept many peers,
# and relay blocks/transactions. It does NOT mine.
SEED_FLAGS="--datadir=$DATADIR"

# Set network mode
if [ "$NETWORK" = "testnet" ]; then
    SEED_FLAGS="$SEED_FLAGS --testnet"
    # Default network ID for testnet is 7848 (set by --testnet flag)
else
    SEED_FLAGS="$SEED_FLAGS --networkid=${NETWORK_ID:-7847}"
fi

SEED_FLAGS="$SEED_FLAGS --port=${P2P_PORT:-30403}"
SEED_FLAGS="$SEED_FLAGS --maxpeers=${MAX_PEERS:-100}"
SEED_FLAGS="$SEED_FLAGS --nat=${NAT:-any}"
SEED_FLAGS="$SEED_FLAGS --syncmode=${SYNC_MODE:-full}"
SEED_FLAGS="$SEED_FLAGS --gcmode=${GC_MODE:-archive}"

# Enable HTTP RPC if requested (off by default for security)
if [ "${ENABLE_RPC:-false}" = "true" ]; then
    SEED_FLAGS="$SEED_FLAGS --http"
    SEED_FLAGS="$SEED_FLAGS --http.addr=0.0.0.0"
    SEED_FLAGS="$SEED_FLAGS --http.port=${HTTP_PORT:-8645}"
    SEED_FLAGS="$SEED_FLAGS --http.api=${HTTP_API:-eth,net,web3,txpool,debug}"
    SEED_FLAGS="$SEED_FLAGS --http.vhosts=${HTTP_VHOSTS:-*}"
    SEED_FLAGS="$SEED_FLAGS --http.corsdomain=${HTTP_CORS:-*}"
fi

# Enable WebSocket (on by default for Blockscout newHeads subscription)
if [ "${ENABLE_WS:-true}" = "true" ]; then
    SEED_FLAGS="$SEED_FLAGS --ws"
    SEED_FLAGS="$SEED_FLAGS --ws.addr=0.0.0.0"
    SEED_FLAGS="$SEED_FLAGS --ws.port=${WS_PORT:-8646}"
    SEED_FLAGS="$SEED_FLAGS --ws.api=${WS_API:-eth,net,web3}"
    SEED_FLAGS="$SEED_FLAGS --ws.origins=${WS_ORIGINS:-*}"
fi

# Disable peer discovery if requested (not recommended for seed nodes)
if [ "${NODISCOVER:-false}" = "true" ]; then
    SEED_FLAGS="$SEED_FLAGS --nodiscover"
fi

# Add any static/trusted nodes via bootnodes flag
if [ -n "${BOOTNODES:-}" ]; then
    SEED_FLAGS="$SEED_FLAGS --bootnodes=$BOOTNODES"
fi

# Allow passing additional flags via EXTRA_FLAGS env or command-line args
# shellcheck disable=SC2086
exec yottaflux $SEED_FLAGS ${EXTRA_FLAGS:-} "$@"
