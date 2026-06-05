package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"yourproject/internal/auth" // Replace with your actual module path

	"github.com/gorilla/websocket"
)

// Message follows the Pusher/Laravel Echo protocol
type Message struct {
	Event   string      `json:"event"`
	Channel string      `json:"channel,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	socketID string
	user     *auth.User // Populated after client sends "auth" event
	channels map[string]bool
	mu       sync.RWMutex
}

type Hub struct {
	authService *auth.Service
	channels    map[string]map[*Client]bool
	mu          sync.RWMutex
	register    chan *Client
	unregister  chan *Client
	broadcast   chan broadcastMsg
}

type broadcastMsg struct {
	channel string
	data    []byte
	exclude *Client
}

func NewHub(authService *auth.Service) *Hub {
	return &Hub{
		authService: authService,
		channels:    make(map[string]map[*Client]bool),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		broadcast:   make(chan broadcastMsg, 100),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			log.Printf("Client connected: %s", client.socketID)
		case client := <-h.unregister:
			h.mu.Lock()
			for channel, clients := range h.channels {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					if len(clients) == 0 {
						delete(h.channels, channel)
					}
				}
			}
			close(client.send)
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			clients, exists := h.channels[msg.channel]
			h.mu.RUnlock()

			if exists {
				for client := range clients {
					if client == msg.exclude {
						continue
					}
					select {
					case client.send <- msg.data:
					default:
						go func(c *Client) { h.unregister <- c }(client) // Drop slow clients
					}
				}
			}
		}
	}
}

func (h *Hub) Subscribe(c *Client, channelName string) {
	h.mu.Lock()
	if h.channels[channelName] == nil {
		h.channels[channelName] = make(map[*Client]bool)
	}
	h.channels[channelName][c] = true
	c.mu.Lock()
	c.channels[channelName] = true
	c.mu.Unlock()
	h.mu.Unlock()
}

func (h *Hub) Broadcast(channelName string, event string, data interface{}, exclude *Client) {
	msg := Message{Event: event, Channel: channelName, Data: data}
	b, _ := json.Marshal(msg)
	h.broadcast <- broadcastMsg{channel: channelName, data: b, exclude: exclude}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func generateSocketID() string {
	b1, b2 := make([]byte, 4), make([]byte, 4)
	rand.Read(b1)
	rand.Read(b2)
	return hex.EncodeToString(b1) + "." + hex.EncodeToString(b2)
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 256),
		socketID: generateSocketID(),
		channels: make(map[string]bool),
	}

	// 1. Send Socket ID to client immediately
	initMsg := Message{Event: "pusher:connection_established", Data: map[string]string{"socket_id": client.socketID}}
	b, _ := json.Marshal(initMsg)
	client.send <- b

	h.register <- client
	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var msg Message
		if err := c.conn.ReadJSON(&msg); err != nil {
			break
		}

		switch msg.Event {
		case "auth":
			c.handleAuth(msg)
		case "pusher:subscribe":
			c.handleSubscribe(msg)
		case strings.HasPrefix(msg.Event, "client-"): // Client events (e.g., client:typing)
			c.handleClientEvent(msg)
		}
	}
}

func (c *Client) handleAuth(msg Message) {
	var data struct {
		Token string `json:"token"`
	}
	b, _ := json.Marshal(msg.Data)
	json.Unmarshal(b, &data)

	user, err := c.hub.authService.ValidateToken(data.Token)
	if err != nil {
		c.sendError("auth", "", "Invalid token")
		return
	}
	c.user = user
	c.send <- marshal(Message{Event: "auth_success", Data: map[string]string{"username": user.Username}})
}

func (c *Client) handleSubscribe(msg Message) {
	var data struct {
		Channel string `json:"channel"`
		Auth    string `json:"auth"`
	}
	b, _ := json.Marshal(msg.Data)
	json.Unmarshal(b, &data)

	parts := strings.SplitN(data.Channel, ":", 2)
	if len(parts) != 2 {
		c.sendError("pusher:subscribe", data.Channel, "Invalid channel format. Use 'prefix:name'")
		return
	}
	prefix := parts[0]

	// 1. Verify Signature for Private/Presence channels
	if prefix != "public" {
		partsAuth := strings.SplitN(data.Auth, ":", 2)
		if len(partsAuth) != 2 || !c.hub.authService.VerifySignature(c.socketID, data.Channel, partsAuth[1]) {
			c.sendError("pusher:subscribe", data.Channel, "Invalid auth signature")
			return
		}
	}

	// 2. Ensure user is authenticated for private channels
	if prefix != "public" && c.user == nil {
		c.sendError("pusher:subscribe", data.Channel, "Authentication required")
		return
	}

	// 3. Subscribe
	c.hub.Subscribe(c, data.Channel)
	c.send <- marshal(Message{Event: "pusher:subscription_succeeded", Channel: data.Channel})
}

func (c *Client) handleClientEvent(msg Message) {
	c.mu.RLock()
	_, isSubscribed := c.channels[msg.Channel]
	c.mu.RUnlock()

	if !isSubscribed {
		c.sendError(msg.Event, msg.Channel, "Not subscribed to channel")
		return
	}
	// Broadcast to others in the channel
	c.hub.Broadcast(msg.Channel, msg.Event, msg.Data, c)
}

func (c *Client) sendError(event, channel, errMsg string) {
	errMsgObj := Message{Event: "pusher:error", Channel: channel, Data: map[string]string{"message": errMsg, "event": event}}
	c.send <- marshal(errMsgObj)
}

func marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
