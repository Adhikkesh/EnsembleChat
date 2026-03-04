package client

import (
	"distributed-chat-coordinator/internal/types"
	"fmt"
)

type ChatClient struct {
	ID     string
	prefix string
}

func NewChatClient(id string) *ChatClient {
	nodeNum := 0
	if len(id) > 0 {
		nodeNum = int(id[len(id)-1]) % 6
	}
	return &ChatClient{
		ID:     id,
		prefix: types.LogPrefix(nodeNum, "CLIENT-"+id),
	}
}

func (c *ChatClient) JoinRoom(roomName string) {
	fmt.Printf("%s🚪 Joining room \"%s\"\n", c.prefix, roomName)
}

func (c *ChatClient) SendMessage(roomName, content string) {
	fmt.Printf("%s📤 Sending to \"%s\": %s\n", c.prefix, roomName, content)
}

func (c *ChatClient) ReceiveMessage(roomName, sender, content string) {
	fmt.Printf("%s📩 [%s] %s: %s\n", c.prefix, roomName, sender, content)
}
