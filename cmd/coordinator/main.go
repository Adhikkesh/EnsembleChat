package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

func main() {
	id := flag.Int("id", 0, "Node ID (unique integer)")
	addr := flag.String("addr", ":7000", "Listen address for this coordinator")
	peers := flag.String("peers", "", "Comma-separated peer list: id=host:port,id=host:port,...")
	flag.Parse()

	fmt.Printf("%s🚀 Starting Coordinator Node %d at %s%s\n",
		types.ColorGreen+types.ColorBold, *id, *addr, types.ColorReset)

	// Parse peers into map[int]string and collect peer IDs
	peerAddrs := make(map[int]string)
	var peerIDs []int

	if *peers != "" {
		for _, entry := range strings.Split(*peers, ",") {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "invalid peer format %q (expected id=host:port)\n", entry)
				os.Exit(1)
			}
			pid, err := strconv.Atoi(parts[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid peer id %q: %v\n", parts[0], err)
				os.Exit(1)
			}
			peerAddrs[pid] = parts[1]
			peerIDs = append(peerIDs, pid)
		}
	}

	node := coordinator.NewCoordinatorNode(*id, peerIDs)
	node.Addr = *addr
	node.PeerAddrs = peerAddrs

	// Start TCP listener before election so peers can connect
	if err := node.StartTCP(); err != nil {
		fmt.Fprintf(os.Stderr, "TCP start failed: %v\n", err)
		os.Exit(1)
	}

	// Wire TCP-based send callbacks
	coordinator.SetupTCPComm([]*coordinator.CoordinatorNode{node})

	// Begin the election loop
	node.Start()

	fmt.Printf("%s📡 Coordinator Node %d running. Press Ctrl+C to stop.%s\n",
		types.ColorCyan, *id, types.ColorReset)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\n%s🛑 Shutting down Coordinator Node %d...%s\n",
		types.ColorYellow, *id, types.ColorReset)
	node.Stop()
}
