package consensus

import (
	"fmt"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

const (
	// ReplicationTimeout is how long the leader waits for majority replication.
	ReplicationTimeout = 5 * time.Second
)

// RaftLog implements Raft Log Replication for distributed consensus.
//
// Algorithm Overview (from the Raft paper):
//  1. A client sends a command to the leader.
//  2. The leader appends the command as a new entry in its log.
//  3. The leader sends AppendEntries RPCs to all followers in parallel.
//  4. Followers append the entries to their logs and acknowledge.
//  5. Once a majority of nodes have replicated the entry, it is committed.
//  6. The leader applies the committed entry to its state machine.
//  7. The leader notifies followers of the new commit index.
//  8. Followers apply committed entries to their state machines.
//
// Key Safety Properties:
//   - Log Matching: If two logs contain an entry with the same index and term,
//     then the logs are identical in all preceding entries.
//   - Leader Completeness: If an entry is committed in a given term, it will
//     be present in the logs of all leaders for higher-term numbers.
//   - State Machine Safety: If a server has applied a log entry at a given
//     index, no other server will ever apply a different entry for that index.
type RaftLog struct {
	mu sync.Mutex

	nodeID      int
	prefix      string
	isLeader    bool
	currentTerm int

	// Replicated log (persistent state)
	log []types.LogEntry

	// Volatile state — all servers
	commitIndex int // index of highest log entry known to be committed
	lastApplied int // index of highest log entry applied to state machine

	// Volatile state — leaders only (reinitialized after each election)
	nextIndex  map[int]int // for each follower: index of next log entry to send
	matchIndex map[int]int // for each follower: index of highest entry known to be replicated

	// State machine (the replicated application state)
	RoutingTable *types.RoutingTable

	// Peer IDs
	peers []int

	// Callbacks for sending RPCs
	OnSendAppendEntries     func(followerID int, req types.AppendEntriesRequest)
	OnSendAppendEntriesResp func(leaderID int, resp types.AppendEntriesResponse)

	// Channel for leader to receive responses from followers
	AppendEntriesRespCh chan types.AppendEntriesResponse

	stopCh chan struct{}
}

// NewRaftLog creates a new Raft log replication manager.
func NewRaftLog(nodeID int, peers []int) *RaftLog {
	return &RaftLog{
		nodeID:              nodeID,
		prefix:              types.LogPrefix(nodeID, "RAFT-LOG"),
		log:                 make([]types.LogEntry, 0),
		commitIndex:         -1,
		lastApplied:         -1,
		nextIndex:           make(map[int]int),
		matchIndex:          make(map[int]int),
		RoutingTable:        types.NewRoutingTable(),
		peers:               peers,
		AppendEntriesRespCh: make(chan types.AppendEntriesResponse, 100),
		stopCh:              make(chan struct{}),
	}
}

// ============================================================
// Leader-side methods
// ============================================================

// BecomeLeader reinitializes volatile leader state after winning an election.
// Called by the election module when this node becomes leader.
func (r *RaftLog) BecomeLeader(term int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.isLeader = true
	r.currentTerm = term

	// Initialize nextIndex to the end of the leader's log
	// Initialize matchIndex to -1 (no entries replicated yet)
	lastIdx := len(r.log)
	for _, pid := range r.peers {
		r.nextIndex[pid] = lastIdx
		r.matchIndex[pid] = -1
	}

	fmt.Printf("%s👑 Initialized as leader for term %d (log length: %d)\n",
		r.prefix, term, len(r.log))
}

// BecomeFollower updates state when this node steps down from being leader.
func (r *RaftLog) BecomeFollower(term int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.isLeader = false
	r.currentTerm = term
}

// ProposeEntry is called on the leader to propose a new log entry.
// It appends the entry to the leader's log, replicates to followers via
// AppendEntries RPCs, and waits for majority confirmation.
// Returns true if the entry was committed (majority replicated).
func (r *RaftLog) ProposeEntry(change types.ChangeType, key, value string) bool {
	r.mu.Lock()

	if !r.isLeader {
		fmt.Printf("%s⚠️ Cannot propose — not the leader\n", r.prefix)
		r.mu.Unlock()
		return false
	}

	// Step 1: Append entry to leader's log
	entry := types.LogEntry{
		Term:   r.currentTerm,
		Index:  len(r.log),
		Change: change,
		Key:    key,
		Value:  value,
	}
	r.log = append(r.log, entry)

	fmt.Printf("%s\n", "══════════════════════════════════════════════════════════════")
	fmt.Printf("%s📋 PROPOSING LOG ENTRY #%d (Raft Log Replication)\n", r.prefix, entry.Index)
	fmt.Printf("%s   Term: %d | Change: %s | Key: \"%s\" | Value: \"%s\"\n",
		r.prefix, entry.Term, change, key, value)
	fmt.Printf("%s   Replicating to %d followers via AppendEntries RPC...\n", r.prefix, len(r.peers))
	fmt.Printf("%s\n", "══════════════════════════════════════════════════════════════")

	r.mu.Unlock()

	// Step 2: Send AppendEntries to all followers
	r.sendAppendEntriesToAll()

	// Step 3: Wait for majority replication
	committed := r.waitForMajority(entry.Index)

	if committed {
		// Step 4: Send one more round of AppendEntries to propagate the
		// updated commit index to all followers (Raft §5.3)
		r.sendAppendEntriesToAll()
	}

	return committed
}

// sendAppendEntriesToAll sends AppendEntries RPCs to every follower.
func (r *RaftLog) sendAppendEntriesToAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, pid := range r.peers {
		r.sendAppendEntriesTo(pid)
	}
}

