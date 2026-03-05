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
	fmt.Println("║       DEMO: MUTUAL EXCLUSION (Token Ring Algorithm)        ║")
	fmt.Println("║       Nodes Compete for Critical Section Access            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

	// ━━━ STEP 1 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 1: Creating 3 Coordinator Nodes in a Token Ring ━━━" + types.ColorReset)
	fmt.Println("  Ring topology: Node 0 → Node 1 → Node 2 → Node 0")
	fmt.Println("  Token initially held by: Node 0 (lowest ID)")
	fmt.Println()

	node0 := coordinator.NewCoordinatorNode(0, []int{1, 2})
	node1 := coordinator.NewCoordinatorNode(1, []int{0, 2})
	node2 := coordinator.NewCoordinatorNode(2, []int{0, 1})

	nodes := []*coordinator.CoordinatorNode{node0, node1, node2}
	coordinator.SetupInProcessComm(nodes)

	for _, n := range nodes {
		n.Start()
	}

	fmt.Println()
	fmt.Println(types.ColorYellow + "⏳ Waiting for election and token ring to initialize..." + types.ColorReset)
	time.Sleep(2 * time.Second)
	fmt.Println()

	// ━━━ STEP 2 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 2: Node 0 Requests Critical Section for \"General_Chat\" ━━━" + types.ColorReset)
	fmt.Println("  Node 0 holds the token, so access should be granted immediately.")
	fmt.Println()

	resp0 := node0.AcquireLock("General_Chat", "Server_A")
	fmt.Println()

	if resp0.Granted {
		fmt.Printf("  %s✅ Server_A (via Node 0): GRANTED — %s%s\n",
			types.ColorGreen, resp0.Reason, types.ColorReset)
	} else {
		fmt.Printf("  %s❌ Server_A (via Node 0): DENIED — %s%s\n",
			types.ColorRed, resp0.Reason, types.ColorReset)
	}
	fmt.Println()

	// ━━━ STEP 3 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 3: Node 1 Requests Critical Section (Must Wait for Token) ━━━" + types.ColorReset)
	fmt.Println("  Node 1 does NOT have the token. It must wait until the token arrives.")
	fmt.Println()

	var wg sync.WaitGroup
	var resp1 types.LockResponse

	wg.Add(1)
	go func() {
		defer wg.Done()
		resp1 = node1.AcquireLock("General_Chat", "Server_B")
	}()

	// Give a moment for the request to be queued and displayed
	time.Sleep(300 * time.Millisecond)
	fmt.Println()

	// ━━━ STEP 4 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 4: Node 0 Releases → Token Passes to Node 1 ━━━" + types.ColorReset)
	fmt.Println("  When Node 0 exits the critical section, the token is passed to")
	fmt.Println("  the next node in the ring (Node 1). Since Node 1 is waiting,")
	fmt.Println("  it immediately enters the critical section.")
	fmt.Println()

	node0.ReleaseLock("General_Chat", "Server_A")

	// Wait for Node 1 to receive token and get access
	wg.Wait()
	fmt.Println()

	if resp1.Granted {
		fmt.Printf("  %s✅ Server_B (via Node 1): GRANTED — %s%s\n",
			types.ColorGreen, resp1.Reason, types.ColorReset)
	}
	fmt.Println()

	// ━━━ STEP 5 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 5: Node 1 Releases → Token Continues Around Ring ━━━" + types.ColorReset)
	fmt.Println("  After release, the token passes to Node 2, then back to Node 0.")
	fmt.Println()

	node1.ReleaseLock("General_Chat", "Server_B")

	time.Sleep(600 * time.Millisecond)
	fmt.Println()

	// ━━━ STEP 6 ━━━
	fmt.Println(types.ColorYellow + "━━━ STEP 6: Token Ring Status ━━━" + types.ColorReset)
	for _, n := range nodes {
		hasToken, inCS, resource, holder := n.TokenRing.GetStatus()
		status := "waiting for token"
		if hasToken && inCS {
			status = fmt.Sprintf("IN CRITICAL SECTION (resource: %s, holder: %s)", resource, holder)
		} else if hasToken {
			status = "HOLDING TOKEN (idle)"
		}
		fmt.Printf("  Node %d: %s\n", n.ID, status)
	}

	fmt.Println()
	fmt.Println(types.ColorPurple + "━━━ DEMO COMPLETE ━━━" + types.ColorReset)
	fmt.Println("Key takeaways (Token Ring Algorithm):")
	fmt.Println("  1. A single token circulates: Node 0 → Node 1 → Node 2 → Node 0")
	fmt.Println("  2. Only the token holder can enter the critical section (mutual exclusion)")
	fmt.Println("  3. After exiting, the token is passed to the next node in the ring")
	fmt.Println("  4. Waiting nodes are granted access when the token arrives")
	fmt.Println("  5. Fairness: every node eventually receives the token (starvation-free)")
	fmt.Println()

	for _, n := range nodes {
		n.Stop()
	}
}
