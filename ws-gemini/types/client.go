package types

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn     *websocket.Conn
	Send     chan WSMessage
	UserID   string
	Username string
	Rooms    map[*Room]bool // Track which rooms I am in
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
	// defer func() {
	// 	c.RemoveFromPool()
	// 	c.Conn.Close()
	// }()

	// ... (Keep your SetReadLimit and PongHandler code here) ...

	for {
		// STEP 1: Read the raw bytes (Network Check)
		_, rawMessage, err := c.Conn.ReadMessage()
		if err != nil {
			// If the socket closed or network failed, stop the loop.
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// Log only real network errors
			}
			break // <--- FATAL ERROR: Kill connection
		}

		// STEP 2: Try to parse the JSON (Data Check)
		var incoming WSMessage
		if err := json.Unmarshal(rawMessage, &incoming); err != nil {
			// NON-FATAL ERROR: The user sent garbage (e.g., plain text)
			// We just log it and ignore this specific message.
			// We do NOT break the loop.
			// Optional: Send an error message back to the user
			c.Conn.WriteJSON(WSMessage{
				Type:    "error",
				Content: "Invalid JSON format. Please send a JSON object like {\"type\":\"text\", \"content\":\"Hello\"}.",
			})
			continue // <--- Skip to next message, keep connection alive!
		}

		// STEP 3: Success! Send to Hub
		broadcast <- Message{
			Client:  c,
			Payload: incoming,
		}
	}
}
