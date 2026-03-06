package main

import (
	"bufio"
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

	fmt.Printf("%s[START] Starting Coordinator Node %d at %s%s\n",
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

	// Start net/rpc server before election so peers can connect
	if err := node.StartRPC(); err != nil {
		fmt.Fprintf(os.Stderr, "RPC start failed: %v\n", err)
		os.Exit(1)
	}

	// Wire RPC-based send callbacks
	coordinator.SetupRPCComm([]*coordinator.CoordinatorNode{node})

	// Begin the election loop
	node.Start()

	fmt.Printf("%s[RPC] Coordinator Node %d running. Type 'help' for commands.%s\n",
		types.ColorCyan, *id, types.ColorReset)
	fmt.Println()

	// Handle Ctrl+C in background
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Printf("\n%s[STOP] Shutting down Coordinator Node %d...%s\n",
			types.ColorYellow, *id, types.ColorReset)
		node.Stop()
		os.Exit(0)
	}()

	// Interactive command loop
	scanner := bufio.NewScanner(os.Stdin)
	printHelp()

	for {
		fmt.Printf("%s> %s", types.ColorCyan, types.ColorReset)
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "help":
			printHelp()

		case "status":
			state, term, leaderID := node.Election.GetState()
			fmt.Printf("  Node %d: State=%s, Term=%d, KnownLeader=%d\n",
				node.ID, state, term, leaderID)
			if node.Election.IsLeader() {
				fmt.Printf("  %s[LEADER] This node IS the leader%s\n",
					types.ColorGreen+types.ColorBold, types.ColorReset)
			} else {
				fmt.Printf("  %s[FOLLOW] This node is a follower%s\n",
					types.ColorBlue, types.ColorReset)
			}

		case "lock":
			if len(parts) != 3 {
				fmt.Println("  Usage: lock <resource> <requester>")
				continue
			}
			resp := node.AcquireLock(parts[1], parts[2])
			if resp.Granted {
				fmt.Printf("  %s[OK] GRANTED critical section on \"%s\" to [%s] (via Token Ring)%s\n",
					types.ColorGreen, resp.Resource, resp.Holder, types.ColorReset)
			} else {
				fmt.Printf("  %s[DENIED] DENIED critical section on \"%s\" -- %s%s\n",
					types.ColorRed, resp.Resource, resp.Reason, types.ColorReset)
			}

		case "unlock":
			if len(parts) != 3 {
				fmt.Println("  Usage: unlock <resource> <requester>")
				continue
			}
			ok := node.ReleaseLock(parts[1], parts[2])
			if ok {
				fmt.Printf("  %s[UNLOCK] Released critical section on \"%s\" (token passed)%s\n",
					types.ColorGreen, parts[1], types.ColorReset)
			} else {
				fmt.Printf("  %s[FAIL] Failed to release on \"%s\"%s\n",
					types.ColorRed, parts[1], types.ColorReset)
			}

		case "token":
			hasToken, inCS, resource, holder := node.TokenRing.GetStatus()
			if hasToken && inCS {
				fmt.Printf("  [TOKEN] Token: HELD -- IN CRITICAL SECTION (resource: %s, holder: %s)\n", resource, holder)
			} else if hasToken {
				fmt.Println("  [TOKEN] Token: HELD (idle, will pass soon)")
			} else {
				fmt.Println("  [TOKEN] Token: NOT HERE (circulating in ring)")
			}
			fmt.Printf("  [RING] Next node in ring: Node %d\n", node.TokenRing.GetNextNode())

		case "propose":
			// propose <txID> <add_room|add_server> <key> <value>
			if len(parts) != 5 {
				fmt.Println("  Usage: propose <txID> <add_room|add_server> <key> <value>")
				continue
			}
			txID := parts[1]
			var changeType types.ChangeType
			switch strings.ToLower(parts[2]) {
			case "add_room":
				changeType = types.ChangeAddRoom
			case "add_server":
				changeType = types.ChangeAddServer
			default:
				fmt.Printf("  Unknown change type %q. Use add_room or add_server.\n", parts[2])
				continue
			}
			key := parts[3]
			value := parts[4]

			fmt.Printf("  %s[PROPOSE] Proposing %s: %s = %s (tx: %s)...%s\n",
				types.ColorPurple, changeType, key, value, txID, types.ColorReset)

			success := node.ProposeChangeRPC(txID, changeType, key, value)
			if success {
				fmt.Printf("  %s[OK] Raft Log Entry COMMITTED (majority replicated)%s\n",
					types.ColorGreen+types.ColorBold, types.ColorReset)
			} else {
				fmt.Printf("  %s[FAIL] Raft Log Entry FAILED to commit%s\n",
					types.ColorRed+types.ColorBold, types.ColorReset)
			}

		case "state":
			fmt.Println("  [LOG] Routing Table (from Raft Log state machine):")
			rt := node.RaftLog.GetRoutingTable()
			if len(rt.Rooms) == 0 && len(rt.Servers) == 0 {
				fmt.Println("     (empty)")
			} else {
				for name, entry := range rt.Rooms {
					fmt.Printf("     Room: \"%s\" → %s\n", name, entry.ServerAddr)
				}
				for name, addr := range rt.Servers {
					fmt.Printf("     Server: \"%s\" → %s\n", name, addr)
				}
			}
			log := node.RaftLog.GetLog()
			commitIdx := node.RaftLog.GetCommitIndex()
			fmt.Printf("  [LOG] Raft Log: %d entries, CommitIndex: %d\n", len(log), commitIdx)

		case "quit", "exit":
			fmt.Printf("%s[STOP] Shutting down Coordinator Node %d...%s\n",
				types.ColorYellow, *id, types.ColorReset)
			node.Stop()
			return

		default:
			fmt.Printf("  Unknown command: %q. Type 'help' for commands.\n", cmd)
		}
	}
}

func printHelp() {
	fmt.Println(types.ColorYellow + "━━━ Available Commands ━━━" + types.ColorReset)
	fmt.Println("  status                                  — Show election state")
	fmt.Println("  lock <resource> <requester>              — Enter critical section (Token Ring)")
	fmt.Println("  unlock <resource> <requester>            — Exit critical section (passes token)")
	fmt.Println("  token                                   — Show token ring status")
	fmt.Println("  propose <txID> add_room <name> <server>  — Raft: replicate room creation")
	fmt.Println("  propose <txID> add_server <name> <addr>  — Raft: replicate server registration")
	fmt.Println("  state                                   — Show routing table & Raft log")
	fmt.Println("  help                                    — Show this help")
	fmt.Println("  quit                                    — Shutdown node")
	fmt.Println()
}
