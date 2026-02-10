package main

import (
	"fmt"
	"net/http"
	"ws-gemini/types"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// FIX: Channel carries the Internal 'Message' struct
var broadcast = make(chan types.Message)

func main() {
	go handleMessages()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// 1. Initialize Client with the WSMessage channel
		client := &types.Client{
			Conn: conn,
			Send: make(chan types.WSMessage, 256), // <--- MATCHES STRUCT
		}
		client.AddtoPool()

		// 2. Replay History
		types.HistoryMu.Lock()
		for _, oldMsg := range types.History {
			// FIX: Use WriteJSON because oldMsg is a Struct now
			conn.WriteJSON(oldMsg)
		}
		types.HistoryMu.Unlock()

		fmt.Printf("Client connected. Total: %d\n", len(types.Clients))

		go client.WritePump()
		client.ReadPump(broadcast)
	})

	fmt.Println("Server started on :8080")
	http.ListenAndServe(":8080", nil)
}

func handleMessages() {
	for {
		// 1. Receive the Internal Message
		internalMsg := <-broadcast

		// 2. Extract the payload
		payload := internalMsg.Payload
		// Use .Client instead of .SenderClient
		payload.Sender = internalMsg.Client.Conn.RemoteAddr().String()

		// 3. Save to History
		types.HistoryMu.Lock()
		types.History = append(types.History, payload)
		if len(types.History) > 10 {
			types.History = types.History[1:]
		}
		types.HistoryMu.Unlock()

		// 4. Broadcast
		types.ClientsMu.Lock()
		for _, client := range types.Clients {
			// FIX: Check against .Client
			if client == internalMsg.Client {
				continue
			}

			select {
			case client.Send <- payload:
			default:
				close(client.Send)
				delete(types.Clients, client.Conn)
			}
		}
		types.ClientsMu.Unlock()
	}
}
