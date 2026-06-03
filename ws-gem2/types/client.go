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
	mu       sync.RWMutex
}

var (
	Clients   = make(map[string][]*Client)
	ClientsMu sync.Mutex

	History   []WSMessage
	HistoryMu sync.Mutex
)

func (client *Client) AddtoPool() {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()
	Clients[client.UserID] = append(Clients[client.UserID], client)
}

func (c *Client) RemoveFromPool() {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()

	userConnections := Clients[c.UserID]

	for i, client := range userConnections {
		// Compare pointers: Is this the specific tab that closed?
		if client == c {
			// Cut it out of the list
			Clients[c.UserID] = append(userConnections[:i], userConnections[i+1:]...)
			break
		}
	}

	// Clean up empty keys
	if len(Clients[c.UserID]) == 0 {
		delete(Clients, c.UserID)
	}
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
			roomName := incoming.Content
			room := GlobalHub.GetRoom(roomName)

			// 2. Add this client to the room
			room.Join(c)

			// 3. (Optional) Confirm to user
			c.Send <- WSMessage{Type: "system", Content: "You joined " + roomName}
		case "get_users":
			// 1. Which room?
			roomName := incoming.Content

			// 2. Get the list (Thread-Safe)
			onlineUsers := GlobalHub.GetOnlineUsers(roomName)

			// 3. Convert list to a single string (e.g., "Alice, Bob")
			// OR send a JSON array if you prefer
			// Simple comma-separated string for now:
			userListString := ""
			for _, u := range onlineUsers {
				userListString += u + ","
			}

			// 4. Send ONLY to the user who asked
			c.Send <- WSMessage{
				Type:    "user_list",
				Content: userListString, // "Alice,Bob,Charlie,"
				Room:    roomName,
			}

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
	room.Join(c)
	// Add to Room's list
	room.Clients[c] = true

	// Add to Client's list

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Rooms == nil {
		c.Rooms = make(map[*Room]bool)
	}
	c.Rooms[room] = true
}
