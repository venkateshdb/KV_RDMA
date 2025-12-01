#!/bin/bash

# run-rdma-cluster.sh - Script to run RDMA KV store benchmark on CloudLab

set -euo pipefail

# Configuration
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_ROOT="${ROOT}/logs-rdma"
BIN="${ROOT}/kv-rdma"

# Default args
IB_PORT=2
GID_INDEX=2
DURATION="30s"

# Function to display usage
usage() {
    echo "Usage: $0 [server_count] [client_count]"
    echo "  server_count: Number of server nodes to use [optional - defaults to half]"
    echo "  client_count: Number of client nodes to use [optional - defaults to remaining]"
    exit 1
}

function cluster_size() {
    /usr/local/etc/emulab/tmcc hostnames | wc -l
}

AVAILABLE_COUNT=$(cluster_size)

if [ "$#" -eq 0 ]; then
    SERVER_COUNT=$((AVAILABLE_COUNT / 2))
    CLIENT_COUNT=$((AVAILABLE_COUNT - SERVER_COUNT))
elif [ "$#" -eq 1 ]; then
    SERVER_COUNT="$1"
    CLIENT_COUNT=$((AVAILABLE_COUNT - SERVER_COUNT))
elif [ "$#" -eq 2 ]; then
    SERVER_COUNT="$1"
    CLIENT_COUNT="$2"
else
    usage
fi

# Validate
if [ "$SERVER_COUNT" -eq 0 ] || [ "$CLIENT_COUNT" -eq 0 ]; then
    echo "Error: Need at least 1 server and 1 client"
    exit 1
fi

TOTAL_NEEDED=$((SERVER_COUNT + CLIENT_COUNT))
if [ "$TOTAL_NEEDED" -gt "$AVAILABLE_COUNT" ]; then
    echo "Error: Not enough nodes"
    exit 1
fi

echo "Using $TOTAL_NEEDED nodes (Servers: $SERVER_COUNT, Clients: $CLIENT_COUNT)"

# Build node lists
SERVER_NODES=()
for ((i=0; i<SERVER_COUNT; i++)); do
    SERVER_NODES+=("node$i")
done

CLIENT_NODES=()
for ((i=SERVER_COUNT; i<SERVER_COUNT+CLIENT_COUNT; i++)); do
    CLIENT_NODES+=("node$i")
done

SSH_OPTS="-o StrictHostKeyChecking=no"
SSH="ssh ${SSH_OPTS}"

# Logs
TS=$(date +"%Y%m%d-%H%M%S")
LOG_DIR="$LOG_ROOT/$TS"
mkdir -p "$LOG_DIR"
rm -f "$LOG_ROOT/latest"
ln -s "$LOG_DIR" "$LOG_ROOT/latest"
echo "Logs in $LOG_DIR"

cleanup() {
    echo "Cleaning up..."
    for node in "${SERVER_NODES[@]}" "${CLIENT_NODES[@]}"; do
        ${SSH} $node "pkill -f 'kv-rdma' || true" 2>/dev/null || true
    done
}
trap cleanup EXIT INT TERM

echo "Initial cleanup..."
cleanup

echo "Building..."
export PATH=$PATH:/usr/local/go/bin
go build -o kv-rdma cmd/main.go

# Start Servers
for node in "${SERVER_NODES[@]}"; do
    echo "Starting server on $node..."
    ${SSH} $node "${ROOT}/kv-rdma -mode server -addr :8090 -dev mlx4_0 -ib-port $IB_PORT -gid-index $GID_INDEX > \"$LOG_DIR/server-$node.log\" 2>&1 &"
done

sleep 5

# Start Clients (Benchmark)
# Build comma-separated list of server hosts with port 8090
SERVER_HOSTS=""
for node in "${SERVER_NODES[@]}"; do
    if [ -n "$SERVER_HOSTS" ]; then
        SERVER_HOSTS="$SERVER_HOSTS,$node:8090"
    else
        SERVER_HOSTS="$node:8090"
    fi
done

WORKERS=${WORKERS:-16} # Default workers per client node
BATCH_SIZE=64 # Optimized batch size

CLIENT_PIDS=()
for node in "${CLIENT_NODES[@]}"; do
    echo "Starting benchmark on $node..."
    ${SSH} $node "${ROOT}/kv-rdma -mode bench -hosts $SERVER_HOSTS -dev mlx4_0 -ib-port $IB_PORT -gid-index $GID_INDEX -duration $DURATION -workers $WORKERS -batch-size $BATCH_SIZE > \"$LOG_DIR/client-$node.log\" 2>&1" &
    CLIENT_PIDS+=($!)
done

echo "Waiting for benchmarks..."
for pid in "${CLIENT_PIDS[@]}"; do
    wait $pid
done

echo "Done. Logs in $LOG_DIR"
python3 report-tput.py
