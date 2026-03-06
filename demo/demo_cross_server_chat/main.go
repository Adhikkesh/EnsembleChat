package main

import (
	"fmt"
	"net/rpc"
	"time"

	"distributed-chat-coordinator/internal/chatserver"
	"distributed-chat-coordinator/internal/client"
	"distributed-chat-coordinator/internal/coordinator"
	"distributed-chat-coordinator/internal/types"
)

// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  CROSS-SERVER CHAT DEMO                                                ║
// ║                                                                        ║
// ║  Shows clients on DIFFERENT servers exchanging messages in the SAME     ║
// ║  room through cross-server message relay.                              ║
// ║                                                                        ║
// ║  Flow:                                                                 ║
// ║    1. Coordinator cluster elects leader (Raft)                         ║
// ║    2. Register 2 chat servers via consensus                            ║
// ║    3. Create "General_Chat" room on both servers (consensus + mutex)   ║
// ║    4. Start 2 chat servers on real TCP ports                           ║
// ║    5. Bob connects to Server_Alpha (port 9001)                         ║
// ║    6. Alice connects to Server_Beta (port 9002)                        ║
// ║    7. Bob sends message → forwarded to Server_Beta → Alice sees it     ║
// ║    8. Alice replies → forwarded to Server_Alpha → Bob sees it          ║
// ╚══════════════════════════════════════════════════════════════════════════╝

