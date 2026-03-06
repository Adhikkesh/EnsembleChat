package lock

import (
	"fmt"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

const (
	// TokenIdlePassInterval controls how often an idle token holder
	// automatically passes the token to the next node in the ring.
	TokenIdlePassInterval = 200 * time.Millisecond
)

// TokenRingManager implements mutual exclusion using the Token Ring algorithm.
//
// Algorithm Overview:
//   - Nodes are arranged in a logical ring (e.g., Node 0 → Node 1 → Node 2 → Node 0).
//   - A single token circulates around the ring.
//   - Only the node currently holding the token can enter the critical section.
//   - After exiting the critical section, the token is passed to the next node.
//   - If a node doesn't need the critical section, it passes the token along
//     after a short idle interval.
//
// Properties:
//   - Mutual Exclusion: Only one node can be in the critical section at a time.
//   - Fairness: Every node eventually receives the token (starvation-free).
//   - No deadlock: The token always circulates.
type TokenRingManager struct {
	mu     sync.Mutex
	nodeID int
	prefix string

	// Ring topology
	ringOrder []int // ordered node IDs forming the ring
	ringIndex int   // this node's position in ringOrder

	// Token state
	hasToken bool
	seqNum   int

	// Critical section state
	inCS       bool
	csHolder   string // who holds the CS currently
	csResource string // which resource

	// Waiting requests queue
	waitQueue []csRequest

	// Channel to receive token from predecessor in the ring
	TokenCh chan types.TokenMessage

	// Callback to pass token to successor in the ring
	OnPassToken func(nextNodeID int, tok types.TokenMessage)

	stopCh chan struct{}
}

// csRequest represents a pending request to enter the critical section.
type csRequest struct {
	resource   string
	requester  string
	responseCh chan types.LockResponse
}

// NewTokenRingManager creates a new Token Ring mutual exclusion manager.
// ringOrder defines the logical ring. initialHolder is the node that starts with the token.
func NewTokenRingManager(nodeID int, ringOrder []int, initialHolder int) *TokenRingManager {
	idx := -1
	for i, id := range ringOrder {
		if id == nodeID {
			idx = i
			break
		}
	}

	return &TokenRingManager{
		nodeID:    nodeID,
		prefix:    types.LogPrefix(nodeID, "TOKEN-RING"),
		ringOrder: ringOrder,
		ringIndex: idx,
		hasToken:  nodeID == initialHolder,
		seqNum:    0,
		waitQueue: make([]csRequest, 0),
		TokenCh:   make(chan types.TokenMessage, 10),
		stopCh:    make(chan struct{}),
	}
}

// Start begins the token ring processing loop.
func (t *TokenRingManager) Start() {
	go t.run()
	if t.hasToken {
		fmt.Printf("%s🪙 Token Ring started — THIS NODE holds the initial token (ring: %v)\n",
			t.prefix, t.ringOrder)
	} else {
		fmt.Printf("%s🔄 Token Ring started — waiting for token (ring: %v)\n",
			t.prefix, t.ringOrder)
	}
}

// Stop terminates the token ring processing loop.
func (t *TokenRingManager) Stop() {
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
}

// run is the main event loop: receive tokens, grant waiting requests, or pass along.
func (t *TokenRingManager) run() {
	ticker := time.NewTicker(TokenIdlePassInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case tok := <-t.TokenCh:
			t.handleTokenReceive(tok)
		case <-ticker.C:
			t.idlePass()
		}
	}
}

// handleTokenReceive is called when this node receives the token from its predecessor.
func (t *TokenRingManager) handleTokenReceive(tok types.TokenMessage) {
	t.mu.Lock()
	t.hasToken = true
	t.seqNum = tok.SeqNum

	if len(t.waitQueue) > 0 {
		// Only log when the token is actually being used
		fmt.Printf("%s🪙 ← Received token (seq: %d) — granting queued request\n", t.prefix, tok.SeqNum)
		// Grant access to the first waiting request
		req := t.waitQueue[0]
		t.waitQueue = t.waitQueue[1:]

		t.inCS = true
		t.csHolder = req.requester
		t.csResource = req.resource

		fmt.Printf("%s🪙 ✅ GRANTED critical section to [%s] for resource \"%s\"\n",
			t.prefix, req.requester, req.resource)

		t.mu.Unlock()

		req.responseCh <- types.LockResponse{
			Resource: req.resource,
			Granted:  true,
			Holder:   req.requester,
			Reason:   "token acquired — critical section granted",
		}
		return
	}

	// No one waiting — pass along silently (idle circulation, no log)
	t.mu.Unlock()
}

