package election

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

const (
	HeartbeatInterval = 100 * time.Millisecond

	ElectionTimeoutMin = 300 * time.Millisecond
	ElectionTimeoutMax = 500 * time.Millisecond
)

type ElectionNode struct {
	mu sync.Mutex

	ID    int
	Peers []int

	// Raft States
	CurrentTerm int
	VotedFor    int
	State       types.NodeState
	voteCount   int

	LeaderID int

	lastHeartbeat   time.Time
	electionTimeout time.Duration

	VoteRequestCh  chan types.VoteRequest
	VoteResponseCh chan types.VoteResponse
	HeartbeatCh    chan types.Heartbeat

	OnBecomeLeader   func(term int)
	OnBecomeFollower func(leaderID, term int)
	OnSendVoteReq    func(req types.VoteRequest)
	OnSendHeartbeat  func(hb types.Heartbeat)
	OnSendVoteResp   func(targetID int, resp types.VoteResponse)

	stopCh chan struct{}
	prefix string
}

func NewElectionNode(id int, peers []int) *ElectionNode {
	return &ElectionNode{
		ID:              id,
		Peers:           peers,
		CurrentTerm:     0,
		VotedFor:        -1,
		State:           types.Follower,
		LeaderID:        -1,
		voteCount:       0,
		lastHeartbeat:   time.Now(),
		electionTimeout: randomElectionTimeout(),
		VoteRequestCh:   make(chan types.VoteRequest, 100),
		VoteResponseCh:  make(chan types.VoteResponse, 100),
		HeartbeatCh:     make(chan types.Heartbeat, 100),
		stopCh:          make(chan struct{}),
		prefix:          types.LogPrefix(id, "ELECTION"),
	}
}

func (e *ElectionNode) Start() {
	go e.run()
}

func (e *ElectionNode) Stop() {
	select {
	case <-e.stopCh:
	default:
		close(e.stopCh)
	}
}

func (e *ElectionNode) GetState() (types.NodeState, int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.State, e.CurrentTerm, e.LeaderID
}

func (e *ElectionNode) IsLeader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.State == types.Leader
}

func (e *ElectionNode) run() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return

		case <-ticker.C:
			e.checkElectionTimeout()

		case req := <-e.VoteRequestCh:
			e.handleVoteRequest(req)

		case resp := <-e.VoteResponseCh:
			e.handleVoteResponse(resp)

		case hb := <-e.HeartbeatCh:
			e.handleHeartbeat(hb)
		}
	}
}

func (e *ElectionNode) checkElectionTimeout() {
	e.mu.Lock()

	if e.State == types.Leader {
		e.mu.Unlock()
		e.sendHeartbeat()
		return
	}

	if time.Since(e.lastHeartbeat) > e.electionTimeout {
		fmt.Printf("%s⏱ Election timeout! No heartbeat for %v. Starting election...\n",
			e.prefix, e.electionTimeout)
		e.startElection()
	}
	e.mu.Unlock()
}

func (e *ElectionNode) startElection() {
	e.CurrentTerm++
	e.State = types.Candidate
	e.VotedFor = e.ID
	e.voteCount = 1
	e.lastHeartbeat = time.Now()
	e.electionTimeout = randomElectionTimeout()

	term := e.CurrentTerm
	fmt.Printf("%s🗳  Started election for term %d (voted for self, votes: 1)\n",
		e.prefix, term)

	req := types.VoteRequest{
		Term:        term,
		CandidateID: e.ID,
	}

	if e.OnSendVoteReq != nil {
		go e.OnSendVoteReq(req)
	}
}