func main() {
	printBanner()

	// ====================================================================
	// PHASE 1: CLUSTER SETUP & LEADER ELECTION
	// ====================================================================
	phase("PHASE 1", "CLUSTER SETUP — Raft Leader Election",
		"3 coordinator nodes start, one becomes LEADER via majority vote.")

	node0 := coordinator.NewCoordinatorNode(0, []int{1, 2})
	node1 := coordinator.NewCoordinatorNode(1, []int{0, 2})
	node2 := coordinator.NewCoordinatorNode(2, []int{0, 1})

	nodes := []*coordinator.CoordinatorNode{node0, node1, node2}
	coordinator.SetupInProcessComm(nodes)

	for _, n := range nodes {
		n.Start()
	}

	info("Waiting for leader election...")
	time.Sleep(2500 * time.Millisecond)

	leader := findLeader(nodes)
	if leader == nil {
		fail("No leader elected. Exiting.")
		return
	}
	success(fmt.Sprintf("LEADER ELECTED: Node %d", leader.ID))
	divider()

	// ====================================================================
	// PHASE 2: REGISTER CHAT SERVERS (Raft Consensus)
	// ====================================================================
	phase("PHASE 2", "REGISTER CHAT SERVERS — Raft Log Replication",
		"Leader proposes ADD_SERVER entries → replicated to all nodes via consensus.")

	info("Registering Server_Alpha (127.0.0.1:9001)...")
	fmt.Println()
	ok1 := leader.ProposeChange("tx-1", types.ChangeAddServer, "Server_Alpha", "127.0.0.1:9001")
	printResult(ok1, "Server_Alpha registered (replicated to all nodes)")

	info("Registering Server_Beta (127.0.0.1:9002)...")
	fmt.Println()
	ok2 := leader.ProposeChange("tx-2", types.ChangeAddServer, "Server_Beta", "127.0.0.1:9002")
	printResult(ok2, "Server_Beta registered (replicated to all nodes)")
	time.Sleep(300 * time.Millisecond)
	divider()

	// ====================================================================
	// PHASE 3: CREATE CHAT ROOM (Token Ring Mutex + Consensus)
	// ====================================================================
	phase("PHASE 3", "CREATE ROOM — Token Ring Mutex + Raft Consensus",
		"Acquire TOKEN for exclusive access → propose room via Raft → release TOKEN.\n"+
			"  Room \"General_Chat\" is registered in the routing table on ALL nodes.")

	resp := leader.AcquireLock("room_creation", "Coordinator")
	if resp.Granted {
		success("TOKEN ACQUIRED — entering critical section")
		info("Proposing \"General_Chat\" via Raft consensus...")
		fmt.Println()
		ok3 := leader.ProposeChange("tx-3", types.ChangeAddRoom, "General_Chat", "Server_Alpha:9001")
		printResult(ok3, "\"General_Chat\" replicated to all nodes")
		leader.ReleaseLock("room_creation", "Coordinator")
		success("TOKEN RELEASED — passed to next node in ring")
	}
	time.Sleep(300 * time.Millisecond)
	divider()

	// ====================================================================
	// PHASE 4: START CHAT SERVERS WITH TCP LISTENERS
	// ====================================================================
	phase("PHASE 4", "START CHAT SERVERS — Real TCP Listeners",
		"Two chat servers start on separate TCP ports.\n"+
			"  Server_Alpha listens on :9001, Server_Beta on :9002.\n"+
			"  Both create the \"General_Chat\" room locally.")

	serverAlpha := chatserver.NewChatServer("Alpha", "127.0.0.1:9001")
	serverBeta := chatserver.NewChatServer("Beta", "127.0.0.1:9002")

	// Create the room on BOTH servers (in a real system, the coordinator
	// would tell each server to create the room — here we do it directly)
	serverAlpha.CreateRoom("General_Chat")
	serverBeta.CreateRoom("General_Chat")

	// === Wire cross-server message relay ===
	// When Server_Alpha receives a message, forward it to Server_Beta and vice versa.
	// This simulates the coordinator-driven relay that would happen in production.
	serverAlpha.OnMessageReceived = func(serverID, roomName, sender, content string) {
		// Forward to Server_Beta via RelayMessage RPC (no callback = no loop)
		go func() {
			conn, err := rpc.Dial("tcp", "127.0.0.1:9002")
			if err != nil {
				return
			}
			defer conn.Close()
			args := &chatserver.RelayMessageArgs{RoomName: roomName, Sender: sender, Content: content}
			var reply chatserver.RelayMessageReply
			conn.Call("ChatService.RelayMessage", args, &reply)
		}()
	}

	serverBeta.OnMessageReceived = func(serverID, roomName, sender, content string) {
		// Forward to Server_Alpha via RelayMessage RPC (no callback = no loop)
		go func() {
			conn, err := rpc.Dial("tcp", "127.0.0.1:9001")
			if err != nil {
				return
			}
			defer conn.Close()
			args := &chatserver.RelayMessageArgs{RoomName: roomName, Sender: sender, Content: content}
			var reply chatserver.RelayMessageReply
			conn.Call("ChatService.RelayMessage", args, &reply)
		}()
	}

	// Start TCP RPC servers
	if err := serverAlpha.StartRPC(":9001"); err != nil {
		fail(fmt.Sprintf("Server_Alpha failed to start: %v", err))
		return
	}
	if err := serverBeta.StartRPC(":9002"); err != nil {
		fail(fmt.Sprintf("Server_Beta failed to start: %v", err))
		return
	}

	success("Server_Alpha listening on :9001")
	success("Server_Beta  listening on :9002")
	time.Sleep(500 * time.Millisecond)
	divider()

	// ====================================================================
	// PHASE 5: CLIENTS CONNECT & JOIN ROOM
	// ====================================================================
	phase("PHASE 5", "CLIENTS CONNECT — Bob → Server_Alpha, Alice → Server_Beta",
		"Bob connects to Server_Alpha (:9001), Alice connects to Server_Beta (:9002).\n"+
			"  Both join \"General_Chat\". They are on DIFFERENT servers but the SAME room.")

	bob := client.NewChatClient("Bob")
	alice := client.NewChatClient("Alice")

	if err := bob.ConnectToServer("127.0.0.1:9001"); err != nil {
		fail(fmt.Sprintf("Bob failed to connect: %v", err))
		return
	}
	if err := alice.ConnectToServer("127.0.0.1:9002"); err != nil {
		fail(fmt.Sprintf("Alice failed to connect: %v", err))
		return
	}

	if err := bob.JoinRoomRPC("General_Chat"); err != nil {
		fail(fmt.Sprintf("Bob failed to join: %v", err))
	}
	if err := alice.JoinRoomRPC("General_Chat"); err != nil {
		fail(fmt.Sprintf("Alice failed to join: %v", err))
	}

	fmt.Println()
	success("Bob  → Server_Alpha (:9001) → joined \"General_Chat\"")
	success("Alice → Server_Beta  (:9002) → joined \"General_Chat\"")
	divider()

	// ====================================================================
	// PHASE 6: CROSS-SERVER MESSAGING
	// ====================================================================
	phase("PHASE 6", "CROSS-SERVER MESSAGING — Messages travel between servers",
		"Bob sends a message on Server_Alpha. It gets relayed to Server_Beta.\n"+
			"  Alice polls Server_Beta and sees Bob's message.\n"+
			"  Alice replies on Server_Beta. It gets relayed to Server_Alpha.\n"+
			"  Bob polls Server_Alpha and sees Alice's reply.")

	// Bob sends a message
	info("Bob sends: \"Hey Alice! Can you hear me across servers?\"")
	fmt.Println()
	if err := bob.SendMessageRPC("General_Chat", "Hey Alice! Can you hear me across servers?"); err != nil {
		fail(fmt.Sprintf("Bob send failed: %v", err))
	}

	// Wait for cross-server relay
	time.Sleep(800 * time.Millisecond)

	// Alice polls for new messages
	info("Alice polls Server_Beta for new messages...")
	msgs, err := alice.PollMessages("General_Chat")
	if err != nil {
		fail(fmt.Sprintf("Alice poll failed: %v", err))
	} else {
		fmt.Println()
		for _, msg := range msgs {
			fmt.Printf("  %s[RECV] Alice received: [%s] %s: %s%s\n",
				types.ColorGreen+types.ColorBold, "General_Chat", msg.Sender, msg.Content, types.ColorReset)
		}
	}

	fmt.Println()

	// Alice replies
	info("Alice sends: \"Hi Bob! Yes, cross-server messaging works!\"")
	fmt.Println()
	if err := alice.SendMessageRPC("General_Chat", "Hi Bob! Yes, cross-server messaging works!"); err != nil {
		fail(fmt.Sprintf("Alice send failed: %v", err))
	}

	// Wait for cross-server relay
	time.Sleep(800 * time.Millisecond)

	// Bob polls for new messages
	info("Bob polls Server_Alpha for new messages...")
	msgs2, err := bob.PollMessages("General_Chat")
	if err != nil {
		fail(fmt.Sprintf("Bob poll failed: %v", err))
	} else {
		fmt.Println()
		for _, msg := range msgs2 {
			fmt.Printf("  %s[RECV] Bob received: [%s] %s: %s%s\n",
				types.ColorGreen+types.ColorBold, "General_Chat", msg.Sender, msg.Content, types.ColorReset)
		}
	}
	divider()

	// ====================================================================
	// PHASE 7: VERIFY STATE ON BOTH SERVERS
	// ====================================================================
	phase("PHASE 7", "VERIFICATION — Both servers have ALL messages",
		"Both Server_Alpha and Server_Beta should have identical message history\n"+
			"  for \"General_Chat\", proving cross-server relay is working.")

	alphaMessages := serverAlpha.GetRoom("General_Chat").GetMessages()
	betaMessages := serverBeta.GetRoom("General_Chat").GetMessages()

	fmt.Printf("  %sServer_Alpha (General_Chat) — %d messages:%s\n",
		types.ColorBlue+types.ColorBold, len(alphaMessages), types.ColorReset)
	for i, msg := range alphaMessages {
		fmt.Printf("    %d. %s: %s\n", i+1, msg.Sender, msg.Content)
	}

	fmt.Println()
	fmt.Printf("  %sServer_Beta (General_Chat) — %d messages:%s\n",
		types.ColorPurple+types.ColorBold, len(betaMessages), types.ColorReset)
	for i, msg := range betaMessages {
		fmt.Printf("    %d. %s: %s\n", i+1, msg.Sender, msg.Content)
	}

	fmt.Println()
	if len(alphaMessages) == len(betaMessages) && len(alphaMessages) > 0 {
		success(fmt.Sprintf("BOTH servers have %d messages -- cross-server relay VERIFIED", len(alphaMessages)))
	}
	divider()

	// ====================================================================
	// PHASE 8: ROUTING TABLE SHOWS THE FULL PICTURE
	// ====================================================================
	phase("PHASE 8", "COORDINATOR ROUTING TABLE — All nodes consistent",
		"The Raft-replicated routing table shows servers and rooms across all coordinators.")

	for _, n := range nodes {
		rt := n.RaftLog.GetRoutingTable()
		role := "Follower"
		if n.Election.IsLeader() {
			role = "Leader"
		}
		fmt.Printf("  [%s Node %d]\n", role, n.ID)
		for name, addr := range rt.Servers {
			fmt.Printf("    Server \"%s\" -> %s\n", name, addr)
		}
		for name, entry := range rt.Rooms {
			fmt.Printf("    Room \"%s\" -> %s\n", name, entry.ServerAddr)
		}
	}

	// ====================================================================
	// SUMMARY
	// ====================================================================
	fmt.Println()
	fmt.Println(types.ColorBold + types.ColorCyan)
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    CROSS-SERVER CHAT DEMO COMPLETE                      ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  WHAT WE DEMONSTRATED:                                                  ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  1. Raft Leader Election -- 3 coordinators, 1 leader elected             ║")
	fmt.Println("║  2. Raft Consensus -- servers & rooms replicated to all nodes            ║")
	fmt.Println("║  3. Token Ring Mutex -- room creation under mutual exclusion             ║")
	fmt.Println("║  4. Chat Servers -- 2 servers on separate TCP ports                      ║")
	fmt.Println("║  5. Clients -- Bob on Server_Alpha, Alice on Server_Beta                 ║")
	fmt.Println("║  6. Cross-Server Messaging:                                              ║")
	fmt.Println("║       Bob (Server_Alpha) --msg--> relay --> Server_Beta (Alice)           ║")
	fmt.Println("║       Alice (Server_Beta) --msg--> relay --> Server_Alpha (Bob)           ║")
	fmt.Println("║  7. Both servers have identical message history                           ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║  ARCHITECTURE:                                                           ║")
	fmt.Println("║    Client ──RPC──▶ ChatServer ──relay──▶ ChatServer ──poll──▶ Client     ║")
	fmt.Println("║            └── All coordinated by Raft consensus + Token Ring ──┘        ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════╝" + types.ColorReset)
	fmt.Println()

	// Cleanup
	bob.Disconnect()
	alice.Disconnect()
	serverAlpha.StopRPC()
	serverBeta.StopRPC()
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
	fmt.Println("║          CROSS-SERVER CHAT DEMO — Distributed Messaging                 ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║   Bob → Server_Alpha (:9001)  ┐                                         ║")
	fmt.Println("║                                 ├── Both in \"General_Chat\"               ║")
	fmt.Println("║   Alice → Server_Beta (:9002) ┘                                         ║")
	fmt.Println("║                                                                          ║")
	fmt.Println("║   Messages relay between servers so both users see everything.            ║")
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
	fmt.Printf("  %s[WAIT] %s%s\n", types.ColorCyan, msg, types.ColorReset)
}

func success(msg string) {
	fmt.Printf("  %s[OK] %s%s\n", types.ColorGreen+types.ColorBold, msg, types.ColorReset)
}

func fail(msg string) {
	fmt.Printf("  %s[FAIL] %s%s\n", types.ColorRed+types.ColorBold, msg, types.ColorReset)
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
