package chatserver

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

// ══════════════════════════════════════════════════════════════════════════════
// RPC request / reply types for the chat service
// ══════════════════════════════════════════════════════════════════════════════

type JoinRoomArgs struct {
	ClientID string
	RoomName string
}

type JoinRoomReply struct {
	OK      bool
	Message string
}

type SendMessageArgs struct {
	RoomName string
	Sender   string
	Content  string
}

type SendMessageReply struct {
	OK bool
}

type RelayMessageArgs struct {
	RoomName string
	Sender   string
	Content  string
}

type RelayMessageReply struct {
	OK bool
}

type GetMessagesArgs struct {
	RoomName string
	After    int // return messages with index >= After
}

type GetMessagesReply struct {
	Messages []ChatMessage
	Total    int
}

// ══════════════════════════════════════════════════════════════════════════════
// Core types
// ══════════════════════════════════════════════════════════════════════════════

type ChatRoom struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`

	mu       sync.Mutex
	messages []ChatMessage
	members  map[string]bool
}

type ChatMessage struct {
	Sender    string    `json:"sender"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func NewChatRoom(name string) *ChatRoom {
	return &ChatRoom{
		Name:      name,
		CreatedAt: time.Now(),
		messages:  make([]ChatMessage, 0),
		members:   make(map[string]bool),
	}
}

func (r *ChatRoom) AddMessage(sender, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, ChatMessage{
		Sender:    sender,
		Content:   content,
		Timestamp: time.Now(),
	})
}

func (r *ChatRoom) Join(memberID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members[memberID] = true
}

func (r *ChatRoom) GetMessages() []ChatMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]ChatMessage, len(r.messages))
	copy(copied, r.messages)
	return copied
}

func (r *ChatRoom) GetMessagesAfter(after int) []ChatMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	if after >= len(r.messages) {
		return nil
	}
	result := make([]ChatMessage, len(r.messages)-after)
	copy(result, r.messages[after:])
	return result
}

func (r *ChatRoom) MessageCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

func (r *ChatRoom) GetMembers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	members := make([]string, 0, len(r.members))
	for m := range r.members {
		members = append(members, m)
	}
	return members
}

// ══════════════════════════════════════════════════════════════════════════════
// ChatServer
// ══════════════════════════════════════════════════════════════════════════════

type ChatServer struct {
	mu sync.Mutex

	ID     string
	Addr   string
	prefix string

	rooms   map[string]*ChatRoom
	clients map[string]bool

	listener net.Listener

	// OnMessageReceived is called when a message arrives via RPC.
	// The demo wires this to forward messages to other servers hosting the same room.
	OnMessageReceived func(serverID, roomName, sender, content string)
}

func NewChatServer(id, addr string) *ChatServer {
	nodeNum := 0
	if len(id) > 0 {
		nodeNum = int(id[len(id)-1]) % 6
	}
	return &ChatServer{
		ID:      id,
		Addr:    addr,
		prefix:  types.LogPrefix(nodeNum, "CHAT-SRV-"+id),
		rooms:   make(map[string]*ChatRoom),
		clients: make(map[string]bool),
	}
}

func (s *ChatServer) CreateRoom(name string) *ChatRoom {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.rooms[name]; ok {
		fmt.Printf("%s⚠️  Room \"%s\" already exists on this server\n", s.prefix, name)
		return existing
	}

	room := NewChatRoom(name)
	s.rooms[name] = room

	fmt.Printf("%s🏠 Created room \"%s\"\n", s.prefix, name)
	return room
}

func (s *ChatServer) GetRoom(name string) *ChatRoom {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms[name]
}

func (s *ChatServer) ListRooms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.rooms))
	for name := range s.rooms {
		names = append(names, name)
	}
	return names
}

func (s *ChatServer) ConnectClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[clientID] = true
	fmt.Printf("%s👤 Client \"%s\" connected\n", s.prefix, clientID)
}

func (s *ChatServer) SendMessage(roomName, sender, content string) bool {
	s.mu.Lock()
	room, ok := s.rooms[roomName]
	s.mu.Unlock()

	if !ok {
		fmt.Printf("%s⚠️  Room \"%s\" not found on this server\n", s.prefix, roomName)
		return false
	}

	room.AddMessage(sender, content)
	fmt.Printf("%s💬 [%s] %s: %s\n", s.prefix, roomName, sender, content)

	// Fire the callback so the demo can relay to other servers
	if s.OnMessageReceived != nil {
		s.OnMessageReceived(s.ID, roomName, sender, content)
	}

	return true
}

// RelayMessage stores a message received from another server WITHOUT
// triggering the OnMessageReceived callback (prevents infinite relay loops).
func (s *ChatServer) RelayMessage(roomName, sender, content string) bool {
	s.mu.Lock()
	room, ok := s.rooms[roomName]
	s.mu.Unlock()

	if !ok {
		return false
	}

	room.AddMessage(sender, content)
	fmt.Printf("%s📨 [RELAY] [%s] %s: %s\n", s.prefix, roomName, sender, content)
	return true
}

// ══════════════════════════════════════════════════════════════════════════════
// RPC Service — allows clients to connect over TCP
// ══════════════════════════════════════════════════════════════════════════════

type ChatService struct {
	server *ChatServer
}

func (svc *ChatService) JoinRoom(args *JoinRoomArgs, reply *JoinRoomReply) error {
	svc.server.mu.Lock()
	room, ok := svc.server.rooms[args.RoomName]
	svc.server.mu.Unlock()

	if !ok {
		// Auto-create the room if it doesn't exist on this server
		room = svc.server.CreateRoom(args.RoomName)
	}

	room.Join(args.ClientID)
	svc.server.ConnectClient(args.ClientID)

	fmt.Printf("%s🚪 %s joined room \"%s\" via RPC\n", svc.server.prefix, args.ClientID, args.RoomName)
	reply.OK = true
	reply.Message = fmt.Sprintf("Joined room \"%s\" on server %s", args.RoomName, svc.server.ID)
	return nil
}

func (svc *ChatService) SendMessage(args *SendMessageArgs, reply *SendMessageReply) error {
	ok := svc.server.SendMessage(args.RoomName, args.Sender, args.Content)
	reply.OK = ok
	return nil
}

// RelayMessage RPC — used for cross-server relay (does NOT trigger callback)
func (svc *ChatService) RelayMessage(args *RelayMessageArgs, reply *RelayMessageReply) error {
	ok := svc.server.RelayMessage(args.RoomName, args.Sender, args.Content)
	reply.OK = ok
	return nil
}

func (svc *ChatService) GetMessages(args *GetMessagesArgs, reply *GetMessagesReply) error {
	svc.server.mu.Lock()
	room, ok := svc.server.rooms[args.RoomName]
	svc.server.mu.Unlock()

	if !ok {
		reply.Messages = nil
		reply.Total = 0
		return nil
	}

	reply.Messages = room.GetMessagesAfter(args.After)
	reply.Total = room.MessageCount()
	return nil
}

// StartRPC opens a TCP listener and registers the ChatService for RPC calls.
func (s *ChatServer) StartRPC(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("chat server %s: failed to listen on %s: %w", s.ID, addr, err)
	}
	s.listener = ln

	server := rpc.NewServer()
	svc := &ChatService{server: s}
	server.RegisterName("ChatService", svc)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go server.ServeConn(conn)
		}
	}()

	fmt.Printf("%s📡 Chat RPC server started on %s\n", s.prefix, addr)
	return nil
}

// StopRPC closes the TCP listener.
func (s *ChatServer) StopRPC() {
	if s.listener != nil {
		s.listener.Close()
	}
}
