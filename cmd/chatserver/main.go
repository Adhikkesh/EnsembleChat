package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"distributed-chat-coordinator/internal/chatserver"
	"distributed-chat-coordinator/internal/types"
)

func main() {
	id := flag.String("id", "server1", "Unique server identifier")
	addr := flag.String("addr", ":9001", "Listen address for client connections")
	flag.Parse()

	fmt.Printf("%s🚀 Starting Chat Server \"%s\" at %s%s\n",
		types.ColorBlue+types.ColorBold, *id, *addr, types.ColorReset)

	server := chatserver.NewChatServer(*id, *addr)

	server.CreateRoom("Lobby")

	fmt.Printf("%s📡 Chat Server \"%s\" running. Press Ctrl+C to stop.%s\n",
		types.ColorCyan, *id, types.ColorReset)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\n%s🛑 Shutting down Chat Server \"%s\"...%s\n",
		types.ColorYellow, *id, types.ColorReset)

	_ = server
}
