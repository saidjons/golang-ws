package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"webs9-chat-db/database"
	"webs9-chat-db/types"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var (
	jwtSecret = []byte("super-secret-key-change-in-production")

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

var hub = types.Hub{
	Rooms:      make(map[string]map[*types.Client]bool),
	Broadcast:  make(chan types.Message, 100),
	Register:   make(chan *types.Client),
	Unregister: make(chan *types.Client),
}

func saveMessage(room, username, content string) {
	_, err := db.Exec("INSERT INTO messages (room, username, content) VALUES (?, ?, ?)", room, username, content)
	if err != nil {
		log.Println("DB save error:", err)
	}
}

func getRecentMessages(room string, limit int) []types.Message {
	rows, err := db.Query("SELECT username, content, timestamp FROM messages WHERE room = ? ORDER BY id DESC LIMIT ?", room, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var msgs []types.Message
	for rows.Next() {
		var username, content, ts string
		rows.Scan(&username, &content, &ts)
		msgs = append(msgs, types.Message{
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

func (h *types.Hub) run() {
	for {
		select {
		case client := <-h.Register:
			h.Mu.Lock()
			if h.Rooms[client.Room] == nil {
				h.Rooms[client.Room] = make(map[*types.Client]bool)
			}
			h.Rooms[client.Room][client] = true
			h.Mu.Unlock()

			welcome := types.Message{Type: "join", Content: "Welcome to room: " + client.Room, Timestamp: time.Now().Format(time.RFC3339)}
			client.Send <- marshal(welcome)

			// Send history (public always gets it, private only if authenticated)
			if client.Room == "public" || client.UserID != "" {
				for _, m := range getRecentMessages(client.room, 20) {
					client.Send <- marshal(m)
				}
			}

		case client := <-h.Unregister:
			h.Mu.Lock()
			if roomClients, ok := h.Rooms[client.Room]; ok {
				if _, ok := roomClients[client]; ok {
					delete(roomClients, client)
					close(client.Send)
					if len(roomClients) == 0 {
						delete(h.Rooms, client.Room)
					}
				}
			}
			h.Mu.Unlock()

		case message := <-h.Broadcast:
			data := marshal(message)
			saveMessage(message.Room, message.Username, message.Content)

			h.Mu.RLock()
			clients := h.Rooms[message.Room]
			h.Mu.RUnlock()

			for client := range clients {
				if message.Room != "public" && client.UserID == "" {
					continue // block unauth in private rooms
				}

				select {
				case client.Send <- data:
				default:
					// Client is dead/slow
					h.Mu.Lock()
					delete(h.Rooms[client.Room], client)
					close(client.Send)
					h.Mu.Unlock()
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

	client := &types.Client{
		Conn:   conn,
		Send:   make(chan []byte, 256),
		Room:   room,
		UserID: "",
	}

	hub.Register <- client
	go client.writePump()
	client.readPump()
}

func (c *types.Client) readPump() {
	defer func() {
		hub.Unregister <- c
		c.Conn.Close()
	}()

	for {
		var msg types.Message
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
	db := database.InitDB()
	defer db.Close()

	go hub.Run()

	http.HandleFunc("/ws", handleConnections)
	http.HandleFunc("/login", loginHandler)

	fmt.Println("🚀 WebSocket Chat Server Running on :8081")
	fmt.Println("Public room:  ws://localhost:8080/ws?room=public")
	fmt.Println("Private room: ws://localhost:8080/ws?room=secret  (needs JWT)")
	log.Fatal(http.ListenAndServe(":8081", nil))
}
