package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

/* ---------- 1.  hub keeps all clients ---------- */
type Hub struct {
	sync.Mutex
	clients map[*Client]bool
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan json.RawMessage // buffered outbound JSON
}

var hub = &Hub{clients: make(map[*Client]bool)}

/* ---------- 2.  register / unregister ---------- */
func (h *Hub) register(c *Client) {
	h.Lock()
	h.clients[c] = true
	h.Unlock()
	log.Println("client joined, total:", len(h.clients))
}

func (h *Hub) unregister(c *Client) {
	h.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.Unlock()
	log.Println("client left, total:", len(h.clients))
}

/* ---------- 3.  broadcast to every client ---------- */
func (h *Hub) broadcast(msg json.RawMessage, sender *Client) {
	h.Lock()
	for c := range h.clients {
		if c == sender { continue } // optional: skip sender
		select {
		case c.send <- msg:
		default: // client too slow, drop
			close(c.send)
			delete(h.clients, c)
		}
	}
	h.Unlock()
}

/* ---------- 4.  upgrade handler ---------- */
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { log.Print("upgrade:", err); return }

	client := &Client{hub: hub, conn: conn, send: make(chan json.RawMessage, 256)}
	hub.register(client)

	// read loop
	go func() {
		defer func() { hub.unregister(client); conn.Close() }()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil { log.Println("read:", err); return }
			hub.broadcast(msg, client)
		}
	}()

	// write loop
	go func() {
		for msg := range client.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				break
			}
		}
	}()
}

func main() {
	http.HandleFunc("/ws", wsHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}