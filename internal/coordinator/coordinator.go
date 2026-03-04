package coordinator

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/consensus"
	"distributed-chat-coordinator/internal/election"
	"distributed-chat-coordinator/internal/lock"
	"distributed-chat-coordinator/internal/transport"
	"distributed-chat-coordinator/internal/types"
)

type CoordinatorNode struct {
	mu sync.Mutex

	ID        int
	Addr      string
	Peers     []*CoordinatorNode
	PeerAddrs map[int]string

	Election   *election.ElectionNode
	LockMgr    *lock.LockManager
	TwoPCCoord *consensus.Coordinator
	TwoPCPart  *consensus.Participant

	listener net.Listener
	prefix   string
	alive    bool
	stopCh   chan struct{}
}

func NewCoordinatorNode(id int, peerIDs []int) *CoordinatorNode {
	node := &CoordinatorNode{
		ID:         id,
		prefix:     types.LogPrefix(id, "COORDINATOR"),
		alive:      true,
		stopCh:     make(chan struct{}),
		PeerAddrs:  make(map[int]string),
		Election:   election.NewElectionNode(id, peerIDs),
		LockMgr:    lock.NewLockManager(id, 10*time.Second),
		TwoPCCoord: consensus.NewCoordinator(id),
		TwoPCPart:  consensus.NewParticipant(id),
	}

	node.Election.OnBecomeLeader = func(term int) {
		fmt.Printf("%s👑 I am now the LEADER (term %d). Starting lock manager.\n",
			node.prefix, term)
		node.LockMgr.Start()
	}

	node.Election.OnBecomeFollower = func(leaderID, term int) {
		fmt.Printf("%s📥 Following Leader Node %d (term %d)\n",
			node.prefix, leaderID, term)
	}

	return node
}

func (n *CoordinatorNode) Start() {
	fmt.Printf("%s🚀 Starting coordinator node...\n", n.prefix)
	n.Election.Start()
}

func (n *CoordinatorNode) Stop() {
	n.mu.Lock()
	n.alive = false
	n.mu.Unlock()

	if n.listener != nil {
		n.listener.Close()
	}

	fmt.Printf("%s🛑 Stopping coordinator node...\n", n.prefix)
	n.Election.Stop()
	n.LockMgr.Stop()
}

// StartTCP opens a TCP listener on n.Addr and handles incoming messages.
func (n *CoordinatorNode) StartTCP() error {
	ln, err := net.Listen("tcp", n.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", n.Addr, err)
	}
	n.listener = ln
	fmt.Printf("%s📡 TCP listener started on %s\n", n.prefix, n.Addr)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-n.stopCh:
					return
				default:
					fmt.Printf("%s⚠️  Accept error: %v\n", n.prefix, err)
					continue
				}
			}
			go n.handleConn(conn)
		}
	}()

	return nil
}

func (n *CoordinatorNode) handleConn(conn net.Conn) {
	defer conn.Close()

	msg, err := transport.Receive(conn)
	if err != nil {
		fmt.Printf("%s⚠️  Receive error: %v\n", n.prefix, err)
		return
	}

	// Re-marshal the generic payload so we can unmarshal into the concrete type.
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		fmt.Printf("%s⚠️  Payload re-marshal error: %v\n", n.prefix, err)
		return
	}

	switch msg.Type {
	case types.MsgVoteRequest:
		var req types.VoteRequest
		json.Unmarshal(payloadBytes, &req)
		n.Election.VoteRequestCh <- req

	case types.MsgVoteResponse:
		var resp types.VoteResponse
		json.Unmarshal(payloadBytes, &resp)
		n.Election.VoteResponseCh <- resp

	case types.MsgHeartbeat:
		var hb types.Heartbeat
		json.Unmarshal(payloadBytes, &hb)
		n.Election.HeartbeatCh <- hb

	case types.MsgPrepareRequest:
		var req types.PrepareRequest
		json.Unmarshal(payloadBytes, &req)
		n.TwoPCPart.HandlePrepare(req)

	case types.MsgPrepareResponse:
		var resp types.PrepareResponse
		json.Unmarshal(payloadBytes, &resp)
		n.TwoPCCoord.PrepareResponseCh <- resp

	case types.MsgCommitRequest:
		var req types.CommitRequest
		json.Unmarshal(payloadBytes, &req)
		n.TwoPCPart.HandleCommit(req)

	case types.MsgAbortRequest:
		var req types.AbortRequest
		json.Unmarshal(payloadBytes, &req)
		n.TwoPCPart.HandleAbort(req)

	default:
		fmt.Printf("%s⚠️  Unknown message type: %s\n", n.prefix, msg.Type)
	}
}

// tcpSend is a helper that connects to a peer and sends a single message.
func (n *CoordinatorNode) tcpSend(peerID int, msg types.Message) {
	addr, ok := n.PeerAddrs[peerID]
	if !ok {
		fmt.Printf("%s⚠️  No address for peer %d\n", n.prefix, peerID)
		return
	}
	conn, err := transport.ConnectWithRetry(addr, 3, 200*time.Millisecond)
	if err != nil {
		fmt.Printf("%s⚠️  Connect to peer %d (%s) failed: %v\n", n.prefix, peerID, addr, err)
		return
	}
	defer conn.Close()
	if err := transport.Send(conn, msg); err != nil {
		fmt.Printf("%s⚠️  Send to peer %d failed: %v\n", n.prefix, peerID, err)
	}
}

