package main

import (
	"fmt"
	"time"

	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

func main() {
	fmt.Println(types.ColorBold + types.ColorGreen)
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║       DEMO: CONSENSUS via Raft Log Replication             ║")
	fmt.Println("║       Replicating Chat Room Creation Across 3 Nodes        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

	// ━━━ STEP 1 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 1: Starting 3 Coordinator Nodes ━━━" + types.ColorReset)

	node0 := coordinator.NewCoordinatorNode(0, []int{1, 2})
	node1 := coordinator.NewCoordinatorNode(1, []int{0, 2})
	node2 := coordinator.NewCoordinatorNode(2, []int{0, 1})

	nodes := []*coordinator.CoordinatorNode{node0, node1, node2}
	coordinator.SetupInProcessComm(nodes)

	for _, n := range nodes {
		n.Start()
	}

	fmt.Println(types.ColorYellow + "⏳ Waiting for leader election..." + types.ColorReset)
	time.Sleep(2 * time.Second)

	var leader *coordinator.CoordinatorNode
	for _, n := range nodes {
		if n.Election.IsLeader() {
			leader = n
			break
		}
	}

	if leader == nil {
		fmt.Println(types.ColorRed + "❌ No leader elected. Waiting more..." + types.ColorReset)
		time.Sleep(3 * time.Second)
		for _, n := range nodes {
			if n.Election.IsLeader() {
				leader = n
				break
			}
		}
		if leader == nil {
			fmt.Println(types.ColorRed + "❌ No leader. Exiting." + types.ColorReset)
			return
		}
	}

	fmt.Printf("\n%s👑 Leader: Node %d%s\n\n",
		types.ColorGreen+types.ColorBold, leader.ID, types.ColorReset)

	// ━━━ STEP 2 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 2: Creating Chat Room via Raft Log Replication ━━━" + types.ColorReset)
	fmt.Println("  The Leader will propose: Create room \"Gaming_Lounge\" on Server_Alpha")
	fmt.Println("  Flow: Leader appends entry → sends AppendEntries to followers →")
	fmt.Println("        followers replicate → majority reached → entry committed")
	fmt.Println()

	success := leader.ProposeChange(
		"entry-001",
		types.ChangeAddRoom,
		"Gaming_Lounge",
		"Server_Alpha:9001",
	)

	fmt.Println()
	if success {
		fmt.Printf("%s✅ Entry COMMITTED — Room created via Raft majority!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	} else {
		fmt.Printf("%s❌ Entry FAILED to commit%s\n",
			types.ColorRed+types.ColorBold, types.ColorReset)
	}

	// ━━━ STEP 3 ━━━
	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 3: Creating a Second Chat Room ━━━" + types.ColorReset)

	success2 := leader.ProposeChange(
		"entry-002",
		types.ChangeAddRoom,
		"Study_Group",
		"Server_Beta:9002",
	)

	fmt.Println()
	if success2 {
		fmt.Printf("%s✅ Entry COMMITTED — Room created via Raft majority!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	// ━━━ STEP 4 ━━━
	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 4: Registering New Chat Server via Raft ━━━" + types.ColorReset)

	success3 := leader.ProposeChange(
		"entry-003",
		types.ChangeAddServer,
		"Server_Gamma",
		"192.168.1.103:9003",
	)

	fmt.Println()
	if success3 {
		fmt.Printf("%s✅ Entry COMMITTED — Server registered via Raft majority!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	// ━━━ STEP 5 ━━━
	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 5: Verifying Raft Log Replication Across All Nodes ━━━" + types.ColorReset)
	fmt.Println()

	// Allow time for commit index to propagate to all followers
	time.Sleep(500 * time.Millisecond)

	for _, n := range nodes {
		role := "Follower"
		if n.Election.IsLeader() {
			role = "Leader"
		}
		log := n.RaftLog.GetLog()
		commitIdx := n.RaftLog.GetCommitIndex()
		fmt.Printf("  %s📋 %s (Node %d) — Log: %d entries, CommitIndex: %d%s\n",
			types.ColorCyan, role, n.ID, len(log), commitIdx, types.ColorReset)
		for _, entry := range log {
			committed := ""
			if entry.Index <= commitIdx {
				committed = " ✅ COMMITTED"
			}
			fmt.Printf("     [#%d term=%d] %s \"%s\" → %s%s\n",
				entry.Index, entry.Term, entry.Change, entry.Key, entry.Value, committed)
		}
		fmt.Println()
	}

	// ━━━ STEP 6 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 6: Verifying State Machine (Routing Table) Consistency ━━━" + types.ColorReset)
	fmt.Println()

	leaderRT := leader.RaftLog.GetRoutingTable()
	fmt.Printf("  %s📋 Leader (Node %d) Routing Table:%s\n",
		types.ColorGreen, leader.ID, types.ColorReset)
	printRoutingTable(leaderRT)

	for _, n := range nodes {
		if n.ID == leader.ID {
			continue
		}
		fmt.Printf("  %s📋 Follower (Node %d) Routing Table:%s\n",
			types.ColorBlue, n.ID, types.ColorReset)
		printRoutingTable(n.RaftLog.GetRoutingTable())
	}

	// ━━━ Consistency Check ━━━
	fmt.Println(types.ColorYellow + "━━━ Consistency Check ━━━" + types.ColorReset)

	allConsistent := true
	for _, n := range nodes {
		if n.ID == leader.ID {
			continue
		}
		followerRT := n.RaftLog.GetRoutingTable()
		if len(followerRT.Rooms) != len(leaderRT.Rooms) {
			allConsistent = false
			fmt.Printf("  %s❌ Node %d has %d rooms, Leader has %d rooms%s\n",
				types.ColorRed, n.ID, len(followerRT.Rooms), len(leaderRT.Rooms), types.ColorReset)
		}
		if len(followerRT.Servers) != len(leaderRT.Servers) {
			allConsistent = false
			fmt.Printf("  %s❌ Node %d has %d servers, Leader has %d servers%s\n",
				types.ColorRed, n.ID, len(followerRT.Servers), len(leaderRT.Servers), types.ColorReset)
		}
	}

	if allConsistent {
		fmt.Printf("  %s✅ All nodes have identical routing tables — CONSISTENT!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	fmt.Println()
	fmt.Println(types.ColorGreen + "━━━ DEMO COMPLETE ━━━" + types.ColorReset)
	fmt.Println("Key takeaways (Raft Log Replication):")
	fmt.Println("  1. Leader appends entry to its log")
	fmt.Println("  2. Leader sends AppendEntries RPC to all followers")
	fmt.Println("  3. Followers append entries and acknowledge")
	fmt.Println("  4. Once majority (2/3) replicate, entry is committed")
	fmt.Println("  5. Committed entries are applied to the state machine")
	fmt.Println("  6. All nodes converge to the same state (strong consistency)")
	fmt.Println()

	for _, n := range nodes {
		n.Stop()
	}
}

func printRoutingTable(rt *types.RoutingTable) {
	if len(rt.Rooms) == 0 && len(rt.Servers) == 0 {
		fmt.Println("     (empty)")
		return
	}
	for name, entry := range rt.Rooms {
		fmt.Printf("     Room: \"%s\" → %s\n", name, entry.ServerAddr)
	}
	for name, addr := range rt.Servers {
		fmt.Printf("     Server: \"%s\" → %s\n", name, addr)
	}
	fmt.Println()
}
