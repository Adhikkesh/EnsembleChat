package coordinator

import (
	"fmt"
	"net"
	"sort"
	"sync"

	"distributed-chat-coordinator/internal/consensus"
	"distributed-chat-coordinator/internal/election"
	"distributed-chat-coordinator/internal/lock"
	"distributed-chat-coordinator/internal/transport"
	"distributed-chat-coordinator/internal/types"
)

// CoordinatorNode represents a single node in the coordination ensemble.
// It integrates three distributed algorithms:
//   - Raft Leader Election (election module)
//   - Token Ring Mutual Exclusion (lock module)
//   - Raft Log Replication for Consensus (consensus module)
type CoordinatorNode struct {
	mu sync.Mutex

	ID        int
	Addr      string
	Peers     []*CoordinatorNode
	PeerAddrs map[int]string // peerID → "host:port"

	Election  *election.ElectionNode
	TokenRing *lock.TokenRingManager
	RaftLog   *consensus.RaftLog

	listener   net.Listener
	rpcClients map[int]*transport.RPCClient // lazy-initialized per peer
	prefix     string
	alive      bool
	stopCh     chan struct{}
}

// NewCoordinatorNode creates a new coordinator node with all three algorithms.
func NewCoordinatorNode(id int, peerIDs []int) *CoordinatorNode {
	// Build ring order: all node IDs sorted for a deterministic ring
	allNodes := make([]int, 0, len(peerIDs)+1)
	allNodes = append(allNodes, id)
	allNodes = append(allNodes, peerIDs...)
	sort.Ints(allNodes)

	node := &CoordinatorNode{
		ID:         id,
		prefix:     types.LogPrefix(id, "COORDINATOR"),
		alive:      true,
		stopCh:     make(chan struct{}),
		PeerAddrs:  make(map[int]string),
		rpcClients: make(map[int]*transport.RPCClient),
		Election:   election.NewElectionNode(id, peerIDs),
		TokenRing:  lock.NewTokenRingManager(id, allNodes, allNodes[0]), // lowest ID starts with token
		RaftLog:    consensus.NewRaftLog(id, peerIDs),
	}

	// Wire election callbacks to update the Raft log module
	node.Election.OnBecomeLeader = func(term int) {
		fmt.Printf("%s👑 I am now the LEADER (term %d)\n", node.prefix, term)
		node.RaftLog.BecomeLeader(term)
	}

	node.Election.OnBecomeFollower = func(leaderID, term int) {
		fmt.Printf("%s📥 Following Leader Node %d (term %d)\n",
			node.prefix, leaderID, term)
		node.RaftLog.BecomeFollower(term)
	}

	return node
}

// Start begins the election and token ring modules.
func (n *CoordinatorNode) Start() {
	fmt.Printf("%s🚀 Starting coordinator node...\n", n.prefix)
	n.Election.Start()
	n.TokenRing.Start()
}

// Stop shuts down all modules.
func (n *CoordinatorNode) Stop() {
	n.mu.Lock()
	n.alive = false
	n.mu.Unlock()

	if n.listener != nil {
		n.listener.Close()
	}

	fmt.Printf("%s🛑 Stopping coordinator node...\n", n.prefix)
	n.Election.Stop()
	n.TokenRing.Stop()
	n.RaftLog.Stop()
}

// ══════════════════════════════════════════════════════════════════════════════
// net/rpc server — implements transport.RPCHandler
// ══════════════════════════════════════════════════════════════════════════════

// HandleVoteRequest dispatches an inbound RPC to the election module.
func (n *CoordinatorNode) HandleVoteRequest(req types.VoteRequest) {
	n.Election.VoteRequestCh <- req
}

// HandleVoteResponse dispatches an inbound RPC to the election module.
func (n *CoordinatorNode) HandleVoteResponse(resp types.VoteResponse) {
	n.Election.VoteResponseCh <- resp
}

// HandleHeartbeat dispatches an inbound RPC to the election module.
func (n *CoordinatorNode) HandleHeartbeat(hb types.Heartbeat) {
	n.Election.HeartbeatCh <- hb
}

// HandleAppendEntries dispatches an inbound RPC to the Raft log module.
func (n *CoordinatorNode) HandleAppendEntries(req types.AppendEntriesRequest) {
	n.RaftLog.HandleAppendEntries(req)
}

// HandleAppendEntriesResp dispatches an inbound RPC to the Raft log module.
func (n *CoordinatorNode) HandleAppendEntriesResp(resp types.AppendEntriesResponse) {
	n.RaftLog.AppendEntriesRespCh <- resp
}

// HandleTokenPass dispatches an inbound RPC to the token ring module.
func (n *CoordinatorNode) HandleTokenPass(tok types.TokenMessage) {
	n.TokenRing.TokenCh <- tok
}

// StartRPC opens a TCP listener and registers this node's RPC service.
// Call this before Start() so peers can connect immediately.
func (n *CoordinatorNode) StartRPC() error {
	ln, err := net.Listen("tcp", n.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", n.Addr, err)
	}
	n.listener = ln

	svc := transport.NewNodeRPC(n) // n satisfies transport.RPCHandler
	transport.StartRPCServer(ln, svc)

	fmt.Printf("%s📡 RPC server started on %s\n", n.prefix, n.Addr)
	return nil
}

// ══════════════════════════════════════════════════════════════════════════════
// net/rpc client helpers
// ══════════════════════════════════════════════════════════════════════════════

// rpcClient returns (or lazily creates) an RPCClient for the given peer.
func (n *CoordinatorNode) rpcClient(peerID int) *transport.RPCClient {
	if c, ok := n.rpcClients[peerID]; ok {
		return c
	}
	addr, ok := n.PeerAddrs[peerID]
	if !ok {
		fmt.Printf("%s⚠️  No address for peer %d\n", n.prefix, peerID)
		return nil
	}
	c := transport.NewRPCClient(addr, n.prefix)
	n.rpcClients[peerID] = c
	return c
}

