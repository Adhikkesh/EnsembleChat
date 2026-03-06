package client

import (
	"fmt"
	"net"
	"net/rpc"
	"time"

	"distributed-chat-coordinator/internal/chatserver"
	"distributed-chat-coordinator/internal/types"
)

type ChatClient struct {
	ID     string
	prefix string

	serverAddr string
	rpcClient  *rpc.Client
	lastSeen   map[string]int // roomName → last message index seen
}

func NewChatClient(id string) *ChatClient {
	nodeNum := 0
	if len(id) > 0 {
		nodeNum = int(id[len(id)-1]) % 6
	}
	return &ChatClient{
		ID:       id,
		prefix:   types.LogPrefix(nodeNum, "CLIENT-"+id),
		lastSeen: make(map[string]int),
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// Original in-process methods (kept for backward compatibility with demos)
// ══════════════════════════════════════════════════════════════════════════════

func (c *ChatClient) JoinRoom(roomName string) {
	fmt.Printf("%s🚪 Joining room \"%s\"\n", c.prefix, roomName)
}

func (c *ChatClient) SendMessage(roomName, content string) {
	fmt.Printf("%s📤 Sending to \"%s\": %s\n", c.prefix, roomName, content)
}

func (c *ChatClient) ReceiveMessage(roomName, sender, content string) {
	fmt.Printf("%s📩 [%s] %s: %s\n", c.prefix, roomName, sender, content)
}

// ══════════════════════════════════════════════════════════════════════════════
// RPC methods — connect to a real chat server over TCP
// ══════════════════════════════════════════════════════════════════════════════

// ConnectToServer dials the chat server at the given address.
func (c *ChatClient) ConnectToServer(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("client %s: failed to connect to %s: %w", c.ID, addr, err)
	}
	c.rpcClient = rpc.NewClient(conn)
	c.serverAddr = addr
	fmt.Printf("%s🔗 Connected to chat server at %s\n", c.prefix, addr)
	return nil
}

// JoinRoomRPC joins a room on the connected server via RPC.
func (c *ChatClient) JoinRoomRPC(roomName string) error {
	if c.rpcClient == nil {
		return fmt.Errorf("not connected to any server")
	}
	args := &chatserver.JoinRoomArgs{ClientID: c.ID, RoomName: roomName}
	var reply chatserver.JoinRoomReply
	if err := c.rpcClient.Call("ChatService.JoinRoom", args, &reply); err != nil {
		return fmt.Errorf("JoinRoom RPC failed: %w", err)
	}
	fmt.Printf("%s🚪 Joined room \"%s\" (server: %s)\n", c.prefix, roomName, c.serverAddr)
	c.lastSeen[roomName] = 0
	return nil
}

// SendMessageRPC sends a message to a room on the connected server via RPC.
func (c *ChatClient) SendMessageRPC(roomName, content string) error {
	if c.rpcClient == nil {
		return fmt.Errorf("not connected to any server")
	}
	args := &chatserver.SendMessageArgs{RoomName: roomName, Sender: c.ID, Content: content}
	var reply chatserver.SendMessageReply
	if err := c.rpcClient.Call("ChatService.SendMessage", args, &reply); err != nil {
		return fmt.Errorf("SendMessage RPC failed: %w", err)
	}
	fmt.Printf("%s📤 Sent to \"%s\": %s\n", c.prefix, roomName, content)
	return nil
}

// PollMessages fetches new messages from the server for the given room.
// Returns only messages the client hasn't seen before.
func (c *ChatClient) PollMessages(roomName string) ([]chatserver.ChatMessage, error) {
	if c.rpcClient == nil {
		return nil, fmt.Errorf("not connected to any server")
	}
	after := c.lastSeen[roomName]
	args := &chatserver.GetMessagesArgs{RoomName: roomName, After: after}
	var reply chatserver.GetMessagesReply
	if err := c.rpcClient.Call("ChatService.GetMessages", args, &reply); err != nil {
		return nil, fmt.Errorf("GetMessages RPC failed: %w", err)
	}
	c.lastSeen[roomName] = reply.Total
	return reply.Messages, nil
}

// Disconnect closes the RPC connection.
func (c *ChatClient) Disconnect() {
	if c.rpcClient != nil {
		c.rpcClient.Close()
		fmt.Printf("%s🔌 Disconnected from server\n", c.prefix)
	}
}
