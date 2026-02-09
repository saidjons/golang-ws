package main

import (
	"fmt"
	"net/http"
	"ws-gemini/types"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Global broadcast channel
var broadcast = make(chan types.Message)

func main() {
	// Start the "Hub" in the background
	go handleMessages()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// 1. Create Client
		client := &types.Client{Conn: conn, Send: make(chan types.WSMessage, 256)}

		// 2. Add to Pool
		client.AddtoPool()

		// 3. Replay History (Safe to do before starting pumps)
		types.HistoryMu.Lock()
		for _, oldMsg := range types.History {
			// Note: Direct write is okay here because pumps aren't running yet
			conn.WriteMessage(websocket.TextMessage, oldMsg)
		}
		types.HistoryMu.Unlock()

		fmt.Printf("Client connected %d\n", len(types.Clients))
		fmt.Printf("Remote Address: %s\n", conn.RemoteAddr())

		// 4. Start the Write Pump (Background Worker)
		// This handles Pings and writing messages to the user.
		go client.WritePump()

		// 5. Start the Read Pump (BLOCKING)
		// CRITICAL: We do NOT use 'go' here. We want this to block.
		// We pass 'broadcast' so the package knows where to send messages.
		client.ReadPump(broadcast)

		// When ReadPump finishes (user disconnects), the function exits
		// and the defer inside ReadPump cleans everything up.
	})

	fmt.Println("Server started on :8080")
	http.ListenAndServe(":8080", nil)
}

func handleMessages() {
	for {
		msg := <-broadcast

		// Save to History
		types.HistoryMu.Lock()

		types.History = append(types.History, msg)

		if len(types.History) > 10 {
			types.History = types.History[1:]
		}

		types.HistoryMu.Unlock()

		// Broadcast to all clients
		types.ClientsMu.Lock()
		for _, client := range types.Clients {
			if client == msg.Sender {
				continue
			}

			select {
			case client.Send <- msg:
				// Message sent successfully
				fmt.Printf("Sent message to client %s\n", client.Conn.RemoteAddr())
				// Optionally, you can log the message content here
			default:
				close(client.Send)
				delete(types.Clients, client.Conn)
			}
		}
		types.ClientsMu.Unlock()
	}
}
