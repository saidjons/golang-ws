package types

import (
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn     *websocket.Conn
	Send     chan WSMessage
	UserID   string
	Username string
	Rooms    map[*Room]bool
}

var (
	Clients   = make(map[string]*Client)
	ClientsMu sync.Mutex

	History   []WSMessage
	HistoryMu sync.Mutex
)

func (client *Client) AddtoPool() {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()
	Clients[client.UserID] = client
}

func (client *Client) RemoveFromPool() {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()
	delete(Clients, client.UserID)
}

// talkToClient()  /The Sender
func (c *Client) WritePump() {
	defer c.Conn.Close()
	for {
		msg, ok := <-c.Send
		if !ok {
			c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		// WriteJSON automatically converts the struct to {"type":"...", "content":"..."}
		if err := c.Conn.WriteJSON(msg); err != nil {
			return
		}
	}
}

// listenToClient()  /The Receiver

func (c *Client) ReadPump(broadcast chan Message) {
	defer func() {
		c.RemoveFromPool()
		c.Conn.Close()
	}()

	// ... (Keep your SetReadLimit and PongHandler code here) ...

	for {
		var incoming WSMessage
		err := c.Conn.ReadJSON(&incoming)
		if err != nil {
			break
		}

		// --- NEW: LOGIC ROUTER ---
		switch incoming.Type {

		case "join":
			// 1. Permission Check (Mock)
			if incoming.Content == "admin-only" && c.UserID != "admin" {
				c.Send <- WSMessage{Type: "error", Content: "Permission Denied"}
				continue
			}

			// 2. Join the Room
			c.JoinRoom(incoming.Content) // Content = "general"
			c.Send <- WSMessage{Type: "system", Content: "Joined " + incoming.Content}

		case "message":
			// 3. Send to Specific Room
			// The user must tell us WHICH room they are sending to
			targetRoomName := incoming.Room // You need to add 'Room' field to WSMessage

			if room, ok := GlobalHub.Rooms[targetRoomName]; ok {
				// Check if user is actually IN that room
				if _, inRoom := room.Clients[c]; inRoom {
					room.Broadcast <- incoming
				} else {
					c.Send <- WSMessage{Type: "error", Content: "You are not in this room"}
				}
			}
		}
	}
}

func (c *Client) JoinRoom(roomName string) {
	room := GlobalHub.GetRoom(roomName)

	// Add to Room's list
	room.Clients[c] = true

	// Add to Client's list
	if c.Rooms == nil {
		c.Rooms = make(map[*Room]bool)
	}
	c.Rooms[room] = true
}
