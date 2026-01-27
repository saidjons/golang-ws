package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var (
	jwtSecret = []byte("super-secret-key-change-in-production")

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	db *sql.DB
)

type Message struct {
	Type      string `json:"type"` // "message", "join", "auth_success", "error"
	Username  string `json:"username,omitempty"`
	Room      string `json:"room,omitempty"`
	Content   string `json:"content,omitempty"`
	Timestamp string `json:"timestamp"`
}

type Client struct {
	conn   *websocket.Conn
	send   chan []byte
	userID string // empty if not authenticated
	room   string
}

type Hub struct {
	rooms      map[string]map[*Client]bool
	mu         sync.RWMutex
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
}

var hub = Hub{
	rooms:      make(map[string]map[*Client]bool),
	broadcast:  make(chan Message, 100),
	register:   make(chan *Client),
	unregister: make(chan *Client),
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./db.sqlite")
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			room TEXT,
			username TEXT,
			content TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
}

func saveMessage(room, username, content string) {
	_, err := db.Exec("INSERT INTO messages (room, username, content) VALUES (?, ?, ?)", room, username, content)
	if err != nil {
		log.Println("DB save error:", err)
	}
}

func getRecentMessages(room string, limit int) []Message {
	rows, err := db.Query("SELECT username, content, timestamp FROM messages WHERE room = ? ORDER BY id DESC LIMIT ?", room, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var username, content, ts string
		rows.Scan(&username, &content, &ts)
		msgs = append(msgs, Message{
			Type:      "message",
			Username:  username,
			Content:   content,
			Timestamp: ts,
			Room:      room,
		})
	}
	// Reverse to chronological order
	for i := len(msgs)/2 - 1; i >= 0; i-- {
		opp := len(msgs) - 1 - i
		msgs[i], msgs[opp] = msgs[opp], msgs[i]
	}
	return msgs
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if h.rooms[client.room] == nil {
				h.rooms[client.room] = make(map[*Client]bool)
			}
			h.rooms[client.room][client] = true
			h.mu.Unlock()

			welcome := Message{Type: "join", Content: "Welcome to room: " + client.room, Timestamp: time.Now().Format(time.RFC3339)}
			client.send <- marshal(welcome)

			// Send history (public always gets it, private only if authenticated)
			if client.room == "public" || client.userID != "" {
				for _, m := range getRecentMessages(client.room, 20) {
					client.send <- marshal(m)
				}
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if roomClients, ok := h.rooms[client.room]; ok {
				if _, ok := roomClients[client]; ok {
					delete(roomClients, client)
					close(client.send)
					if len(roomClients) == 0 {
						delete(h.rooms, client.room)
					}
				}
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			data := marshal(message)
			saveMessage(message.Room, message.Username, message.Content)

			h.mu.RLock()
			clients := h.rooms[message.Room]
			h.mu.RUnlock()

			for client := range clients {
				if message.Room != "public" && client.userID == "" {
					continue // block unauth in private rooms
				}

				select {
				case client.send <- data:
				default:
					// Client is dead/slow
					h.mu.Lock()
					delete(h.rooms[client.room], client)
					close(client.send)
					h.mu.Unlock()
				}
			}
		}
	}
}

func marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	// === CRITICAL: Keep connection alive ===
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	room := r.URL.Query().Get("room")
	if room == "" {
		room = "public"
	}

	client := &Client{
		conn:   conn,
		send:   make(chan []byte, 256),
		room:   room,
		userID: "",
	}

	hub.register <- client
	go client.writePump()
	client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Unexpected close error: %v", err)
			}
			break
		}

		// Authentication
		if msg.Type == "auth" && c.userID == "" {
			token, err := jwt.Parse(msg.Content, func(t *jwt.Token) (interface{}, error) {
				return jwtSecret, nil
			})

			if err != nil || !token.Valid {
				c.send <- marshal(Message{Type: "error", Content: "Invalid token"})
				continue
			}

			claims := token.Claims.(jwt.MapClaims)
			username := claims["username"].(string)
			c.userID = username

			c.send <- marshal(Message{
				Type: "auth_success", Username: username, Content: "Authenticated!", Timestamp: time.Now().Format(time.RFC3339),
			})

			// Send history after auth
			for _, m := range getRecentMessages(c.room, 20) {
				c.send <- marshal(m)
			}
			continue
		}

		// Block unauthenticated sends in private rooms
		if c.room != "public" && c.userID == "" {
			c.send <- marshal(Message{Type: "error", Content: "Auth required"})
			continue
		}

		if msg.Type == "message" && msg.Content != "" {
			broadcastMsg := Message{
				Type:      "message",
				Username:  c.userID,
				Content:   msg.Content,
				Room:      c.room,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			hub.broadcast <- broadcastMsg
		}
	}
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
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Send all queued messages in one batch
			for len(c.send) > 0 {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
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

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Dummy auth - replace with real DB check later
	if creds.Password != "password123" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": creds.Username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, _ := token.SignedString(jwtSecret)
	json.NewEncoder(w).Encode(map[string]string{"token": tokenString})
}

func main() {
	initDB()
	go hub.run()

	http.HandleFunc("/ws", handleConnections)
	http.HandleFunc("/login", loginHandler)

	fmt.Println("🚀 WebSocket Chat Server Running on :8080")
	fmt.Println("Public room:  ws://localhost:8080/ws?room=public")
	fmt.Println("Private room: ws://localhost:8080/ws?room=secret  (needs JWT)")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
