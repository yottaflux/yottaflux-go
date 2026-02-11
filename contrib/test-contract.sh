#!/bin/sh
# Yottaflux devnet contract deployment test
# Deploys a SimpleStorage contract via RPC, calls set(42), verifies get() returns 42.
#
# Requires: Docker, curl, sed
# Usage: cd contrib && ./test-contract.sh
set -e

COMPOSE_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$COMPOSE_DIR/.." && pwd)"

# Test account (same key used in Go unit tests)
TEST_PRIVKEY="b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291"
TEST_ADDRESS="0x71562b71999873DB5b286dF957af199Ec94617F7"
TEST_PASSWORD="testpassword"

# SimpleStorage deploy bytecode (init + runtime, same as contract_test.go)
DEPLOY_BYTECODE="0x6032600c60003960326000f360003560e01c806360fe47b114601e57636d4ce63c1460265760006000fd5b600435600055005b60005460005260206000f3"

# set(42) calldata: selector 0x60fe47b1 + uint256(42)
SET_42_DATA="0x60fe47b1000000000000000000000000000000000000000000000000000000000000002a"

# get() calldata: selector 0x6d4ce63c
GET_DATA="0x6d4ce63c"

CONTAINER_NAME="yottaflux-contract-test"
RPC_PORT=8746
RPC_URL="http://127.0.0.1:${RPC_PORT}"
TIMEOUT=120
POLL_INTERVAL=3
MINE_WAIT=60

