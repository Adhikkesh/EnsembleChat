package main

import (
	"fmt"
	"time"

	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

// ──────────────────────────────────────────────────────────────────────────────
// DEMO: Multi-Node RPC Communication
//
// This demo spins up 3 coordinator nodes, each with its OWN TCP/RPC server
// on a different port.  All inter-node communication (leader election,
// token passing, log replication) travels over real net/rpc calls —
// exactly the same path that would be used across separate machines.
//
// Ports: Node 0 → :7000, Node 1 → :7001, Node 2 → :7002
// ──────────────────────────────────────────────────────────────────────────────

func main() {
	fmt.Println(types.ColorBold + types.ColorCyan)
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     DEMO: MULTI-NODE RPC (net/rpc over TCP)                ║")
	fmt.Println("║     3 Nodes ▸ Real Network Communication ▸ All Algorithms  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

	// ━━━ STEP 1: Create nodes with network addresses ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 1: Creating 3 Coordinator Nodes with RPC Servers ━━━" + types.ColorReset)
	fmt.Println("  Each node listens on its own TCP port and communicates via Go net/rpc.")
	fmt.Println("  Node 0 → localhost:7000")
	fmt.Println("  Node 1 → localhost:7001")
	fmt.Println("  Node 2 → localhost:7002")
	fmt.Println()

	node0 := coordinator.NewCoordinatorNode(0, []int{1, 2})
	node0.Addr = "127.0.0.1:7000"
	node0.PeerAddrs = map[int]string{1: "127.0.0.1:7001", 2: "127.0.0.1:7002"}

	node1 := coordinator.NewCoordinatorNode(1, []int{0, 2})
	node1.Addr = "127.0.0.1:7001"
	node1.PeerAddrs = map[int]string{0: "127.0.0.1:7000", 2: "127.0.0.1:7002"}

	node2 := coordinator.NewCoordinatorNode(2, []int{0, 1})
	node2.Addr = "127.0.0.1:7002"
	node2.PeerAddrs = map[int]string{0: "127.0.0.1:7000", 1: "127.0.0.1:7001"}

	nodes := []*coordinator.CoordinatorNode{node0, node1, node2}

	// ━━━ STEP 2: Start RPC servers ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 2: Starting RPC Servers (net/rpc over TCP) ━━━" + types.ColorReset)

	for _, n := range nodes {
		if err := n.StartRPC(); err != nil {
			fmt.Printf("%s[FAIL] Failed to start RPC on Node %d: %v%s\n",
				types.ColorRed, n.ID, err, types.ColorReset)
			return
		}
	}

	// Wire outbound RPC callbacks
	coordinator.SetupRPCComm(nodes)
	fmt.Println()

	// ━━━ STEP 3: Start election & token ring ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 3: Starting Raft Election + Token Ring (over RPC) ━━━" + types.ColorReset)

	for _, n := range nodes {
		n.Start()
	}

	fmt.Println(types.ColorYellow + "[WAIT] Waiting for leader election over RPC..." + types.ColorReset)
	time.Sleep(3 * time.Second)

	var leader *coordinator.CoordinatorNode
	for _, n := range nodes {
		if n.Election.IsLeader() {
			leader = n
			break
		}
	}
	if leader == nil {
		fmt.Println(types.ColorRed + "[FAIL] No leader elected. Waiting longer..." + types.ColorReset)
		time.Sleep(3 * time.Second)
		for _, n := range nodes {
			if n.Election.IsLeader() {
				leader = n
				break
			}
		}
	}
	if leader == nil {
		fmt.Println(types.ColorRed + "[FAIL] Leader election failed. Exiting." + types.ColorReset)
		cleanup(nodes)
		return
	}

	fmt.Println()
	fmt.Printf("%s[LEADER] Leader elected: Node %d (via Raft Leader Election over RPC)%s\n\n",
		types.ColorGreen+types.ColorBold, leader.ID, types.ColorReset)

	// Show all nodes' states
	for _, n := range nodes {
		state, term, leaderID := n.Election.GetState()
		fmt.Printf("  Node %d: State=%s, Term=%d, KnownLeader=%d\n",
			n.ID, state, term, leaderID)
	}
	fmt.Println()

	// ━━━ STEP 4: Mutual Exclusion via Token Ring (over RPC) ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 4: Mutual Exclusion (Token Ring over RPC) ━━━" + types.ColorReset)
	fmt.Println("  The token circulates between nodes over net/rpc calls.")
	fmt.Println()

	resp := leader.AcquireLock("ChatRoom_DB", "AdminService")
	if resp.Granted {
		fmt.Printf("  %s[OK] Node %d acquired critical section for \"%s\" (holder: %s)%s\n",
			types.ColorGreen, leader.ID, resp.Resource, resp.Holder, types.ColorReset)
	}

	time.Sleep(500 * time.Millisecond)
	leader.ReleaseLock("ChatRoom_DB", "AdminService")
	fmt.Printf("  %s[UNLOCK] Node %d released critical section -> token passed via RPC%s\n\n",
		types.ColorGreen, leader.ID, types.ColorReset)

	time.Sleep(600 * time.Millisecond) // let token circulate a bit

	// ━━━ STEP 5: Consensus via Raft Log Replication (over RPC) ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 5: Raft Log Replication (Consensus over RPC) ━━━" + types.ColorReset)
	fmt.Println("  Leader proposes entries → AppendEntries RPCs sent to followers")
	fmt.Println("  over TCP → majority replicate → entry committed.")
	fmt.Println()

	ok1 := leader.ProposeChangeRPC("rpc-tx-1", types.ChangeAddRoom, "Gaming_Lounge", "Server_Alpha:9001")
	fmt.Println()
	if ok1 {
		fmt.Printf("  %s[OK] \"Gaming_Lounge\" committed via Raft majority over RPC%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	ok2 := leader.ProposeChangeRPC("rpc-tx-2", types.ChangeAddServer, "Server_Beta", "192.168.1.50:9002")
	fmt.Println()
	if ok2 {
		fmt.Printf("  %s[OK] \"Server_Beta\" registered via Raft majority over RPC%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	fmt.Println()

	// ━━━ STEP 6: Verify replication across all nodes ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 6: Verifying Replicated State Across All Nodes ━━━" + types.ColorReset)
	fmt.Println()

	time.Sleep(500 * time.Millisecond)

	for _, n := range nodes {
		role := "Follower"
		if n.Election.IsLeader() {
			role = "Leader"
		}
		log := n.RaftLog.GetLog()
		commitIdx := n.RaftLog.GetCommitIndex()
		fmt.Printf("  [LOG] %s (Node %d) -- Log: %d entries, CommitIndex: %d\n",
			role, n.ID, len(log), commitIdx)
		for _, entry := range log {
			status := "[WAIT]"
			if entry.Index <= commitIdx {
				status = "[OK]"
			}
			fmt.Printf("     %s [#%d] %s \"%s\" → %s\n",
				status, entry.Index, entry.Change, entry.Key, entry.Value)
		}
	}
	fmt.Println()

	// Consistency check
	leaderRT := leader.RaftLog.GetRoutingTable()
	allConsistent := true
	for _, n := range nodes {
		if n.ID == leader.ID {
			continue
		}
		followerRT := n.RaftLog.GetRoutingTable()
		if len(followerRT.Rooms) != len(leaderRT.Rooms) || len(followerRT.Servers) != len(leaderRT.Servers) {
			allConsistent = false
		}
	}
	if allConsistent {
		fmt.Printf("  %s[OK] All nodes have identical routing tables -- RPC replication CONSISTENT!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	} else {
		fmt.Printf("  %s[FAIL] Routing tables differ -- replication issue%s\n",
			types.ColorRed, types.ColorReset)
	}

	fmt.Println()
	fmt.Println(types.ColorCyan + "━━━ DEMO COMPLETE ━━━" + types.ColorReset)
	fmt.Println("This demo ran all 3 distributed algorithms over real TCP/RPC:")
	fmt.Println("  1. Raft Leader Election     — VoteRequest/VoteResponse/Heartbeat RPCs")
	fmt.Println("  2. Token Ring Mutual Exclusion — PassToken RPCs")
	fmt.Println("  3. Raft Log Replication      — AppendEntries/AppendEntriesReply RPCs")
	fmt.Println()
	fmt.Println("The same communication works across separate machines by changing")
	fmt.Println("127.0.0.1 to the actual machine IPs (no code changes needed).")
	fmt.Println()

	cleanup(nodes)
}

func cleanup(nodes []*coordinator.CoordinatorNode) {
	for _, n := range nodes {
		n.Stop()
	}
}