// SetupTCPComm wires all OnSend* callbacks to use TCP transport.
// Call this after creating nodes and before calling Start().
func SetupTCPComm(nodes []*CoordinatorNode) {
	for _, node := range nodes {
		n := node

		n.Election.OnSendVoteReq = func(req types.VoteRequest) {
			msg := types.Message{
				Type:     types.MsgVoteRequest,
				SenderID: n.ID,
				Payload:  req,
				Timestamp: time.Now(),
			}
			for peerID := range n.PeerAddrs {
				n.tcpSend(peerID, msg)
			}
		}

		n.Election.OnSendVoteResp = func(targetID int, resp types.VoteResponse) {
			msg := types.Message{
				Type:     types.MsgVoteResponse,
				SenderID: n.ID,
				Payload:  resp,
				Timestamp: time.Now(),
			}
			n.tcpSend(targetID, msg)
		}

		n.Election.OnSendHeartbeat = func(hb types.Heartbeat) {
			msg := types.Message{
				Type:     types.MsgHeartbeat,
				SenderID: n.ID,
				Payload:  hb,
				Timestamp: time.Now(),
			}
			for peerID := range n.PeerAddrs {
				n.tcpSend(peerID, msg)
			}
		}

		n.TwoPCCoord.OnSendPrepare = func(participantID int, req types.PrepareRequest) {
			msg := types.Message{
				Type:     types.MsgPrepareRequest,
				SenderID: n.ID,
				Payload:  req,
				Timestamp: time.Now(),
			}
			n.tcpSend(participantID, msg)
		}

		n.TwoPCCoord.OnSendCommit = func(participantID int, commitReq types.CommitRequest) {
			msg := types.Message{
				Type:     types.MsgCommitRequest,
				SenderID: n.ID,
				Payload:  commitReq,
				Timestamp: time.Now(),
			}
			n.tcpSend(participantID, msg)
		}

		n.TwoPCCoord.OnSendAbort = func(participantID int, abortReq types.AbortRequest) {
			msg := types.Message{
				Type:     types.MsgAbortRequest,
				SenderID: n.ID,
				Payload:  abortReq,
				Timestamp: time.Now(),
			}
			n.tcpSend(participantID, msg)
		}

		n.TwoPCPart.OnSendVote = func(resp types.PrepareResponse) {
			msg := types.Message{
				Type:     types.MsgPrepareResponse,
				SenderID: n.ID,
				Payload:  resp,
				Timestamp: time.Now(),
			}
			// Send back to all peers; only the leader's Coordinator will use it.
			for peerID := range n.PeerAddrs {
				n.tcpSend(peerID, msg)
			}
		}
	}
}

func (n *CoordinatorNode) IsAlive() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.alive
}

func (n *CoordinatorNode) Kill() {
	n.mu.Lock()
	n.alive = false
	n.mu.Unlock()
	n.Election.Stop()
	fmt.Printf("%s💀 ═══════════════════════════════════════════\n", n.prefix)
	fmt.Printf("%s💀 NODE KILLED (simulating crash)\n", n.prefix)
	fmt.Printf("%s💀 ═══════════════════════════════════════════\n", n.prefix)
}

func SetupInProcessComm(nodes []*CoordinatorNode) {
	for _, node := range nodes {
		node.Peers = nodes
	}

	for _, node := range nodes {
		n := node

		n.Election.OnSendVoteReq = func(req types.VoteRequest) {
			for _, peer := range nodes {
				if peer.ID != n.ID && peer.IsAlive() {
					peer.Election.VoteRequestCh <- req
				}
			}
		}

		n.Election.OnSendVoteResp = func(targetID int, resp types.VoteResponse) {
			for _, peer := range nodes {
				if peer.ID == targetID && peer.IsAlive() {
					peer.Election.VoteResponseCh <- resp
				}
			}
		}

		n.Election.OnSendHeartbeat = func(hb types.Heartbeat) {
			for _, peer := range nodes {
				if peer.ID != n.ID && peer.IsAlive() {
					peer.Election.HeartbeatCh <- hb
				}
			}
		}

		n.TwoPCCoord.OnSendPrepare = func(participantID int, req types.PrepareRequest) {
			for _, peer := range nodes {
				if peer.ID == participantID && peer.IsAlive() {
					peer.TwoPCPart.HandlePrepare(req)
				}
			}
		}

		n.TwoPCCoord.OnSendCommit = func(participantID int, commitReq types.CommitRequest) {
			for _, peer := range nodes {
				if peer.ID == participantID && peer.IsAlive() {
					peer.TwoPCPart.HandleCommit(commitReq)
				}
			}
		}

		n.TwoPCCoord.OnSendAbort = func(participantID int, abortReq types.AbortRequest) {
			for _, peer := range nodes {
				if peer.ID == participantID && peer.IsAlive() {
					peer.TwoPCPart.HandleAbort(abortReq)
				}
			}
		}

		n.TwoPCPart.OnSendVote = func(resp types.PrepareResponse) {
			for _, peer := range nodes {
				if peer.Election.IsLeader() && peer.IsAlive() {
					peer.TwoPCCoord.PrepareResponseCh <- resp
				}
			}
		}
	}
}

func (n *CoordinatorNode) AcquireLock(resource, requester string) types.LockResponse {
	if !n.Election.IsLeader() {
		return types.LockResponse{
			Resource: resource,
			Granted:  false,
			Reason:   "this node is not the leader",
		}
	}
	return n.LockMgr.Acquire(resource, requester)
}

func (n *CoordinatorNode) ProposeChange(txID string, change types.ChangeType, key, value string) bool {
	if !n.Election.IsLeader() {
		fmt.Printf("%s⚠️  Cannot propose change — not the leader\n", n.prefix)
		return false
	}

	var participants []int
	for _, peer := range n.Peers {
		if peer.ID != n.ID && peer.IsAlive() {
			participants = append(participants, peer.ID)
		}
	}

	return n.TwoPCCoord.Propose(txID, change, key, value, participants)
}
