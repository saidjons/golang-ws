package types

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

type Client struct {
	Conn             *websocket.Conn
	ConnectedTime    time.Time
	DisConnectedTime time.Time
	IsDisConnected   bool
	Send             chan []Message
}

var (
	Clients   = make(map[*websocket.Conn]*Client)
	ClientsMu sync.Mutex

	History   []Message
	HistoryMu sync.Mutex
)

func (client *Client) AddtoPool() {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()
	Clients[client.Conn] = client
}

func (client *Client) RemoveFromPool() {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()
	delete(Clients, client.Conn)
}

func (client *Client) IsExists() bool {
	ClientsMu.Lock()
	defer ClientsMu.Unlock()
	_, exists := Clients[client.Conn]
	return exists
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The Hub closed the channel.
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(msg)

			// Optimization: Add queued chat messages to the current websocket message.
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) ReadPump(broadcast chan Message) {
	defer func() {
		c.RemoveFromPool()
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		// FIX 3: Capture the message! (Use 'message', not '_')
		var incoming WSMessage
		err := c.Conn.ReadJSON(&incoming) // <--- Go handles the JSON parsing for you!

		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// Log only unexpected errors
			}
			break
		}

		// FIX 4: Actually send the message to the hub
		broadcast <- Message{
			Sender:  c,
			Payload: incoming,
		}
	}
}