// idlePass passes the token to the next node if idle (not in CS, no waiting requests).
func (t *TokenRingManager) idlePass() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.hasToken && !t.inCS && len(t.waitQueue) == 0 {
		t.passTokenLocked(false) // idle circulation — no log
	}
}

// passTokenLocked passes the token to the next node in the ring. Caller must hold t.mu.
// verbose=true logs the pass (used during real lock ops); false suppresses idle-pass noise.
func (t *TokenRingManager) passTokenLocked(verbose bool) {
	nextIdx := (t.ringIndex + 1) % len(t.ringOrder)
	nextNode := t.ringOrder[nextIdx]

	t.seqNum++
	tok := types.TokenMessage{
		SeqNum: t.seqNum,
	}

	t.hasToken = false
	if verbose {
		fmt.Printf("%s🪙 → Passing token (seq: %d) to Node %d\n",
			t.prefix, tok.SeqNum, nextNode)
	}

	if t.OnPassToken != nil {
		go t.OnPassToken(nextNode, tok)
	}
}

// RequestAccess requests to enter the critical section for a given resource.
// Blocks until the token is held and access is granted.
func (t *TokenRingManager) RequestAccess(resource, requester string) types.LockResponse {
	t.mu.Lock()

	// If we already have the token and no one is in CS, grant immediately
	if t.hasToken && !t.inCS {
		t.inCS = true
		t.csHolder = requester
		t.csResource = resource

		fmt.Printf("%s🪙 ✅ GRANTED critical section immediately to [%s] for \"%s\" (token already held)\n",
			t.prefix, requester, resource)

		t.mu.Unlock()
		return types.LockResponse{
			Resource: resource,
			Granted:  true,
			Holder:   requester,
			Reason:   "token already held — access granted immediately",
		}
	}

	// Queue the request and wait for the token
	responseCh := make(chan types.LockResponse, 1)
	t.waitQueue = append(t.waitQueue, csRequest{
		resource:   resource,
		requester:  requester,
		responseCh: responseCh,
	})

	fmt.Printf("%s🪙 ⏳ [%s] queued for critical section on \"%s\" (waiting for token...)\n",
		t.prefix, requester, resource)

	t.mu.Unlock()

	// Block until granted
	return <-responseCh
}

// ReleaseAccess exits the critical section and passes the token to the next node.
func (t *TokenRingManager) ReleaseAccess(resource, requester string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.inCS {
		fmt.Printf("%s⚠️ Cannot release — not in critical section\n", t.prefix)
		return false
	}

	if t.csHolder != requester {
		fmt.Printf("%s⚠️ Cannot release — held by [%s], not [%s]\n",
			t.prefix, t.csHolder, requester)
		return false
	}

	fmt.Printf("%s🪙 🔓 [%s] exited critical section for \"%s\"\n",
		t.prefix, requester, resource)

	t.inCS = false
	t.csHolder = ""
	t.csResource = ""

	// Check if there's another waiting request on this same node
	if len(t.waitQueue) > 0 {
		req := t.waitQueue[0]
		t.waitQueue = t.waitQueue[1:]

		t.inCS = true
		t.csHolder = req.requester
		t.csResource = req.resource

		fmt.Printf("%s🪙 ✅ GRANTED critical section to next queued: [%s] for \"%s\"\n",
			t.prefix, req.requester, req.resource)

		go func() {
			req.responseCh <- types.LockResponse{
				Resource: req.resource,
				Granted:  true,
				Holder:   req.requester,
				Reason:   "token acquired — critical section granted",
			}
		}()
		return true
	}

	// Nobody waiting locally, pass token to next node in the ring
	t.passTokenLocked(true) // real release — log it
	return true
}

// HasToken returns whether this node currently holds the token.
func (t *TokenRingManager) HasToken() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hasToken
}

// GetStatus returns the current token ring status for this node.
func (t *TokenRingManager) GetStatus() (hasToken bool, inCS bool, resource string, holder string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hasToken, t.inCS, t.csResource, t.csHolder
}

// GetNextNode returns the ID of the next node in the ring.
func (t *TokenRingManager) GetNextNode() int {
	nextIdx := (t.ringIndex + 1) % len(t.ringOrder)
	return t.ringOrder[nextIdx]
}
