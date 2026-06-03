package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Thread-safe clients map
var clients = make(map[*websocket.Conn]bool)
var clientsMu sync.Mutex

var broadcast = make(chan string)

var upgrader = websocket.Upgrader{
	// Allow all origins for development
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}
	defer ws.Close()

	log.Println("✅ Client connected:", r.RemoteAddr)

	clientsMu.Lock()
	clients[ws] = true
	clientsMu.Unlock()

	for {
		_, msgBytes, err := ws.ReadMessage()
		if err != nil {
			log.Printf("🔴 Read error from %s: %v", r.RemoteAddr, err)
			clientsMu.Lock()
			delete(clients, ws)
			clientsMu.Unlock()
			break
		}
		msg := string(msgBytes)
		log.Printf("📨 Message from %s: %s", r.RemoteAddr, msg)

		// Echo
		if err := ws.WriteMessage(websocket.TextMessage, []byte("Echo: "+msg)); err != nil {
			log.Printf("🔴 Write error: %v", err)
		}

		broadcast <- msg
	}
}

func handleMessages() {
	for msg := range broadcast {
		clientsMu.Lock()
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("❌ Broadcast write failed: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
		clientsMu.Unlock()
	}
}

func main() {
	http.HandleFunc("/ws", handleConnections)
	go handleMessages()

	log.Println("🚀 Server started at :8081")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