// sendAppendEntriesTo sends an AppendEntries RPC to a specific follower.
// Caller must hold r.mu.
func (r *RaftLog) sendAppendEntriesTo(followerID int) {
	nextIdx := r.nextIndex[followerID]

	// Determine prevLogIndex and prevLogTerm
	prevLogIndex := nextIdx - 1
	prevLogTerm := -1
	if prevLogIndex >= 0 && prevLogIndex < len(r.log) {
		prevLogTerm = r.log[prevLogIndex].Term
	}

	// Collect entries from nextIndex onwards
	var entries []types.LogEntry
	if nextIdx < len(r.log) {
		entries = make([]types.LogEntry, len(r.log)-nextIdx)
		copy(entries, r.log[nextIdx:])
	}

	req := types.AppendEntriesRequest{
		Term:         r.currentTerm,
		LeaderID:     r.nodeID,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: r.commitIndex,
	}

	if len(entries) > 0 {
		fmt.Printf("%s📤 Sending AppendEntries to Node %d: %d entries (prevIdx=%d, prevTerm=%d)\n",
			r.prefix, followerID, len(entries), prevLogIndex, prevLogTerm)
	}

	if r.OnSendAppendEntries != nil {
		go r.OnSendAppendEntries(followerID, req)
	}
}

// waitForMajority blocks until the target entry is committed by majority or timeout.
func (r *RaftLog) waitForMajority(targetIndex int) bool {
	deadline := time.After(ReplicationTimeout)
	totalNodes := len(r.peers) + 1
	majority := (totalNodes / 2) + 1

	fmt.Printf("%s⏳ Waiting for majority (%d/%d nodes) to replicate entry #%d...\n",
		r.prefix, majority, totalNodes, targetIndex)

	for {
		select {
		case resp := <-r.AppendEntriesRespCh:
			r.handleAppendEntriesResponse(resp)

			r.mu.Lock()
			if r.commitIndex >= targetIndex {
				r.applyCommitted()
				r.mu.Unlock()

				fmt.Printf("%s✅ ═══════════════════════════════════════════\n", r.prefix)
				fmt.Printf("%s✅ ENTRY #%d COMMITTED (majority reached)\n", r.prefix, targetIndex)
				fmt.Printf("%s✅ ═══════════════════════════════════════════\n", r.prefix)
				return true
			}
			r.mu.Unlock()

		case <-deadline:
			fmt.Printf("%s❌ Timeout waiting for majority replication of entry #%d\n",
				r.prefix, targetIndex)
			return false

		case <-r.stopCh:
			return false
		}
	}
}