# --- Devnet genesis with low difficulty and pre-funded test account ---
GENESIS_TMP=$(mktemp)
cat > "$GENESIS_TMP" <<GENESIS
{
  "config": {
    "chainId": 7847,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "progpow": {}
  },
  "difficulty": "0x1",
  "gasLimit": "0x1C9C380",
  "baseFeePerGas": "0x3B9ACA00",
  "alloc": {
    "${TEST_ADDRESS}": {
      "balance": "1000000000000000000000"
    }
  },
  "nonce": "0x0000000000000042",
  "timestamp": "0x0",
  "extraData": "0x596f747461666c757820436f6e747261637420546573742047656e65736973",
  "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
GENESIS

# --- Helper functions ---

rpc_call() {
    curl -sf -X POST "$RPC_URL" \
        -H "Content-Type: application/json" \
        -d "$1" 2>/dev/null
}

extract_result() {
    sed -n 's/.*"result":"\{0,1\}\([^"]*\)"\{0,1\}.*/\1/p' | head -1
}

cleanup() {
    echo ""
    echo "=== Cleaning up ==="
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
    rm -f "$GENESIS_TMP"
}
trap cleanup EXIT

# --- Build image ---
echo "=== Building Docker image ==="
docker build -t yottaflux-contract-test -f "$COMPOSE_DIR/Dockerfile" "$REPO_ROOT"

# --- Start single node with mining + RPC + personal API ---
echo "=== Starting yottaflux node (mine + RPC) ==="
docker run -d --name "$CONTAINER_NAME" \
    -p "${RPC_PORT}:${RPC_PORT}" \
    -e NETWORK=mainnet \
    -e NETWORK_ID=7847 \
    -e NODISCOVER=true \
    -e SYNC_MODE=full \
    -e ENABLE_RPC=true \
    -e HTTP_PORT="${RPC_PORT}" \
    -e HTTP_API="eth,net,web3,personal,txpool" \
    -e EXTRA_FLAGS="--mine --miner.threads=1 --miner.etherbase=${TEST_ADDRESS} --allow-insecure-unlock" \
    yottaflux-contract-test

# Inject the custom genesis (overwrite the default one used by entrypoint)
# The container's entrypoint initializes on first run, so we need to replace
# the genesis before the chain is initialized. Since the container just started,
# let's wait briefly then reinitialize if needed.
echo "=== Waiting for container to start ==="
sleep 2

# Check if genesis needs custom init (container may have already initialized with default genesis)
# We'll stop, reinitialize with our genesis, and restart.
docker stop "$CONTAINER_NAME" 2>/dev/null || true
docker cp "$GENESIS_TMP" "${CONTAINER_NAME}:/tmp/genesis_test.json"
docker start "$CONTAINER_NAME"
# Wait for it to come back, then re-init with custom genesis
sleep 2
docker exec "$CONTAINER_NAME" sh -c "rm -rf /var/lib/yottaflux/yottaflux/chaindata /var/lib/yottaflux/yottaflux/lightchaindata /var/lib/yottaflux/yottaflux/nodes" 2>/dev/null || true
docker stop "$CONTAINER_NAME" 2>/dev/null || true
docker exec "$CONTAINER_NAME" yottaflux --datadir /var/lib/yottaflux init /tmp/genesis_test.json 2>/dev/null || {
    # Container must be running for exec; start it, init, restart
    docker start "$CONTAINER_NAME"
    sleep 1
    docker exec "$CONTAINER_NAME" yottaflux --datadir /var/lib/yottaflux init /tmp/genesis_test.json
    docker stop "$CONTAINER_NAME"
}
docker start "$CONTAINER_NAME"

# --- Wait for RPC ---
echo "=== Waiting for RPC to respond (timeout: ${TIMEOUT}s) ==="
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    if rpc_call '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' > /dev/null 2>&1; then
        echo "RPC is up after ${elapsed}s"
        break
    fi
    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
done

if [ "$elapsed" -ge "$TIMEOUT" ]; then
    echo "FAIL: RPC did not respond within ${TIMEOUT}s"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -30
    exit 1
fi

# --- Import test account ---
echo "=== Importing test account ==="
IMPORT_RESULT=$(rpc_call "{
    \"jsonrpc\":\"2.0\",
    \"method\":\"personal_importRawKey\",
    \"params\":[\"${TEST_PRIVKEY}\", \"${TEST_PASSWORD}\"],
    \"id\":1
}")
IMPORTED_ADDR=$(echo "$IMPORT_RESULT" | extract_result)

if [ -z "$IMPORTED_ADDR" ]; then
    echo "WARN: Could not import key (may already exist)"
    IMPORTED_ADDR="$TEST_ADDRESS"
else
    echo "Imported account: $IMPORTED_ADDR"
fi

# --- Verify account has balance ---
echo "=== Checking test account balance ==="
BALANCE_RESULT=$(rpc_call "{
    \"jsonrpc\":\"2.0\",
    \"method\":\"eth_getBalance\",
    \"params\":[\"${TEST_ADDRESS}\", \"latest\"],
    \"id\":1
}")
BALANCE=$(echo "$BALANCE_RESULT" | extract_result)
echo "Account balance: $BALANCE"

if [ "$BALANCE" = "0x0" ] || [ -z "$BALANCE" ]; then
    echo "FAIL: Test account has zero balance. Genesis may not have been applied correctly."
    docker logs "$CONTAINER_NAME" 2>&1 | tail -30
    exit 1
fi

# --- Wait for at least one block to be mined ---
echo "=== Waiting for block production (up to ${MINE_WAIT}s) ==="
mine_elapsed=0
while [ "$mine_elapsed" -lt "$MINE_WAIT" ]; do
    BLOCK_HEX=$(rpc_call '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | extract_result)
    if [ -n "$BLOCK_HEX" ] && [ "$BLOCK_HEX" != "0x0" ]; then
        echo "Mining active: block $BLOCK_HEX"
        break
    fi
    sleep "$POLL_INTERVAL"
    mine_elapsed=$((mine_elapsed + POLL_INTERVAL))
done

if [ "$mine_elapsed" -ge "$MINE_WAIT" ]; then
    echo "WARN: No blocks mined within ${MINE_WAIT}s — ProgPow mining may be slow."
    echo "Continuing with contract deployment (transactions will be pending until mined)."
fi

# --- Deploy SimpleStorage contract ---
echo ""
echo "=== Step 1: Deploy SimpleStorage contract ==="
DEPLOY_RESULT=$(rpc_call "{
    \"jsonrpc\":\"2.0\",
    \"method\":\"personal_sendTransaction\",
    \"params\":[{
        \"from\": \"${TEST_ADDRESS}\",
        \"data\": \"${DEPLOY_BYTECODE}\",
        \"gas\": \"0x30D40\"
    }, \"${TEST_PASSWORD}\"],
    \"id\":1
}")
DEPLOY_TX=$(echo "$DEPLOY_RESULT" | extract_result)

if [ -z "$DEPLOY_TX" ] || echo "$DEPLOY_TX" | grep -q "error"; then
    echo "FAIL: Could not send deploy transaction"
    echo "Response: $DEPLOY_RESULT"
    exit 1
fi
echo "Deploy TX: $DEPLOY_TX"

# --- Wait for deploy transaction to be mined ---
echo "=== Waiting for deploy TX to be mined ==="
tx_wait=0
CONTRACT_ADDR=""
while [ "$tx_wait" -lt "$MINE_WAIT" ]; do
    RECEIPT=$(rpc_call "{
        \"jsonrpc\":\"2.0\",
        \"method\":\"eth_getTransactionReceipt\",
        \"params\":[\"${DEPLOY_TX}\"],
        \"id\":1
    }")
    # Check if result is not null
    if echo "$RECEIPT" | grep -q '"contractAddress"'; then
        CONTRACT_ADDR=$(echo "$RECEIPT" | sed -n 's/.*"contractAddress":"\([^"]*\)".*/\1/p')
        STATUS=$(echo "$RECEIPT" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
        if [ "$STATUS" = "0x1" ]; then
            echo "PASS: Contract deployed successfully at $CONTRACT_ADDR"
            break
        else
            echo "FAIL: Deploy transaction reverted (status: $STATUS)"
            exit 1
        fi
    fi
    sleep "$POLL_INTERVAL"
    tx_wait=$((tx_wait + POLL_INTERVAL))
done

if [ -z "$CONTRACT_ADDR" ]; then
    echo "FAIL: Deploy transaction not mined within ${MINE_WAIT}s"
    exit 1
fi

# --- Verify initial value via eth_call (get() should return 0) ---
echo ""
echo "=== Step 2: Verify initial value (get() == 0) ==="
GET_RESULT=$(rpc_call "{
    \"jsonrpc\":\"2.0\",
    \"method\":\"eth_call\",
    \"params\":[{
        \"to\": \"${CONTRACT_ADDR}\",
        \"data\": \"${GET_DATA}\"
    }, \"latest\"],
    \"id\":1
}")
GET_VALUE=$(echo "$GET_RESULT" | extract_result)
EXPECTED_ZERO="0x0000000000000000000000000000000000000000000000000000000000000000"

if [ "$GET_VALUE" = "$EXPECTED_ZERO" ]; then
    echo "PASS: get() returned 0 (initial value)"
else
    echo "FAIL: get() returned $GET_VALUE, expected $EXPECTED_ZERO"
    exit 1
fi

# --- Call set(42) ---
echo ""
echo "=== Step 3: Call set(42) ==="
SET_RESULT=$(rpc_call "{
    \"jsonrpc\":\"2.0\",
    \"method\":\"personal_sendTransaction\",
    \"params\":[{
        \"from\": \"${TEST_ADDRESS}\",
        \"to\": \"${CONTRACT_ADDR}\",
        \"data\": \"${SET_42_DATA}\",
        \"gas\": \"0x186A0\"
    }, \"${TEST_PASSWORD}\"],
    \"id\":1
}")
SET_TX=$(echo "$SET_RESULT" | extract_result)

