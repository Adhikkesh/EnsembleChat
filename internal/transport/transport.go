package transport

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

// ──────────────────────────────────────────────────────────────────────────────
// RPC Service — registered on every coordinator node so peers can call it.
//
// Go's net/rpc requires exported methods on an exported type with the
// signature:  func (t *T) MethodName(args *Args, reply *Reply) error
//
// Each RPC method simply pushes the received payload into the appropriate
// channel on the owning CoordinatorNode (via the Handler interface).
// ──────────────────────────────────────────────────────────────────────────────

// RPCHandler is implemented by the coordinator node to dispatch inbound RPCs.
type RPCHandler interface {
	HandleVoteRequest(req types.VoteRequest)
	HandleVoteResponse(resp types.VoteResponse)
	HandleHeartbeat(hb types.Heartbeat)
	HandleAppendEntries(req types.AppendEntriesRequest)
	HandleAppendEntriesResp(resp types.AppendEntriesResponse)
	HandleTokenPass(tok types.TokenMessage)
}

// NodeRPC is the RPC service that gets registered with net/rpc.
type NodeRPC struct {
	handler RPCHandler
}

// NewNodeRPC creates a NodeRPC backed by the given handler.
func NewNodeRPC(h RPCHandler) *NodeRPC {
	return &NodeRPC{handler: h}
}

// Empty is a placeholder reply for one-way RPCs.
type Empty struct{}

// RequestVote — Raft leader election: receive a vote request.
func (s *NodeRPC) RequestVote(req *types.VoteRequest, reply *Empty) error {
	s.handler.HandleVoteRequest(*req)
	return nil
}

// RespondVote — Raft leader election: receive a vote response.
func (s *NodeRPC) RespondVote(resp *types.VoteResponse, reply *Empty) error {
	s.handler.HandleVoteResponse(*resp)
	return nil
}

// SendHeartbeat — Raft leader election: receive a heartbeat from the leader.
func (s *NodeRPC) SendHeartbeat(hb *types.Heartbeat, reply *Empty) error {
	s.handler.HandleHeartbeat(*hb)
	return nil
}

// AppendEntries — Raft log replication: receive log entries from the leader.
func (s *NodeRPC) AppendEntries(req *types.AppendEntriesRequest, reply *Empty) error {
	s.handler.HandleAppendEntries(*req)
	return nil
}

// AppendEntriesReply — Raft log replication: leader receives acknowledgement.
func (s *NodeRPC) AppendEntriesReply(resp *types.AppendEntriesResponse, reply *Empty) error {
	s.handler.HandleAppendEntriesResp(*resp)
	return nil
}

// PassToken — Token Ring mutual exclusion: receive the token from predecessor.
func (s *NodeRPC) PassToken(tok *types.TokenMessage, reply *Empty) error {
	s.handler.HandleTokenPass(*tok)
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Server helpers
// ──────────────────────────────────────────────────────────────────────────────

// StartRPCServer registers the service and accepts connections on the listener.
func StartRPCServer(ln net.Listener, service *NodeRPC) {
	server := rpc.NewServer()
	server.RegisterName("Node", service)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go server.ServeConn(conn)
		}
	}()
}

// ──────────────────────────────────────────────────────────────────────────────
// Client helpers — thin wrappers over net/rpc.Client for each RPC method.
// ──────────────────────────────────────────────────────────────────────────────

// RPCClient wraps a net/rpc.Client for a specific peer.
type RPCClient struct {
	addr      string
	prefix    string
	mu        sync.Mutex
	reachable bool // last known state; used to suppress repeated failure logs
}

// NewRPCClient creates a client that dials the given address.
func NewRPCClient(addr, prefix string) *RPCClient {
	return &RPCClient{addr: addr, prefix: prefix, reachable: true}
}

// dial connects to the peer with a single fast attempt.
func (c *RPCClient) dial() (*rpc.Client, error) {
	conn, err := net.DialTimeout("tcp", c.addr, 500*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("dial %s failed: %w", c.addr, err)
	}
	return rpc.NewClient(conn), nil
}

// callAsync makes a non-blocking RPC call (fire and forget with logging).
// Dial failures are logged only once per reachability state change.
func (c *RPCClient) callAsync(method string, args interface{}) {
	client, err := c.dial()
	if err != nil {
		c.mu.Lock()
		if c.reachable {
			fmt.Printf("%s[WARN] Peer %s unreachable (will suppress further failures)\n", c.prefix, c.addr)
			c.reachable = false
		}
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	if !c.reachable {
		fmt.Printf("%s[OK] Peer %s is reachable again\n", c.prefix, c.addr)
		c.reachable = true
	}
	c.mu.Unlock()
	defer client.Close()

	var reply Empty
	if err := client.Call(method, args, &reply); err != nil {
		fmt.Printf("%s[WARN] RPC call %s failed: %v\n", c.prefix, method, err)
	}
}

func (c *RPCClient) SendVoteRequest(req types.VoteRequest) {
	c.callAsync("Node.RequestVote", &req)
}

func (c *RPCClient) SendVoteResponse(resp types.VoteResponse) {
	c.callAsync("Node.RespondVote", &resp)
}

func (c *RPCClient) SendHeartbeat(hb types.Heartbeat) {
	c.callAsync("Node.SendHeartbeat", &hb)
}

func (c *RPCClient) SendAppendEntries(req types.AppendEntriesRequest) {
	c.callAsync("Node.AppendEntries", &req)
}

func (c *RPCClient) SendAppendEntriesResp(resp types.AppendEntriesResponse) {
	c.callAsync("Node.AppendEntriesReply", &resp)
}

func (c *RPCClient) SendToken(tok types.TokenMessage) {
	c.callAsync("Node.PassToken", &tok)
}
