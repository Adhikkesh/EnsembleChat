package types

import (
	"fmt"
	"time"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
	ColorBold   = "\033[1m"
)

func LogPrefix(nodeID int, role string) string {
	colors := []string{ColorGreen, ColorBlue, ColorPurple, ColorCyan, ColorYellow, ColorRed}
	color := colors[nodeID%len(colors)]
	return fmt.Sprintf("%s%s[Node %d | %s]%s ", color, ColorBold, nodeID, role, ColorReset)
}

type MessageType string

const (
	// Raft Leader Election
	MsgVoteRequest  MessageType = "VOTE_REQUEST"
	MsgVoteResponse MessageType = "VOTE_RESPONSE"
	MsgHeartbeat    MessageType = "HEARTBEAT"

	// Raft Log Replication (Consensus)
	MsgAppendEntries     MessageType = "APPEND_ENTRIES"
	MsgAppendEntriesResp MessageType = "APPEND_ENTRIES_RESP"

	// Token Ring (Mutual Exclusion)
	MsgTokenPass MessageType = "TOKEN_PASS"

	// Application-level messages
	MsgRegisterServer MessageType = "REGISTER_SERVER"
	MsgCreateRoom     MessageType = "CREATE_ROOM"
	MsgChatMessage    MessageType = "CHAT_MESSAGE"
	MsgJoinRoom       MessageType = "JOIN_ROOM"
)

type Message struct {
	Type      MessageType `json:"type"`
	SenderID  int         `json:"sender_id"`
	Payload   interface{} `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}

// === Node States (Raft) ===

type NodeState int

const (
	Follower  NodeState = iota
	Candidate
	Leader
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "FOLLOWER"
	case Candidate:
		return "CANDIDATE"
	case Leader:
		return "LEADER"
	default:
		return "UNKNOWN"
	}
}

// === Raft Leader Election Types ===

type VoteRequest struct {
	Term        int `json:"term"`
	CandidateID int `json:"candidate_id"`
}

type VoteResponse struct {
	Term        int  `json:"term"`
	VoteGranted bool `json:"vote_granted"`
	VoterID     int  `json:"voter_id"`
}

type Heartbeat struct {
	Term     int `json:"term"`
	LeaderID int `json:"leader_id"`
}

// === Raft Log Replication Types (Consensus) ===

type ChangeType string

const (
	ChangeAddServer ChangeType = "ADD_SERVER"
	ChangeAddRoom   ChangeType = "ADD_ROOM"
)

type LogEntry struct {
	Term   int        `json:"term"`
	Index  int        `json:"index"`
	Change ChangeType `json:"change_type"`
	Key    string     `json:"key"`
	Value  string     `json:"value"`
}

type AppendEntriesRequest struct {
	Term         int        `json:"term"`
	LeaderID     int        `json:"leader_id"`
	PrevLogIndex int        `json:"prev_log_index"`
	PrevLogTerm  int        `json:"prev_log_term"`
	Entries      []LogEntry `json:"entries"`
	LeaderCommit int        `json:"leader_commit"`
}

type AppendEntriesResponse struct {
	Term       int  `json:"term"`
	Success    bool `json:"success"`
	FollowerID int  `json:"follower_id"`
	MatchIndex int  `json:"match_index"`
}

// === Token Ring Types (Mutual Exclusion) ===

type TokenMessage struct {
	SeqNum int `json:"seq_num"`
}

// === Lock Response (external API for critical section access) ===

type LockResponse struct {
	Resource string `json:"resource"`
	Granted  bool   `json:"granted"`
	Holder   string `json:"holder"`
	Reason   string `json:"reason"`
}

// === Routing Types ===

type RoutingEntry struct {
	RoomName   string    `json:"room_name"`
	ServerAddr string    `json:"server_addr"`
	CreatedAt  time.Time `json:"created_at"`
}

type RoutingTable struct {
	Servers map[string]string        `json:"servers"`
	Rooms   map[string]*RoutingEntry `json:"rooms"`
}

func NewRoutingTable() *RoutingTable {
	return &RoutingTable{
		Servers: make(map[string]string),
		Rooms:   make(map[string]*RoutingEntry),
	}
}