// handleAppendEntriesResponse processes a response from a follower.
func (r *RaftLog) handleAppendEntriesResponse(resp types.AppendEntriesResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If the follower's term is higher, step down
	if resp.Term > r.currentTerm {
		fmt.Printf("%s📉 Received higher term %d from Node %d. Stepping down.\n",
			r.prefix, resp.Term, resp.FollowerID)
		r.isLeader = false
		r.currentTerm = resp.Term
		return
	}

	if resp.Success {
		// Update matchIndex and nextIndex for this follower
		r.matchIndex[resp.FollowerID] = resp.MatchIndex
		r.nextIndex[resp.FollowerID] = resp.MatchIndex + 1

		fmt.Printf("%s   ✅ Node %d replicated up to index %d\n",
			r.prefix, resp.FollowerID, resp.MatchIndex)

		// Try to advance commit index
		r.tryAdvanceCommitIndex()
	} else {
		// Decrement nextIndex and retry (log inconsistency)
		if r.nextIndex[resp.FollowerID] > 0 {
			r.nextIndex[resp.FollowerID]--
		}
		fmt.Printf("%s   ❌ Node %d rejected AppendEntries. Retrying with nextIndex=%d\n",
			r.prefix, resp.FollowerID, r.nextIndex[resp.FollowerID])

		// Retry with decremented nextIndex
		r.sendAppendEntriesTo(resp.FollowerID)
	}
}

// tryAdvanceCommitIndex checks if the commit index can be advanced.
// Raft rule: a log entry is committed if stored on a majority of servers
// AND the entry's term matches the leader's current term. Must hold r.mu.
func (r *RaftLog) tryAdvanceCommitIndex() {
	for n := len(r.log) - 1; n > r.commitIndex; n-- {
		if r.log[n].Term != r.currentTerm {
			continue
		}

		// Count how many nodes have this entry (leader always has it)
		count := 1
		for _, pid := range r.peers {
			if r.matchIndex[pid] >= n {
				count++
			}
		}

		totalNodes := len(r.peers) + 1
		majority := (totalNodes / 2) + 1

		if count >= majority {
			oldCommit := r.commitIndex
			r.commitIndex = n
			fmt.Printf("%s📊 Commit index advanced: %d → %d (replicated on %d/%d nodes)\n",
				r.prefix, oldCommit, n, count, totalNodes)
			break
		}
	}
}

// applyCommitted applies all committed but not-yet-applied entries to the
// state machine. Must hold r.mu.
func (r *RaftLog) applyCommitted() {
	for r.lastApplied < r.commitIndex {
		r.lastApplied++
		entry := r.log[r.lastApplied]
		r.applyEntry(entry)
	}
}

// applyEntry applies a single log entry to the state machine. Must hold r.mu.
func (r *RaftLog) applyEntry(entry types.LogEntry) {
	switch entry.Change {
	case types.ChangeAddRoom:
		r.RoutingTable.Rooms[entry.Key] = &types.RoutingEntry{
			RoomName:   entry.Key,
			ServerAddr: entry.Value,
			CreatedAt:  time.Now(),
		}
		fmt.Printf("%s📝 Applied entry #%d to state machine: Room \"%s\" → %s\n",
			r.prefix, entry.Index, entry.Key, entry.Value)

	case types.ChangeAddServer:
		r.RoutingTable.Servers[entry.Key] = entry.Value
		fmt.Printf("%s📝 Applied entry #%d to state machine: Server \"%s\" → %s\n",
			r.prefix, entry.Index, entry.Key, entry.Value)
	}
}

// ============================================================
// Follower-side methods
// ============================================================

