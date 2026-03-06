package main

import (
	"fmt"
	"time"

	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

func main() {
	fmt.Println(types.ColorBold + types.ColorCyan)
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║       DEMO: LEADER ELECTION & FAILURE RECOVERY             ║")
	fmt.Println("║       Simplified Raft Protocol with 3 Coordinator Nodes     ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

	fmt.Println(types.ColorYellow + "━━━ STEP 1: Creating 3 Coordinator Nodes ━━━" + types.ColorReset)

	node0 := coordinator.NewCoordinatorNode(0, []int{1, 2})
	node1 := coordinator.NewCoordinatorNode(1, []int{0, 2})
	node2 := coordinator.NewCoordinatorNode(2, []int{0, 1})

	nodes := []*coordinator.CoordinatorNode{node0, node1, node2}

	coordinator.SetupInProcessComm(nodes)

	fmt.Println()

	fmt.Println(types.ColorYellow + "━━━ STEP 2: Starting All Nodes (all begin as FOLLOWER) ━━━" + types.ColorReset)

	for _, n := range nodes {
		n.Start()
	}

	fmt.Println()
	fmt.Println(types.ColorYellow + "[WAIT] Waiting for leader election..." + types.ColorReset)
	time.Sleep(2 * time.Second)

	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 3: Current Cluster State ━━━" + types.ColorReset)

	var leaderNode *coordinator.CoordinatorNode
	for _, n := range nodes {
		state, term, leaderID := n.Election.GetState()
		fmt.Printf("  Node %d: State=%s, Term=%d, KnownLeader=%d\n",
			n.ID, state, term, leaderID)
		if n.Election.IsLeader() {
			leaderNode = n
		}
	}

	if leaderNode == nil {
		fmt.Println(types.ColorRed + "[FAIL] No leader elected yet. Waiting longer..." + types.ColorReset)
		time.Sleep(3 * time.Second)
		for _, n := range nodes {
			if n.Election.IsLeader() {
				leaderNode = n
				break
			}
		}
	}

	if leaderNode == nil {
		fmt.Println(types.ColorRed + "[FAIL] Failed to elect a leader. Exiting." + types.ColorReset)
		return
	}

	fmt.Printf("\n%s[LEADER] Current Leader: Node %d%s\n\n",
		types.ColorGreen+types.ColorBold, leaderNode.ID, types.ColorReset)

	fmt.Println(types.ColorRed + types.ColorBold)
	fmt.Println("━━━ STEP 4: KILLING THE LEADER ━━━")
	fmt.Printf("  Simulating crash of Node %d...\n", leaderNode.ID)
	fmt.Println(types.ColorReset)

	leaderNode.Kill()

	fmt.Println()
	fmt.Println(types.ColorYellow + "[WAIT] Waiting for followers to detect missing heartbeats and start new election..." + types.ColorReset)
	time.Sleep(3 * time.Second)

	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 5: New Cluster State After Recovery ━━━" + types.ColorReset)

	var newLeader *coordinator.CoordinatorNode
	for _, n := range nodes {
		if !n.IsAlive() {
			fmt.Printf("  Node %d: DEAD (was the old leader)\n", n.ID)
			continue
		}
		state, term, leaderID := n.Election.GetState()
		fmt.Printf("  Node %d: State=%s, Term=%d, KnownLeader=%d\n",
			n.ID, state, term, leaderID)
		if n.Election.IsLeader() {
			newLeader = n
		}
	}

	fmt.Println()
	if newLeader != nil {
		fmt.Printf("%s[LEADER] NEW Leader Elected: Node %d%s\n",
			types.ColorGreen+types.ColorBold, newLeader.ID, types.ColorReset)
		fmt.Println(types.ColorGreen + "[OK] Leader failover completed successfully!" + types.ColorReset)
	} else {
		fmt.Println(types.ColorYellow + "[WAIT] Election still in progress... waiting more" + types.ColorReset)
		time.Sleep(3 * time.Second)
		for _, n := range nodes {
			if n.IsAlive() && n.Election.IsLeader() {
				newLeader = n
				break
			}
		}
		if newLeader != nil {
			fmt.Printf("%s[LEADER] NEW Leader Elected: Node %d%s\n",
				types.ColorGreen+types.ColorBold, newLeader.ID, types.ColorReset)
		}
	}

	fmt.Println()
	fmt.Println(types.ColorCyan + "━━━ DEMO COMPLETE ━━━" + types.ColorReset)
	fmt.Println("Key takeaways:")
	fmt.Println("  1. Leader was elected via majority vote (Raft-style)")
	fmt.Println("  2. When leader crashed, heartbeats stopped")
	fmt.Println("  3. Followers detected timeout and started new election")
	fmt.Println("  4. A new leader was elected with a higher term number")
	fmt.Println()

	for _, n := range nodes {
		if n.IsAlive() {
			n.Stop()
		}
	}
}