// ══════════════════════════════════════════════════════════════════════════════
// SetupRPCComm — wire all OnSend* callbacks to make outbound net/rpc calls.
// Call this after creating nodes and setting PeerAddrs, before calling Start().
// ══════════════════════════════════════════════════════════════════════════════

func SetupRPCComm(nodes []*CoordinatorNode) {
	for _, node := range nodes {
		n := node

		// --- Raft Election callbacks (RPC) ---

		n.Election.OnSendVoteReq = func(req types.VoteRequest) {
			for peerID := range n.PeerAddrs {
				c := n.rpcClient(peerID)
				if c != nil {
					go c.SendVoteRequest(req)
				}
			}
		}

		n.Election.OnSendVoteResp = func(targetID int, resp types.VoteResponse) {
			c := n.rpcClient(targetID)
			if c != nil {
				go c.SendVoteResponse(resp)
			}
		}

		n.Election.OnSendHeartbeat = func(hb types.Heartbeat) {
			for peerID := range n.PeerAddrs {
				c := n.rpcClient(peerID)
				if c != nil {
					go c.SendHeartbeat(hb)
				}
			}
		}

		// --- Raft Log Replication callbacks (RPC) ---

		n.RaftLog.OnSendAppendEntries = func(followerID int, req types.AppendEntriesRequest) {
			c := n.rpcClient(followerID)
			if c != nil {
				go c.SendAppendEntries(req)
			}
		}

		n.RaftLog.OnSendAppendEntriesResp = func(leaderID int, resp types.AppendEntriesResponse) {
			c := n.rpcClient(leaderID)
			if c != nil {
				go c.SendAppendEntriesResp(resp)
			}
		}

		// --- Token Ring callback (RPC) ---

		n.TokenRing.OnPassToken = func(nextNodeID int, tok types.TokenMessage) {
			c := n.rpcClient(nextNodeID)
			if c != nil {
				go c.SendToken(tok)
			}
		}
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// In-process communication — for demos and testing (unchanged)
// ══════════════════════════════════════════════════════════════════════════════

// SetupInProcessComm wires all callbacks for in-process (channel-based)
// communication. Use this for demos and testing instead of RPC.
func SetupInProcessComm(nodes []*CoordinatorNode) {
	for _, node := range nodes {
		node.Peers = nodes
	}

	for _, node := range nodes {
		n := node

		// --- Election callbacks (in-process) ---
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

		// --- Raft Log Replication callbacks (in-process) ---
		n.RaftLog.OnSendAppendEntries = func(followerID int, req types.AppendEntriesRequest) {
			for _, peer := range nodes {
				if peer.ID == followerID && peer.IsAlive() {
					peer.RaftLog.HandleAppendEntries(req)
				}
			}
		}

		n.RaftLog.OnSendAppendEntriesResp = func(leaderID int, resp types.AppendEntriesResponse) {
			for _, peer := range nodes {
				if peer.ID == leaderID && peer.IsAlive() {
					peer.RaftLog.AppendEntriesRespCh <- resp
				}
			}
		}

		// --- Token Ring callback (in-process) ---
		n.TokenRing.OnPassToken = func(nextNodeID int, tok types.TokenMessage) {
			for _, peer := range nodes {
				if peer.ID == nextNodeID && peer.IsAlive() {
					peer.TokenRing.TokenCh <- tok
				}
			}
		}
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// Node lifecycle helpers
// ══════════════════════════════════════════════════════════════════════════════

// IsAlive returns whether this node is still running.
func (n *CoordinatorNode) IsAlive() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.alive
}

// Kill simulates a node crash.
func (n *CoordinatorNode) Kill() {
	n.mu.Lock()
	n.alive = false
	n.mu.Unlock()
	n.Election.Stop()
	n.TokenRing.Stop()
	fmt.Printf("%s💀 ═══════════════════════════════════════════\n", n.prefix)
	fmt.Printf("%s💀 NODE KILLED (simulating crash)\n", n.prefix)
	fmt.Printf("%s💀 ═══════════════════════════════════════════\n", n.prefix)
}

// ══════════════════════════════════════════════════════════════════════════════
// Application-level operations
// ══════════════════════════════════════════════════════════════════════════════

// AcquireLock requests access to the critical section via the Token Ring algorithm.
func (n *CoordinatorNode) AcquireLock(resource, requester string) types.LockResponse {
	return n.TokenRing.RequestAccess(resource, requester)
}

// ReleaseLock exits the critical section and passes the token to the next node.
func (n *CoordinatorNode) ReleaseLock(resource, requester string) bool {
	return n.TokenRing.ReleaseAccess(resource, requester)
}

// ProposeChange proposes a change via Raft Log Replication (in-process mode).
func (n *CoordinatorNode) ProposeChange(txID string, change types.ChangeType, key, value string) bool {
	if !n.Election.IsLeader() {
		fmt.Printf("%s⚠️  Cannot propose change — not the leader\n", n.prefix)
		return false
	}

	return n.RaftLog.ProposeEntry(change, key, value)
}

// ProposeChangeRPC proposes a change via Raft Log Replication (RPC mode).
func (n *CoordinatorNode) ProposeChangeRPC(txID string, change types.ChangeType, key, value string) bool {
	if !n.Election.IsLeader() {
		fmt.Printf("%s⚠️  Cannot propose change — not the leader\n", n.prefix)
		return false
	}

	return n.RaftLog.ProposeEntry(change, key, value)
}
