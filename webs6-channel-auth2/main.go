package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/dgrijalva/jwt-go" // v3
	"github.com/gorilla/websocket"
)

/* ---------- 1.  tiny JWT helpers ---------- */
var jwtKey = []byte("super-secret-demo-key") // FIXME: env var

type Claims struct {
	UserID   string   `json:"user_id"`
	Channels []string `json:"channels"` // what rooms he may listen to
	jwt.StandardClaims
}

func parseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	tkn, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) { return jwtKey, nil })
	if err != nil || !tkn.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

/* ---------- 2.  hub with per-channel clients ---------- */
type Hub struct {
	sync.RWMutex
	// map[channel]set[client]
	channels map[string]map[*Client]bool
}

type Client struct {
	claims *Claims
	conn   *websocket.Conn
	send   chan json.RawMessage
}

var hub = &Hub{channels: make(map[string]map[*Client]bool)}

func (h *Hub) join(ch string, c *Client) {
	h.Lock()
	if h.channels[ch] == nil {
		h.channels[ch] = make(map[*Client]bool)
	}
	h.channels[ch][c] = true
	h.Unlock()
}

func (h *Hub) leave(ch string, c *Client) {
	h.Lock()
	delete(h.channels[ch], c)
	if len(h.channels[ch]) == 0 {
		delete(h.channels, ch)
	}
	h.Unlock()
}

func (h *Hub) broadcast(ch string, msg json.RawMessage, sender *Client) {
	h.RLock()
	clients := h.channels[ch]
	h.RUnlock()

	for c := range clients {
		if c == sender {
			continue
		} // optional
		select {
		case c.send <- msg:
		default:
			// slow client, drop
		}
	}
}

/* ---------- 3.  upgrade + auth ---------- */
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// 3a.  grab token (header ?query)
	tokenStr := r.Header.Get("Authorization")
	if tokenStr == "" {
		tokenStr = r.URL.Query().Get("token")
	} else {
		tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	}
	claims, err := parseToken(tokenStr)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}

	client := &Client{claims: claims, conn: conn, send: make(chan json.RawMessage, 256)}
	// pre-join user to all his allowed channels
	for _, ch := range claims.Channels {
		hub.join(ch, client)
	}
	log.Printf("user %s connected, channels %+v", claims.UserID, claims.Channels)

	// read loop
	go func() {
		defer func() {
			for _, ch := range claims.Channels {
				hub.leave(ch, client)
			}
			conn.Close()
			log.Printf("user %s disconnected", claims.UserID)
		}()

		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// minimal JSON extract: {"channel":"ROOM", ...}
			var envelope struct {
				Channel string          `json:"channel"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(msgBytes, &envelope); err != nil {
				continue // malformed
			}
			// permission check: may this user broadcast to that channel?
			allowed := false
			for _, allowedCh := range claims.Channels {
				if allowedCh == envelope.Channel {
					allowed = true
					break
				}
			}
			if !allowed {
				log.Printf("user %s tried to write to forbidden channel %s", claims.UserID, envelope.Channel)
				continue
			}
			// re-encode full message to listeners
			hub.broadcast(envelope.Channel, msgBytes, client)
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

/* ---------- 4.  dummy login endpoint to grab a token ---------- */
func login(w http.ResponseWriter, r *http.Request) {
	// POST {"user":"alice","password":"123"}
	var cred struct{ User, Password string }
	if err := json.NewDecoder(r.Body).Decode(&cred); err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	// fake auth
	if cred.Password != "123" {
		http.Error(w, "bad creds", 401)
		return
	}
	// build token
	claims := &Claims{
		UserID:   cred.User,
		Channels: []string{"public", "room-" + cred.User}, // each user owns a private room
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, _ := tok.SignedString(jwtKey)
	json.NewEncoder(w).Encode(map[string]string{"token": ss})
}

/* ---------- 5.  boot ---------- */
func main() {
	http.HandleFunc("/login", login)
	http.HandleFunc("/ws", wsHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
