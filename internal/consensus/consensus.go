package consensus

import (
	"fmt"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

const (
	PrepareTimeout = 3 * time.Second
)

type TxState int

const (
	TxPending   TxState = iota
	TxPrepared
	TxCommitted
	TxAborted
)

func (s TxState) String() string {
	switch s {
	case TxPending:
		return "PENDING"
	case TxPrepared:
		return "PREPARED"
	case TxCommitted:
		return "COMMITTED"
	case TxAborted:
		return "ABORTED"
	default:
		return "UNKNOWN"
	}
}

type Transaction struct {
	ID            string                 `json:"id"`
	Change        types.ChangeType       `json:"change_type"`
	Key           string                 `json:"key"`
	Value         string                 `json:"value"`
	State         TxState                `json:"state"`
	Participants  []int                  `json:"participants"`
	Votes         map[int]bool           `json:"votes"`
	CreatedAt     time.Time              `json:"created_at"`
}

type Coordinator struct {
	mu     sync.Mutex
	nodeID int
	prefix string

	transactions map[string]*Transaction

	RoutingTable *types.RoutingTable

	OnSendPrepare func(participantID int, req types.PrepareRequest)
	OnSendCommit  func(participantID int, req types.CommitRequest)
	OnSendAbort   func(participantID int, req types.AbortRequest)

	PrepareResponseCh chan types.PrepareResponse
}

func NewCoordinator(nodeID int) *Coordinator {
	return &Coordinator{
		nodeID:            nodeID,
		prefix:            types.LogPrefix(nodeID, "2PC-COORD"),
		transactions:      make(map[string]*Transaction),
		RoutingTable:      types.NewRoutingTable(),
		PrepareResponseCh: make(chan types.PrepareResponse, 100),
	}
}

func (c *Coordinator) Propose(txID string, change types.ChangeType, key, value string, participants []int) bool {
	c.mu.Lock()

	tx := &Transaction{
		ID:           txID,
		Change:       change,
		Key:          key,
		Value:        value,
		State:        TxPending,
		Participants: participants,
		Votes:        make(map[int]bool),
		CreatedAt:    time.Now(),
	}
	c.transactions[txID] = tx
	c.mu.Unlock()

	fmt.Printf("%s\n", "══════════════════════════════════════════════════════════════")
	fmt.Printf("%s📋 INITIATING 2PC TRANSACTION: %s\n", c.prefix, txID)
	fmt.Printf("%s   Change: %s | Key: \"%s\" | Value: \"%s\"\n", c.prefix, change, key, value)
	fmt.Printf("%s   Participants: %v\n", c.prefix, participants)
	fmt.Printf("%s\n", "══════════════════════════════════════════════════════════════")

	req := types.PrepareRequest{
		TransactionID: txID,
		Change:        change,
		Key:           key,
		Value:         value,
	}

	fmt.Printf("%s📤 PHASE 1: Sending PREPARE to %d participants...\n",
		c.prefix, len(participants))

	for _, pid := range participants {
		if c.OnSendPrepare != nil {
			c.OnSendPrepare(pid, req)
		}
	}

	return c.collectVotes(tx)
}

func (c *Coordinator) collectVotes(tx *Transaction) bool {
	deadline := time.After(PrepareTimeout)
	votesNeeded := len(tx.Participants)
	votesReceived := 0

	fmt.Printf("%s⏳ Waiting for %d votes (timeout: %v)...\n",
		c.prefix, votesNeeded, PrepareTimeout)

	for votesReceived < votesNeeded {
		select {
		case resp := <-c.PrepareResponseCh:
			if resp.TransactionID != tx.ID {
				continue
			}

			c.mu.Lock()
			tx.Votes[resp.ParticipantID] = resp.VoteCommit
			votesReceived++

			if resp.VoteCommit {
				fmt.Printf("%s   ✅ Node %d voted COMMIT (%d/%d)\n",
					c.prefix, resp.ParticipantID, votesReceived, votesNeeded)
			} else {
				fmt.Printf("%s   ❌ Node %d voted ABORT (%d/%d)\n",
					c.prefix, resp.ParticipantID, votesReceived, votesNeeded)
				c.mu.Unlock()
				c.abortTransaction(tx, fmt.Sprintf("Node %d voted ABORT", resp.ParticipantID))
				return false
			}
			c.mu.Unlock()

		case <-deadline:
			c.abortTransaction(tx, "timeout waiting for participant votes")
			return false
		}
	}

	return c.commitTransaction(tx)
}

func (c *Coordinator) commitTransaction(tx *Transaction) bool {
	c.mu.Lock()
	tx.State = TxCommitted

	c.applyChange(tx)
	c.mu.Unlock()

	fmt.Printf("%s📤 PHASE 2: All voted COMMIT → Sending COMMIT to all participants\n", c.prefix)

	commitReq := types.CommitRequest{
		TransactionID: tx.ID,
	}

	for _, pid := range tx.Participants {
		if c.OnSendCommit != nil {
			c.OnSendCommit(pid, commitReq)
		}
	}

	fmt.Printf("%s✅ ═══════════════════════════════════════════\n", c.prefix)
	fmt.Printf("%s✅ TRANSACTION %s COMMITTED SUCCESSFULLY\n", c.prefix, tx.ID)
	fmt.Printf("%s✅ ═══════════════════════════════════════════\n", c.prefix)

	return true
}

func (c *Coordinator) abortTransaction(tx *Transaction, reason string) {
	c.mu.Lock()
	tx.State = TxAborted
	c.mu.Unlock()

	fmt.Printf("%s❌ PHASE 2: ABORTING transaction %s — Reason: %s\n",
		c.prefix, tx.ID, reason)

	abortReq := types.AbortRequest{
		TransactionID: tx.ID,
		Reason:        reason,
	}

	for _, pid := range tx.Participants {
		if c.OnSendAbort != nil {
			c.OnSendAbort(pid, abortReq)
		}
	}

	fmt.Printf("%s❌ Transaction %s ABORTED\n", c.prefix, tx.ID)
}

func (c *Coordinator) applyChange(tx *Transaction) {
	switch tx.Change {
	case types.ChangeAddServer:
		c.RoutingTable.Servers[tx.Key] = tx.Value
		fmt.Printf("%s📝 Applied: Server \"%s\" → %s\n", c.prefix, tx.Key, tx.Value)

	case types.ChangeAddRoom:
		c.RoutingTable.Rooms[tx.Key] = &types.RoutingEntry{
			RoomName:   tx.Key,
			ServerAddr: tx.Value,
			CreatedAt:  time.Now(),
		}
		fmt.Printf("%s📝 Applied: Room \"%s\" → server %s\n", c.prefix, tx.Key, tx.Value)
	}
}

func (c *Coordinator) GetRoutingTable() *types.RoutingTable {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.RoutingTable
}

type Participant struct {
	mu     sync.Mutex
	nodeID int
	prefix string

	RoutingTable *types.RoutingTable

	pending map[string]*types.PrepareRequest

	OnSendVote func(resp types.PrepareResponse)
}

func NewParticipant(nodeID int) *Participant {
	return &Participant{
		nodeID:       nodeID,
		prefix:       types.LogPrefix(nodeID, "2PC-PART"),
		RoutingTable: types.NewRoutingTable(),
		pending:      make(map[string]*types.PrepareRequest),
	}
}

func (p *Participant) HandlePrepare(req types.PrepareRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Printf("%s📩 Received PREPARE for tx %s: %s \"%s\"\n",
		p.prefix, req.TransactionID, req.Change, req.Key)

	voteCommit := true
	reason := ""

	switch req.Change {
	case types.ChangeAddRoom:
		if _, exists := p.RoutingTable.Rooms[req.Key]; exists {
			voteCommit = false
			reason = fmt.Sprintf("room \"%s\" already exists", req.Key)
		}
	case types.ChangeAddServer:
		if _, exists := p.RoutingTable.Servers[req.Key]; exists {
			voteCommit = false
			reason = fmt.Sprintf("server \"%s\" already registered", req.Key)
		}
	}

	if voteCommit {
		p.pending[req.TransactionID] = &req
		fmt.Printf("%s✅ Voting COMMIT for tx %s\n", p.prefix, req.TransactionID)
	} else {
		fmt.Printf("%s❌ Voting ABORT for tx %s — %s\n", p.prefix, req.TransactionID, reason)
	}

	resp := types.PrepareResponse{
		TransactionID: req.TransactionID,
		VoteCommit:    voteCommit,
		ParticipantID: p.nodeID,
	}

	if p.OnSendVote != nil {
		go p.OnSendVote(resp)
	}
}

func (p *Participant) HandleCommit(req types.CommitRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pending, ok := p.pending[req.TransactionID]
	if !ok {
		fmt.Printf("%s⚠️  Received COMMIT for unknown tx %s\n",
			p.prefix, req.TransactionID)
		return
	}

	switch pending.Change {
	case types.ChangeAddRoom:
		p.RoutingTable.Rooms[pending.Key] = &types.RoutingEntry{
			RoomName:   pending.Key,
			ServerAddr: pending.Value,
			CreatedAt:  time.Now(),
		}
		fmt.Printf("%s📝 COMMITTED: Room \"%s\" → server %s\n",
			p.prefix, pending.Key, pending.Value)

	case types.ChangeAddServer:
		p.RoutingTable.Servers[pending.Key] = pending.Value
		fmt.Printf("%s📝 COMMITTED: Server \"%s\" → %s\n",
			p.prefix, pending.Key, pending.Value)
	}

	delete(p.pending, req.TransactionID)
}

func (p *Participant) HandleAbort(req types.AbortRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Printf("%s🗑  ABORTED tx %s — Reason: %s\n",
		p.prefix, req.TransactionID, req.Reason)

	delete(p.pending, req.TransactionID)
}
