package main

import (
	"fmt"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

func main() {
	fmt.Println(types.ColorBold + types.ColorPurple)
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║       DEMO: MUTUAL EXCLUSION (Lease-Based Locking)         ║")
	fmt.Println("║       Two Servers Fight for the Same Chat Room Lock         ║")
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

	fmt.Println(types.ColorYellow + "━━━ STEP 2: Two Servers Racing to Lock \"General_Chat\" ━━━" + types.ColorReset)
	fmt.Println("  Server_A and Server_B will compete for the same resource...")
	fmt.Println()

	var wg sync.WaitGroup
	results := make([]types.LockResponse, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()
		fmt.Printf("%s🏃 Server_A requesting lock on \"General_Chat\"...%s\n",
			types.ColorGreen, types.ColorReset)
		results[0] = leader.AcquireLock("General_Chat", "Server_A")
	}()

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("%s🏃 Server_B requesting lock on \"General_Chat\"...%s\n",
			types.ColorBlue, types.ColorReset)
		results[1] = leader.AcquireLock("General_Chat", "Server_B")
	}()

	wg.Wait()
	fmt.Println()

	fmt.Println(types.ColorYellow + "━━━ STEP 3: Lock Request Results ━━━" + types.ColorReset)
	fmt.Println()

	for i, r := range results {
		serverName := "Server_A"
		if i == 1 {
			serverName = "Server_B"
		}

		if r.Granted {
			fmt.Printf("  %s✅ %s: GRANTED lock on \"%s\" (expires: %s)%s\n",
				types.ColorGreen, serverName, r.Resource,
				r.ExpiresAt.Format("15:04:05"), types.ColorReset)
		} else {
			fmt.Printf("  %s❌ %s: DENIED lock on \"%s\" — Reason: %s%s\n",
				types.ColorRed, serverName, r.Resource, r.Reason, types.ColorReset)
		}
	}
	fmt.Println()

	fmt.Println(types.ColorYellow + "━━━ STEP 4: Winner Releases Lock, Loser Retries ━━━" + types.ColorReset)
	fmt.Println()

	winner := "Server_A"
	loser := "Server_B"
	if results[1].Granted {
		winner = "Server_B"
		loser = "Server_A"
	}

	fmt.Printf("  %s releasing lock on \"General_Chat\"...\n", winner)
	leader.LockMgr.Release("General_Chat", winner)
	fmt.Println()

	fmt.Printf("  %s retrying lock on \"General_Chat\"...\n", loser)
	retryResult := leader.AcquireLock("General_Chat", loser)

	fmt.Println()
	if retryResult.Granted {
		fmt.Printf("  %s✅ %s: GRANTED lock on retry! (expires: %s)%s\n",
			types.ColorGreen, loser, retryResult.ExpiresAt.Format("15:04:05"), types.ColorReset)
	} else {
		fmt.Printf("  %s❌ %s: Still denied: %s%s\n",
			types.ColorRed, loser, retryResult.Reason, types.ColorReset)
	}

	fmt.Println()
	fmt.Println(types.ColorYellow + "━━━ STEP 5: Lease Expiry (Deadlock Prevention) ━━━" + types.ColorReset)
	fmt.Println("  If a lock holder crashes without releasing, the lease")
	fmt.Println("  automatically expires after the TTL, preventing deadlocks.")
	fmt.Println()

	leases := leader.LockMgr.GetAllLeases()
	for name, lease := range leases {
		fmt.Printf("  Active lease: \"%s\" held by [%s], expires at %s\n",
			name, lease.Holder, lease.ExpiresAt.Format("15:04:05"))
	}

	fmt.Println()
	fmt.Println(types.ColorPurple + "━━━ DEMO COMPLETE ━━━" + types.ColorReset)
	fmt.Println("Key takeaways:")
	fmt.Println("  1. Only ONE server acquired the lock (mutual exclusion)")
	fmt.Println("  2. The second server was rejected immediately")
	fmt.Println("  3. After release, the lock becomes available again")
	fmt.Println("  4. Leases have TTL to prevent deadlocks from crashed holders")
	fmt.Println()

	for _, n := range nodes {
		n.Stop()
	}
}
