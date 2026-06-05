// package types.
package types

import (
	"log"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type      string `json:"type"` // "message", "join", "auth_success", "error"
	Username  string `json:"username,omitempty"`
	Room      string `json:"room,omitempty"`
	Content   string `json:"content,omitempty"`
	Timestamp string `json:"timestamp"`
}

type Client struct {
	Conn   *websocket.Conn
	Send   chan []byte
	UserID string // empty if not authenticated
	Room   string
}

type Hub struct {
	Rooms      map[string]map[*Client]bool
	Mu         sync.RWMutex
	Broadcast  chan Message
	Register   chan *Client
	Unregister chan *Client
}

func (c *Client) ReadPump(hub *Hub) {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
	}()

	for {
		var msg Message
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Unexpected close error: %v", err)
			}
			break
		}

		// Authentication
		if msg.Type == "auth" && c.UserID == "" {
			token, err := jwt.Parse(msg.Content, func(t *jwt.Token) (interface{}, error) {
				return jwtSecret, nil
			})

			if err != nil || !token.Valid {
				c.Send <- marshal(types.Message{Type: "error", Content: "Invalid token"})
				continue
			}

			claims := token.Claims.(jwt.MapClaims)
			username := claims["username"].(string)
			c.UserID = username

			c.Send <- marshal(types.Message{
				Type: "auth_success", Username: username, Content: "Authenticated!", Timestamp: time.Now().Format(time.RFC3339),
			})

			// Send history after auth
			for _, m := range getRecentMessages(c.room, 20) {
				c.Send <- marshal(m)
			}
			continue
		}

		// Block unauthenticated sends in private rooms
		if c.Room != "public" && c.UserID == "" {
			c.Send <- marshal(types.Message{Type: "error", Content: "Auth required"})
			continue
		}

		if msg.Type == "message" && msg.Content != "" {
			broadcastMsg := types.Message{
				Type:      "message",
				Username:  c.UserID,
				Content:   msg.Content,
				Room:      c.Room,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			hub.Broadcast <- broadcastMsg
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Send all queued messages in one batch
			for len(c.Send) > 0 {
				w.Write([]byte{'\n'})
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