// HandleAppendEntries processes an AppendEntries RPC from the leader.
// This implements the follower-side of Raft log replication:
//  1. Reject if sender's term < receiver's current term.
//  2. Reject if log doesn't contain entry at prevLogIndex with prevLogTerm.
//  3. Delete conflicting entries and append any new entries.
//  4. Update commitIndex if leader's commitIndex is higher.
func (r *RaftLog) HandleAppendEntries(req types.AppendEntriesRequest) {
	r.mu.Lock()

	// Rule 1: Reply false if term < currentTerm (§5.1)
	if req.Term < r.currentTerm {
		fmt.Printf("%s❌ Rejected AppendEntries from Node %d (term %d < our term %d)\n",
			r.prefix, req.LeaderID, req.Term, r.currentTerm)
		resp := types.AppendEntriesResponse{
			Term:       r.currentTerm,
			Success:    false,
			FollowerID: r.nodeID,
			MatchIndex: -1,
		}
		r.mu.Unlock()
		if r.OnSendAppendEntriesResp != nil {
			r.OnSendAppendEntriesResp(req.LeaderID, resp)
		}
		return
	}

	r.currentTerm = req.Term

	// Rule 2: Reply false if log doesn't contain entry at prevLogIndex
	// whose term matches prevLogTerm (§5.3)
	if req.PrevLogIndex >= 0 {
		if req.PrevLogIndex >= len(r.log) {
			fmt.Printf("%s❌ Log too short: prevLogIndex=%d but log length=%d\n",
				r.prefix, req.PrevLogIndex, len(r.log))
			resp := types.AppendEntriesResponse{
				Term:       r.currentTerm,
				Success:    false,
				FollowerID: r.nodeID,
				MatchIndex: len(r.log) - 1,
			}
			r.mu.Unlock()
			if r.OnSendAppendEntriesResp != nil {
				r.OnSendAppendEntriesResp(req.LeaderID, resp)
			}
			return
		}
		if r.log[req.PrevLogIndex].Term != req.PrevLogTerm {
			fmt.Printf("%s❌ Log mismatch at index %d: expected term %d, got term %d. Truncating.\n",
				r.prefix, req.PrevLogIndex, req.PrevLogTerm, r.log[req.PrevLogIndex].Term)
			// Delete the conflicting entry and everything after it
			r.log = r.log[:req.PrevLogIndex]
			resp := types.AppendEntriesResponse{
				Term:       r.currentTerm,
				Success:    false,
				FollowerID: r.nodeID,
				MatchIndex: len(r.log) - 1,
			}
			r.mu.Unlock()
			if r.OnSendAppendEntriesResp != nil {
				r.OnSendAppendEntriesResp(req.LeaderID, resp)
			}
			return
		}
	}

	// Rule 3: If an existing entry conflicts with a new one (same index but
	// different terms), delete the existing entry and all that follow it.
	// Append any new entries not already in the log. (§5.3)
	insertIdx := req.PrevLogIndex + 1
	for i, entry := range req.Entries {
		logIdx := insertIdx + i
		if logIdx < len(r.log) {
			if r.log[logIdx].Term != entry.Term {
				// Conflict detected — truncate and append remaining
				r.log = r.log[:logIdx]
				r.log = append(r.log, req.Entries[i:]...)
				break
			}
			// Entry already exists with same term, skip
		} else {
			// Append all remaining new entries
			r.log = append(r.log, req.Entries[i:]...)
			break
		}
	}

	if len(req.Entries) > 0 {
		fmt.Printf("%s📥 Appended %d entries from leader (log length now: %d)\n",
			r.prefix, len(req.Entries), len(r.log))
	}

	// Rule 4: If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry) (§5.3)
	if req.LeaderCommit > r.commitIndex {
		oldCommit := r.commitIndex
		lastNewIdx := len(r.log) - 1
		if req.LeaderCommit < lastNewIdx {
			r.commitIndex = req.LeaderCommit
		} else {
			r.commitIndex = lastNewIdx
		}
		if r.commitIndex > oldCommit {
			fmt.Printf("%s📊 Commit index updated: %d → %d\n",
				r.prefix, oldCommit, r.commitIndex)
			r.applyCommitted()
		}
	}

	matchIdx := len(r.log) - 1
	resp := types.AppendEntriesResponse{
		Term:       r.currentTerm,
		Success:    true,
		FollowerID: r.nodeID,
		MatchIndex: matchIdx,
	}

	r.mu.Unlock()

	if r.OnSendAppendEntriesResp != nil {
		r.OnSendAppendEntriesResp(req.LeaderID, resp)
	}
}

// ============================================================
// Query methods
// ============================================================

// GetLog returns a copy of the replicated log.
func (r *RaftLog) GetLog() []types.LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]types.LogEntry, len(r.log))
	copy(copied, r.log)
	return copied
}

// GetCommitIndex returns the current commit index.
func (r *RaftLog) GetCommitIndex() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.commitIndex
}

// GetRoutingTable returns the routing table (state machine).
func (r *RaftLog) GetRoutingTable() *types.RoutingTable {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.RoutingTable
}

// Stop signals the Raft log module to stop.
func (r *RaftLog) Stop() {
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
}