func (e *ElectionNode) handleVoteRequest(req types.VoteRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if req.Term > e.CurrentTerm {
		fmt.Printf("%s📩 Received VoteRequest from Node %d for term %d (higher than our term %d). Stepping down.\n",
			e.prefix, req.CandidateID, req.Term, e.CurrentTerm)
		e.CurrentTerm = req.Term
		e.State = types.Follower
		e.VotedFor = -1
		e.LeaderID = -1
	}

	granted := false
	if req.Term >= e.CurrentTerm && (e.VotedFor == -1 || e.VotedFor == req.CandidateID) {
		granted = true
		e.VotedFor = req.CandidateID
		e.lastHeartbeat = time.Now()
		fmt.Printf("%s✅ Voted for Node %d in term %d\n",
			e.prefix, req.CandidateID, req.Term)
	} else {
		fmt.Printf("%s❌ Denied vote to Node %d in term %d (already voted for %d)\n",
			e.prefix, req.CandidateID, req.Term, e.VotedFor)
	}

	resp := types.VoteResponse{
		Term:        e.CurrentTerm,
		VoteGranted: granted,
		VoterID:     e.ID,
	}

	if e.OnSendVoteResp != nil {
		go e.OnSendVoteResp(req.CandidateID, resp)
	}
}

func (e *ElectionNode) handleVoteResponse(resp types.VoteResponse) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.State != types.Candidate {
		return
	}

	if resp.Term > e.CurrentTerm {
		fmt.Printf("%s📉 Received higher term %d from Node %d. Stepping down to Follower.\n",
			e.prefix, resp.Term, resp.VoterID)
		e.CurrentTerm = resp.Term
		e.State = types.Follower
		e.VotedFor = -1
		e.voteCount = 0
		return
	}

	if resp.Term != e.CurrentTerm {
		return
	}

	if resp.VoteGranted {
		e.voteCount++
		fmt.Printf("%s🎉 Received vote from Node %d for term %d (total votes: %d)\n",
			e.prefix, resp.VoterID, resp.Term, e.voteCount)

		totalNodes := len(e.Peers) + 1
		majority := (totalNodes / 2) + 1

		if e.voteCount >= majority {
			e.becomeLeader()
		}
	}
}

func (e *ElectionNode) becomeLeader() {
	e.State = types.Leader
	e.LeaderID = e.ID

	fmt.Printf("%s👑 ═══════════════════════════════════════════\n", e.prefix)
	fmt.Printf("%s👑 BECAME LEADER for term %d (received %d votes)\n", e.prefix, e.CurrentTerm, e.voteCount)
	fmt.Printf("%s👑 ═══════════════════════════════════════════\n", e.prefix)

	if e.OnBecomeLeader != nil {
		go e.OnBecomeLeader(e.CurrentTerm)
	}
}

func (e *ElectionNode) handleHeartbeat(hb types.Heartbeat) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if hb.Term < e.CurrentTerm {
		return
	}

	if hb.Term > e.CurrentTerm {
		e.CurrentTerm = hb.Term
		e.VotedFor = -1
	}

	wasFollowerOfSameLeader := (e.State == types.Follower && e.LeaderID == hb.LeaderID)

	if e.State != types.Follower {
		fmt.Printf("%s📥 Stepping down to Follower (received heartbeat from Leader %d, term %d)\n",
			e.prefix, hb.LeaderID, hb.Term)
	}

	e.State = types.Follower
	e.LeaderID = hb.LeaderID
	e.lastHeartbeat = time.Now()
	e.voteCount = 0

	if !wasFollowerOfSameLeader && e.OnBecomeFollower != nil {
		go e.OnBecomeFollower(hb.LeaderID, hb.Term)
	}
}

func (e *ElectionNode) sendHeartbeat() {
	e.mu.Lock()
	hb := types.Heartbeat{
		Term:     e.CurrentTerm,
		LeaderID: e.ID,
	}
	e.mu.Unlock()

	if e.OnSendHeartbeat != nil {
		e.OnSendHeartbeat(hb)
	}
}

func randomElectionTimeout() time.Duration {
	spread := ElectionTimeoutMax - ElectionTimeoutMin
	return ElectionTimeoutMin + time.Duration(rand.Int63n(int64(spread)))
}
