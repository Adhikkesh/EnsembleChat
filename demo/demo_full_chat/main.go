package main

import (
	"fmt"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/chatserver"
	"distributed-chat-coordinator/internal/client"
	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  END-TO-END DEMO: Distributed Chat System                              ║
// ║                                                                        ║
// ║  This demo shows all 3 distributed algorithms working together         ║
// ║  through a realistic chat application scenario:                        ║
// ║                                                                        ║
// ║    1. Raft Leader Election    — coordinator nodes elect a leader        ║
// ║    2. Raft Log Replication    — chat rooms & servers replicated         ║
// ║    3. Token Ring Mutex        — exclusive access for room operations    ║
// ║                                                                        ║
// ║  Scenario:                                                             ║
// ║    • 3 coordinator nodes form a cluster (Raft)                         ║
// ║    • Chat servers register with the cluster (consensus)                ║
// ║    • Chat rooms are created (consensus + mutual exclusion)             ║
// ║    • Users join rooms and send messages                                ║
// ║    • Leader crashes → new leader elected → system continues            ║
// ╚══════════════════════════════════════════════════════════════════════════╝

func main() {
	printBanner()

	// ====================================================================
	// PHASE 1: CLUSTER SETUP & LEADER ELECTION (Raft Leader Election)
	// ====================================================================
	phase("PHASE 1", "CLUSTER SETUP — Raft Leader Election",
		"3 coordinator nodes start up. Each begins as a FOLLOWER.\n"+
			"  After a random timeout (300-500ms), a node starts an election,\n"+
			"  requests votes from peers, and becomes LEADER with majority votes.\n"+
			"  The leader then sends periodic heartbeats to maintain authority.")

	node0 := coordinator.NewCoordinatorNode(0, []int{1, 2})
	node1 := coordinator.NewCoordinatorNode(1, []int{0, 2})
	node2 := coordinator.NewCoordinatorNode(2, []int{0, 1})

	nodes := []*coordinator.CoordinatorNode{node0, node1, node2}
	coordinator.SetupInProcessComm(nodes)

	for _, n := range nodes {
		n.Start()
	}

	fmt.Println()
	info("Waiting for Raft leader election...")
	time.Sleep(2500 * time.Millisecond)

	leader := findLeader(nodes)
	if leader == nil {
		fail("No leader elected. Exiting.")
		return
	}

	fmt.Println()
	success(fmt.Sprintf("LEADER ELECTED: Node %d  (Term 1, Majority = 2/3 votes)", leader.ID))
	fmt.Println()
	for _, n := range nodes {
		state, term, lid := n.Election.GetState()
		role := "📥 Follower"
		if n.Election.IsLeader() {
			role = "👑 LEADER"
		}
		fmt.Printf("    Node %d:  %s  |  State=%s  Term=%d  LeaderID=%d\n",
			n.ID, role, state, term, lid)
	}
	divider()

	// ====================================================================
	// PHASE 2: REGISTER CHAT SERVERS (Raft Log Replication — Consensus)
	// ====================================================================
	phase("PHASE 2", "REGISTER CHAT SERVERS — Raft Log Replication",
		"The leader proposes new log entries (ADD_SERVER) via Raft consensus.\n"+
			"  Each entry is appended to the leader's log, then sent to followers\n"+
			"  via AppendEntries RPC. Once a majority (2/3) replicates the entry,\n"+
			"  it is COMMITTED and applied to the state machine (routing table).")

	// Create in-memory chat servers
	serverAlpha := chatserver.NewChatServer("Alpha", "192.168.1.10:9001")
	serverBeta := chatserver.NewChatServer("Beta", "192.168.1.20:9002")

	info("Registering Server_Alpha (192.168.1.10:9001) via Raft consensus...")
	fmt.Println()
	ok1 := leader.ProposeChange("tx-1", types.ChangeAddServer, "Server_Alpha", "192.168.1.10:9001")
	printResult(ok1, "Server_Alpha registered on all nodes")

	info("Registering Server_Beta (192.168.1.20:9002) via Raft consensus...")
	fmt.Println()
	ok2 := leader.ProposeChange("tx-2", types.ChangeAddServer, "Server_Beta", "192.168.1.20:9002")
	printResult(ok2, "Server_Beta registered on all nodes")

	time.Sleep(300 * time.Millisecond)

	// Show replicated routing table
	fmt.Println()
	info("Routing Table replicated across all 3 nodes:")
	for _, n := range nodes {
		rt := n.RaftLog.GetRoutingTable()
		role := "Follower"
		if n.Election.IsLeader() {
			role = "Leader"
		}
		fmt.Printf("    [%s Node %d] Servers: ", role, n.ID)
		for name, addr := range rt.Servers {
			fmt.Printf("%s→%s  ", name, addr)
		}
		fmt.Println()
	}
	divider()

	// ====================================================================
	// PHASE 3: CREATE CHAT ROOMS (Consensus + Token Ring Mutual Exclusion)
	// ====================================================================
	phase("PHASE 3", "CREATE CHAT ROOMS — Consensus + Token Ring Mutual Exclusion",
		"Before creating a room, the coordinator acquires the TOKEN (Token Ring)\n"+
			"  to ensure exclusive access, then proposes the room via Raft consensus.\n"+
			"  The token circulates: Node 0 → 1 → 2 → 0. Only the token holder\n"+
			"  can enter the critical section.")

	// --- Create room "General_Chat" ---
	info("Node " + fmt.Sprint(leader.ID) + " requests TOKEN to create room \"General_Chat\"...")
	fmt.Println()

	resp := leader.AcquireLock("room_creation", "Coordinator")
	if resp.Granted {
		success("TOKEN ACQUIRED — entered critical section for room creation")

		info("Proposing \"General_Chat\" on Server_Alpha via Raft log replication...")
		fmt.Println()
		ok3 := leader.ProposeChange("tx-3", types.ChangeAddRoom, "General_Chat", "Server_Alpha:9001")
		printResult(ok3, "\"General_Chat\" replicated to all nodes")

		if ok3 {
			serverAlpha.CreateRoom("General_Chat")
		}

		leader.ReleaseLock("room_creation", "Coordinator")
		success("TOKEN RELEASED — passed to next node in ring")
	}
	fmt.Println()

	// --- Create room "Gaming_Lounge" ---
	info("Node " + fmt.Sprint(leader.ID) + " requests TOKEN to create room \"Gaming_Lounge\"...")
	fmt.Println()

	resp2 := leader.AcquireLock("room_creation", "Coordinator")
	if resp2.Granted {
		success("TOKEN ACQUIRED — entered critical section")

		info("Proposing \"Gaming_Lounge\" on Server_Beta via Raft log replication...")
		fmt.Println()
		ok4 := leader.ProposeChange("tx-4", types.ChangeAddRoom, "Gaming_Lounge", "Server_Beta:9002")
		printResult(ok4, "\"Gaming_Lounge\" replicated to all nodes")

		if ok4 {
			serverBeta.CreateRoom("Gaming_Lounge")
		}

		leader.ReleaseLock("room_creation", "Coordinator")
		success("TOKEN RELEASED — passed to next node in ring")
	}

	time.Sleep(400 * time.Millisecond)

	// Show full routing table
	fmt.Println()
	info("Full Routing Table (consistent across all nodes):")
	for _, n := range nodes {
		rt := n.RaftLog.GetRoutingTable()
		role := "Follower"
		if n.Election.IsLeader() {
			role = "Leader"
		}
		fmt.Printf("    [%s Node %d]\n", role, n.ID)
		for name, entry := range rt.Rooms {
			fmt.Printf("       🏠 Room \"%s\" → %s\n", name, entry.ServerAddr)
		}
		for name, addr := range rt.Servers {
			fmt.Printf("       🖥️  Server \"%s\" → %s\n", name, addr)
		}
	}
	divider()

	// ====================================================================
	// PHASE 4: USERS JOIN ROOMS & SEND MESSAGES
	// ====================================================================
	phase("PHASE 4", "USERS JOIN ROOMS & SEND MESSAGES",
		"Chat clients connect to servers, join rooms, and exchange messages.\n"+
			"  Room membership changes use Token Ring for mutual exclusion to\n"+
			"  prevent concurrent joins from corrupting the member list.")

	alice := client.NewChatClient("Alice")
	bob := client.NewChatClient("Bob")
	charlie := client.NewChatClient("Charlie")

	// Alice joins General_Chat (with mutex)
	info("Alice joins \"General_Chat\" — acquiring token for exclusive member-list access...")
	lockResp := leader.AcquireLock("member_list_General_Chat", "Alice")
	if lockResp.Granted {
		alice.JoinRoom("General_Chat")
		serverAlpha.ConnectClient("Alice")
		generalRoom := serverAlpha.GetRoom("General_Chat")
		if generalRoom != nil {
			generalRoom.Join("Alice")
		}
		leader.ReleaseLock("member_list_General_Chat", "Alice")
	}

	// Bob joins General_Chat
	info("Bob joins \"General_Chat\" — acquiring token...")
	lockResp2 := leader.AcquireLock("member_list_General_Chat", "Bob")
	if lockResp2.Granted {
		bob.JoinRoom("General_Chat")
		serverAlpha.ConnectClient("Bob")
		generalRoom := serverAlpha.GetRoom("General_Chat")
		if generalRoom != nil {
			generalRoom.Join("Bob")
		}
		leader.ReleaseLock("member_list_General_Chat", "Bob")
	}

	// Charlie joins Gaming_Lounge
	info("Charlie joins \"Gaming_Lounge\" — acquiring token...")
	lockResp3 := leader.AcquireLock("member_list_Gaming_Lounge", "Charlie")
	if lockResp3.Granted {
		charlie.JoinRoom("Gaming_Lounge")
		serverBeta.ConnectClient("Charlie")
		gamingRoom := serverBeta.GetRoom("Gaming_Lounge")
		if gamingRoom != nil {
			gamingRoom.Join("Charlie")
		}
		leader.ReleaseLock("member_list_Gaming_Lounge", "Charlie")
	}

	fmt.Println()
	info("Users send messages:")
	fmt.Println()

	alice.SendMessage("General_Chat", "Hey everyone! Welcome to the General Chat 👋")
	serverAlpha.SendMessage("General_Chat", "Alice", "Hey everyone! Welcome to the General Chat 👋")

	bob.SendMessage("General_Chat", "Hi Alice! Glad to be here 🎉")
	serverAlpha.SendMessage("General_Chat", "Bob", "Hi Alice! Glad to be here 🎉")

	charlie.SendMessage("Gaming_Lounge", "Anyone up for a game? 🎮")
	serverBeta.SendMessage("Gaming_Lounge", "Charlie", "Anyone up for a game? 🎮")

	// Bob also receives Alice's message
	bob.ReceiveMessage("General_Chat", "Alice", "Hey everyone! Welcome to the General Chat 👋")

	fmt.Println()
	info("Chat messages stored on servers:")
	fmt.Printf("    Server_Alpha / General_Chat: %d messages\n",
		len(serverAlpha.GetRoom("General_Chat").GetMessages()))
	fmt.Printf("    Server_Beta / Gaming_Lounge: %d messages\n",
		len(serverBeta.GetRoom("Gaming_Lounge").GetMessages()))
	divider()

	// ====================================================================
	// PHASE 5: LEADER FAILURE & RE-ELECTION (Raft Leader Election)
	// ====================================================================
	phase("PHASE 5", "LEADER CRASHES — Raft Leader Re-Election",
		"The current leader node crashes (simulated). Followers detect the\n"+
			"  missing heartbeats (timeout 300-500ms), start a new election, and\n"+
			"  elect a new leader with a higher term number. The system recovers\n"+
			"  automatically without any manual intervention.")

	oldLeaderID := leader.ID
	fmt.Printf("    %s💀 KILLING Leader Node %d...%s\n\n",
		types.ColorRed+types.ColorBold, oldLeaderID, types.ColorReset)
	leader.Kill()

	info("Waiting for followers to detect failure and re-elect...")
	time.Sleep(3 * time.Second)

	var newLeader *coordinator.CoordinatorNode
	for _, n := range nodes {
		if n.IsAlive() && n.Election.IsLeader() {
			newLeader = n
			break
		}
	}
	if newLeader == nil {
		time.Sleep(2 * time.Second)
		for _, n := range nodes {
			if n.IsAlive() && n.Election.IsLeader() {
				newLeader = n
				break
			}
		}
	}

	fmt.Println()
	for _, n := range nodes {
		if !n.IsAlive() {
			fmt.Printf("    Node %d:  💀 DEAD (was the old leader)\n", n.ID)
			continue
		}
		state, term, lid := n.Election.GetState()
		role := "📥 Follower"
		if n.Election.IsLeader() {
			role = "👑 NEW LEADER"
		}
		fmt.Printf("    Node %d:  %s  |  State=%s  Term=%d  LeaderID=%d\n",
			n.ID, role, state, term, lid)
	}
	fmt.Println()

	if newLeader != nil {
		success(fmt.Sprintf("NEW LEADER ELECTED: Node %d  (higher term, automatic failover!)", newLeader.ID))
	}
	divider()

	// ====================================================================
	// PHASE 6: SYSTEM CONTINUES WITH NEW LEADER
	// ====================================================================
	phase("PHASE 6", "SYSTEM CONTINUES — New Leader Handles Operations",
		"The newly elected leader can continue all operations: create rooms,\n"+
			"  register servers, and coordinate chat — all with the same\n"+
			"  Raft consensus and Token Ring mutual exclusion.")

	if newLeader != nil {
		info(fmt.Sprintf("New leader (Node %d) creates room \"Study_Group\"...", newLeader.ID))

		lockResp4 := newLeader.AcquireLock("room_creation", "Coordinator")
		if lockResp4.Granted {
			info("Token acquired. Proposing via Raft consensus...")
			fmt.Println()
			ok5 := newLeader.ProposeChange("tx-5", types.ChangeAddRoom, "Study_Group", "Server_Alpha:9001")
			printResult(ok5, "\"Study_Group\" replicated to surviving nodes")

			if ok5 {
				serverAlpha.CreateRoom("Study_Group")
			}
			newLeader.ReleaseLock("room_creation", "Coordinator")
		}

		// Alice sends message via new leader
		fmt.Println()
		alice.SendMessage("General_Chat", "The system recovered! New leader is handling things 🚀")
		serverAlpha.SendMessage("General_Chat", "Alice", "The system recovered! New leader is handling things 🚀")

		fmt.Println()
		info("Routing table on surviving nodes:")
		for _, n := range nodes {
			if !n.IsAlive() {
				continue
			}
			rt := n.RaftLog.GetRoutingTable()
			role := "Follower"
			if n.Election.IsLeader() {
				role = "Leader"
			}
			fmt.Printf("    [%s Node %d] Rooms: ", role, n.ID)
			for name := range rt.Rooms {
				fmt.Printf("\"%s\" ", name)
			}
			fmt.Printf("| Servers: ")
			for name := range rt.Servers {
				fmt.Printf("\"%s\" ", name)
			}
			fmt.Println()
		}
	}
	divider()

	// ====================================================================
	// PHASE 7: CONCURRENT ROOM ACCESS (Token Ring Mutual Exclusion)
	// ====================================================================
	phase("PHASE 7", "CONCURRENT ACCESS — Token Ring Prevents Conflicts",
		"Multiple users try to join the same room at the same time.\n"+
			"  The Token Ring algorithm ensures only ONE node/operation can\n"+
			"  modify the member list at a time. Others wait for the token.")

	if newLeader != nil {
		var wg sync.WaitGroup
		wg.Add(2)

		fmt.Println()

		// Alice and Bob both try to join Study_Group concurrently
		go func() {
			defer wg.Done()
			info("Alice tries to join \"Study_Group\" (needs token)...")
			lr := newLeader.AcquireLock("member_list_Study_Group", "Alice")
			if lr.Granted {
				alice.JoinRoom("Study_Group")
				generalRoom := serverAlpha.GetRoom("Study_Group")
				if generalRoom != nil {
					generalRoom.Join("Alice")
				}
				time.Sleep(200 * time.Millisecond) // simulate some work in CS
				newLeader.ReleaseLock("member_list_Study_Group", "Alice")
				success("Alice joined \"Study_Group\" and released token")
			}
		}()

		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond) // slight delay
			info("Bob tries to join \"Study_Group\" (needs token — must wait)...")
			lr := newLeader.AcquireLock("member_list_Study_Group", "Bob")
			if lr.Granted {
				bob.JoinRoom("Study_Group")
				generalRoom := serverAlpha.GetRoom("Study_Group")
				if generalRoom != nil {
					generalRoom.Join("Bob")
				}
				newLeader.ReleaseLock("member_list_Study_Group", "Bob")
				success("Bob joined \"Study_Group\" and released token")
			}
		}()

		wg.Wait()
	}

	// ====================================================================
	// SUMMARY
	// ====================================================================
	fmt.Println()
	fmt.Println(types.ColorBold + types.ColorCyan)
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        DEMO COMPLETE — SUMMARY                          ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  ALGORITHM 1: Raft Leader Election                                       ║")
	fmt.Println("║     ✅ 3 nodes started as followers                                      ║")
	fmt.Println("║     ✅ Leader elected via majority vote (2/3)                             ║")
	fmt.Println("║     ✅ Leader crashed → detected via heartbeat timeout                    ║")
	fmt.Println("║     ✅ New leader elected automatically with higher term                  ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  ALGORITHM 2: Raft Log Replication (Consensus)                           ║")
	fmt.Println("║     ✅ Server registrations replicated across all nodes                   ║")
	fmt.Println("║     ✅ Chat room metadata replicated via AppendEntries RPC                ║")
	fmt.Println("║     ✅ Majority quorum required for commit (2/3 nodes)                    ║")
	fmt.Println("║     ✅ All nodes converge to identical routing table                      ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  ALGORITHM 3: Token Ring Mutual Exclusion                                ║")
	fmt.Println("║     ✅ Token circulates: Node 0 → Node 1 → Node 2 → Node 0               ║")
	fmt.Println("║     ✅ Room creation requires token (exclusive access)                    ║")
	fmt.Println("║     ✅ Room joins serialized via token (no concurrent corruption)          ║")
	fmt.Println("║     ✅ Concurrent access handled — one waits, one proceeds                ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  USE CASES DEMONSTRATED:                                                 ║")
	fmt.Println("║     1. Register chat servers in the distributed cluster                   ║")
	fmt.Println("║     2. Create chat rooms (replicated to all nodes)                        ║")
	fmt.Println("║     3. Users join rooms & send messages                                   ║")
	fmt.Println("║     4. Leader failure and automatic recovery                              ║")
	fmt.Println("║     5. Concurrent room access with mutual exclusion                       ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

	// Cleanup
	for _, n := range nodes {
		if n.IsAlive() {
			n.Stop()
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func printBanner() {
	fmt.Println(types.ColorBold + types.ColorGreen)
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          DISTRIBUTED CHAT COORDINATOR — END-TO-END DEMO                 ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║   Algorithms:  Raft Leader Election  |  Token Ring  |  Raft Log Repl.    ║")
	fmt.Println("║   Scenario:    Chat Server Registration → Room Creation → Messaging      ║")
	fmt.Println("║                → Leader Failure & Recovery → Concurrent Access            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()
}

func phase(tag, title, description string) {
	fmt.Printf("\n%s%s", types.ColorBold+types.ColorYellow, "")
	fmt.Printf("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓\n")
	fmt.Printf("┃  %s: %s\n", tag, title)
	fmt.Printf("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛%s\n", types.ColorReset)
	fmt.Printf("  %s%s%s\n\n", types.ColorWhite, description, types.ColorReset)
}

func info(msg string) {
	fmt.Printf("  %s⏳ %s%s\n", types.ColorCyan, msg, types.ColorReset)
}

func success(msg string) {
	fmt.Printf("  %s✅ %s%s\n", types.ColorGreen+types.ColorBold, msg, types.ColorReset)
}

func fail(msg string) {
	fmt.Printf("  %s❌ %s%s\n", types.ColorRed+types.ColorBold, msg, types.ColorReset)
}

func printResult(ok bool, msg string) {
	fmt.Println()
	if ok {
		success(msg)
	} else {
		fail("FAILED: " + msg)
	}
	fmt.Println()
}

func divider() {
	fmt.Printf("\n%s────────────────────────────────────────────────────────────────────────────%s\n",
		types.ColorYellow, types.ColorReset)
}

func findLeader(nodes []*coordinator.CoordinatorNode) *coordinator.CoordinatorNode {
	for _, n := range nodes {
		if n.Election.IsLeader() {
			return n
		}
	}
	time.Sleep(3 * time.Second)
	for _, n := range nodes {
		if n.Election.IsLeader() {
			return n
		}
	}
	return nil
}
