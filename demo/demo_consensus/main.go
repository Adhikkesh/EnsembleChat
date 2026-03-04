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
	fmt.Println("║       DEMO: CONSENSUS via Two-Phase Commit (2PC)           ║")
	fmt.Println("║       Replicating Chat Room Creation Across 3 Nodes        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

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

	fmt.Println(types.ColorYellow + "━━━ STEP 2: Creating Chat Room via 2PC ━━━" + types.ColorReset)
	fmt.Println("  The Leader will propose: Create room \"Gaming_Lounge\" on Server_Alpha")
	fmt.Println("  All followers must agree (vote COMMIT) for the change to be applied.")
	fmt.Println()

	success := leader.ProposeChange(
		"tx-room-001",
		types.ChangeAddRoom,
		"Gaming_Lounge",
		"Server_Alpha:9001",
	)

	fmt.Println()
	if success {
		fmt.Printf("%s✅ 2PC Transaction COMMITTED — Room created!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	} else {
		fmt.Printf("%s❌ 2PC Transaction ABORTED%s\n",
			types.ColorRed+types.ColorBold, types.ColorReset)
	}

	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 3: Creating a Second Chat Room via 2PC ━━━" + types.ColorReset)

	success2 := leader.ProposeChange(
		"tx-room-002",
		types.ChangeAddRoom,
		"Study_Group",
		"Server_Beta:9002",
	)

	fmt.Println()
	if success2 {
		fmt.Printf("%s✅ 2PC Transaction COMMITTED — Room created!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 4: Registering New Chat Server via 2PC ━━━" + types.ColorReset)

	success3 := leader.ProposeChange(
		"tx-server-001",
		types.ChangeAddServer,
		"Server_Gamma",
		"192.168.1.103:9003",
	)

	fmt.Println()
	if success3 {
		fmt.Printf("%s✅ 2PC Transaction COMMITTED — Server registered!%s\n",
			types.ColorGreen+types.ColorBold, types.ColorReset)
	}

	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 5: Verifying State Replication Across All Nodes ━━━" + types.ColorReset)
	fmt.Println()

	leaderRT := leader.TwoPCCoord.GetRoutingTable()
	fmt.Printf("  %s📋 Leader (Node %d) Routing Table:%s\n",
		types.ColorGreen, leader.ID, types.ColorReset)
	printRoutingTable(leaderRT)

	for _, n := range nodes {
		if n.ID == leader.ID {
			continue
		}
		fmt.Printf("  %s📋 Follower (Node %d) Routing Table:%s\n",
			types.ColorBlue, n.ID, types.ColorReset)
		printRoutingTable(n.TwoPCPart.RoutingTable)
	}

	fmt.Println(types.ColorYellow + "━━━ Consistency Check ━━━" + types.ColorReset)

	allConsistent := true
	for _, n := range nodes {
		if n.ID == leader.ID {
			continue
		}
		followerRT := n.TwoPCPart.RoutingTable
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
	fmt.Println("Key takeaways:")
	fmt.Println("  1. Leader initiated 2PC: sent PREPARE to all followers")
	fmt.Println("  2. All followers validated and voted COMMIT")
	fmt.Println("  3. Leader sent COMMIT → all nodes applied the change")
	fmt.Println("  4. Routing table is now identical across all nodes (consistency!)")
	fmt.Println("  5. If any node had voted ABORT, the transaction would be rolled back")
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
