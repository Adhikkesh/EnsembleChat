package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"distributed-chat-coordinator/internal/client"
	"distributed-chat-coordinator/internal/types"
)

func main() {
	id := flag.String("id", "alice", "Client username")
	flag.Parse()

	fmt.Printf("%s[START] Starting Chat Client \"%s\"%s\n",
		types.ColorCyan+types.ColorBold, *id, types.ColorReset)

	c := client.NewChatClient(*id)

	c.JoinRoom("Lobby")
	c.SendMessage("Lobby", "Hello everyone!")

	fmt.Printf("%s[RPC] Client \"%s\" running. Press Ctrl+C to stop.%s\n",
		types.ColorCyan, *id, types.ColorReset)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\n%s[STOP] Shutting down Client \"%s\"...%s\n",
		types.ColorYellow, *id, types.ColorReset)

	_ = c
}
