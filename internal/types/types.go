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
	MsgVoteRequest  MessageType = "VOTE_REQUEST"
	MsgVoteResponse MessageType = "VOTE_RESPONSE"
	MsgHeartbeat    MessageType = "HEARTBEAT"

	MsgPrepareRequest  MessageType = "PREPARE_REQUEST"
	MsgPrepareResponse MessageType = "PREPARE_RESPONSE"
	MsgCommitRequest   MessageType = "COMMIT_REQUEST"
	MsgAbortRequest    MessageType = "ABORT_REQUEST"
	MsgCommitAck       MessageType = "COMMIT_ACK"

	MsgLockRequest  MessageType = "LOCK_REQUEST"
	MsgLockResponse MessageType = "LOCK_RESPONSE"
	MsgLockRelease  MessageType = "LOCK_RELEASE"

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

type ChangeType string

const (
	ChangeAddServer ChangeType = "ADD_SERVER"
	ChangeAddRoom   ChangeType = "ADD_ROOM"
)

type PrepareRequest struct {
	TransactionID string     `json:"transaction_id"`
	Change        ChangeType `json:"change_type"`
	Key           string     `json:"key"`
	Value         string     `json:"value"`
}

type PrepareResponse struct {
	TransactionID string `json:"transaction_id"`
	VoteCommit    bool   `json:"vote_commit"`
	ParticipantID int    `json:"participant_id"`
}

type CommitRequest struct {
	TransactionID string `json:"transaction_id"`
}

type AbortRequest struct {
	TransactionID string `json:"transaction_id"`
	Reason        string `json:"reason"`
}

type LockRequest struct {
	Resource  string `json:"resource"`
	Requester string `json:"requester"`
}

type LockResponse struct {
	Resource    string    `json:"resource"`
	Granted     bool      `json:"granted"`
	Holder      string    `json:"holder"`
	ExpiresAt   time.Time `json:"expires_at"`
	Reason      string    `json:"reason"`
}

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