if [ -z "$SET_TX" ] || echo "$SET_TX" | grep -q "error"; then
    echo "FAIL: Could not send set(42) transaction"
    echo "Response: $SET_RESULT"
    exit 1
fi
echo "set(42) TX: $SET_TX"

# --- Wait for set(42) to be mined ---
echo "=== Waiting for set(42) TX to be mined ==="
tx_wait=0
while [ "$tx_wait" -lt "$MINE_WAIT" ]; do
    RECEIPT=$(rpc_call "{
        \"jsonrpc\":\"2.0\",
        \"method\":\"eth_getTransactionReceipt\",
        \"params\":[\"${SET_TX}\"],
        \"id\":1
    }")
    if echo "$RECEIPT" | grep -q '"status"'; then
        STATUS=$(echo "$RECEIPT" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
        if [ -n "$STATUS" ]; then
            if [ "$STATUS" = "0x1" ]; then
                echo "PASS: set(42) transaction succeeded"
                break
            else
                echo "FAIL: set(42) transaction reverted (status: $STATUS)"
                exit 1
            fi
        fi
    fi
    sleep "$POLL_INTERVAL"
    tx_wait=$((tx_wait + POLL_INTERVAL))
done

if [ "$tx_wait" -ge "$MINE_WAIT" ]; then
    echo "FAIL: set(42) transaction not mined within ${MINE_WAIT}s"
    exit 1
fi

# --- Verify set(42) via eth_call (get() should return 42) ---
echo ""
echo "=== Step 4: Verify get() == 42 ==="
GET_RESULT=$(rpc_call "{
    \"jsonrpc\":\"2.0\",
    \"method\":\"eth_call\",
    \"params\":[{
        \"to\": \"${CONTRACT_ADDR}\",
        \"data\": \"${GET_DATA}\"
    }, \"latest\"],
    \"id\":1
}")
GET_VALUE=$(echo "$GET_RESULT" | extract_result)
EXPECTED_42="0x000000000000000000000000000000000000000000000000000000000000002a"

if [ "$GET_VALUE" = "$EXPECTED_42" ]; then
    echo "PASS: get() returned 42 (0x2a)"
else
    echo "FAIL: get() returned $GET_VALUE, expected $EXPECTED_42"
    exit 1
fi

echo ""
echo "========================================="
echo "  All contract tests passed!"
echo "========================================="
