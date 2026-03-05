#!/usr/bin/env bash
# ╔══════════════════════════════════════════════════════════════╗
# ║  Launch 3 Coordinator Nodes as Separate Processes           ║
# ║  Each node runs its own RPC server — true multi-process     ║
# ╚══════════════════════════════════════════════════════════════╝
#
# Usage:  bash scripts/launch_cluster.sh
#
# Opens 3 terminal tabs / background processes.
# Communication happens over net/rpc (TCP).

set -e

echo "Building coordinator binary..."
go build -o bin/coordinator ./cmd/coordinator
echo "Build successful."
echo ""

BINARY="./bin/coordinator"

echo "Starting 3 coordinator nodes..."
echo "  Node 0 → localhost:7000"
echo "  Node 1 → localhost:7001"
echo "  Node 2 → localhost:7002"
echo ""

# Launch each node in a new terminal window (or background if no GUI)
if command -v gnome-terminal &> /dev/null; then
    gnome-terminal --title="Node 0" -- bash -c "$BINARY -id 0 -addr :7000 -peers '1=127.0.0.1:7001,2=127.0.0.1:7002'; exec bash"
    sleep 0.5
    gnome-terminal --title="Node 1" -- bash -c "$BINARY -id 1 -addr :7001 -peers '0=127.0.0.1:7000,2=127.0.0.1:7002'; exec bash"
    sleep 0.5
    gnome-terminal --title="Node 2" -- bash -c "$BINARY -id 2 -addr :7002 -peers '0=127.0.0.1:7000,1=127.0.0.1:7001'; exec bash"
elif command -v xterm &> /dev/null; then
    xterm -title "Node 0" -e "$BINARY -id 0 -addr :7000 -peers '1=127.0.0.1:7001,2=127.0.0.1:7002'" &
    sleep 0.5
    xterm -title "Node 1" -e "$BINARY -id 1 -addr :7001 -peers '0=127.0.0.1:7000,2=127.0.0.1:7002'" &
    sleep 0.5
    xterm -title "Node 2" -e "$BINARY -id 2 -addr :7002 -peers '0=127.0.0.1:7000,1=127.0.0.1:7001'" &
else
    echo "No GUI terminal found. Launching in background (use 'fg' or check logs)."
    $BINARY -id 0 -addr :7000 -peers "1=127.0.0.1:7001,2=127.0.0.1:7002" &
    sleep 0.5
    $BINARY -id 1 -addr :7001 -peers "0=127.0.0.1:7000,2=127.0.0.1:7002" &
    sleep 0.5
    $BINARY -id 2 -addr :7002 -peers "0=127.0.0.1:7000,1=127.0.0.1:7001" &
fi

echo ""
echo "3 coordinator nodes launched."
echo "Type 'help' in each terminal for interactive commands."
echo ""
