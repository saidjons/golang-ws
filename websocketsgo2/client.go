package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

/*
	----------------------------------------------------------
	  1.  AUTH  (JWT, unchanged from previous example)

----------------------------------------------------------
*/
var jwtKey = []byte("demo-key")

type Claims struct {
	UserID string `json:"user_id"`
	jwt.StandardClaims
}

func parseToken(s string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(s, claims, func(*jwt.Token) (interface{}, error) { return jwtKey, nil })
	if err != nil {
		return nil, err
	}
	return claims, nil
}

/*
	----------------------------------------------------------
	  2.  DATA MODELS

----------------------------------------------------------
*/
type Room struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"` // user_ids allowed to join
}

type Hub struct {
	sync.RWMutex
	rooms map[string]*Room          // room_id -> room
	conns map[string]map[*Conn]bool // room_id -> set of conns
}

type Conn struct {
	userID string
	roomID string
	conn   *websocket.Conn
	send   chan json.RawMessage
}

var hub = &Hub{
	rooms: make(map[string]*Room),
	conns: make(map[string]map[*Conn]bool),
}

/*
	----------------------------------------------------------
	  3.  ROOM CRUD  (REST, no auth for demo)

----------------------------------------------------------
*/
func createRoom(w http.ResponseWriter, r *http.Request) {
	var req Room
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	hub.Lock()
	if _, ok := hub.rooms[req.ID]; ok {
		hub.Unlock()
		http.Error(w, "exists", 409)
		return
	}
	hub.rooms[req.ID] = &req
	hub.conns[req.ID] = make(map[*Conn]bool)
	hub.Unlock()
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(req)
}

func listRooms(w http.ResponseWriter, r *http.Request) {
	hub.RLock()
	out := make([]Room, 0, len(hub.rooms))
	for _, v := range hub.rooms {
		out = append(out, *v)
	}
	hub.RUnlock()
	json.NewEncoder(w).Encode(out)
}

/*
	----------------------------------------------------------
	  4.  MEMBERSHIP ACL  (add / remove users to a room)

----------------------------------------------------------
*/
func addMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	roomID := mux.Vars(r)["room_id"]
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	hub.Lock()
	room, ok := hub.rooms[roomID]
	if !ok {
		hub.Unlock()
		http.Error(w, "no room", 404)
		return
	}
	// de-dup
	for _, u := range room.Members {
		if u == req.UserID {
			hub.Unlock()
			w.WriteHeader(204)
			return
		}
	}
	room.Members = append(room.Members, req.UserID)
	hub.Unlock()
	w.WriteHeader(204)
}

func removeMember(w http.ResponseWriter, r *http.Request) {
	roomID := mux.Vars(r)["room_id"]
	userID := mux.Vars(r)["user_id"]
	hub.Lock()
	room, ok := hub.rooms[roomID]
	if !ok {
		hub.Unlock()
		http.Error(w, "no room", 404)
		return
	}
	newList := room.Members[:0]
	for _, u := range room.Members {
		if u != userID {
			newList = append(newList, u)
		}
	}
	room.Members = newList
	// kick existing sockets
	for c := range hub.conns[roomID] {
		if c.userID == userID {
			c.conn.Close()
		}
	}
	hub.Unlock()
	w.WriteHeader(204)
}

/*
	----------------------------------------------------------
	  5.  WEBSOCKET UPGRADE  (with per-room auth)

----------------------------------------------------------
*/
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	roomID := mux.Vars(r)["room_id"]
	tokenStr := r.Header.Get("Authorization")
	if tokenStr == "" {
		tokenStr = r.URL.Query().Get("token")
	} else {
		tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	}
	claims, err := parseToken(tokenStr)
	if err != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	hub.RLock()
	room, ok := hub.rooms[roomID]
	if !ok {
		hub.RUnlock()
		http.Error(w, "no room", 404)
		return
	}
	allowed := false
	for _, m := range room.Members {
		if m == claims.UserID {
			allowed = true
			break
		}
	}
	hub.RUnlock()
	if !allowed {
		http.Error(w, "forbidden", 403)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	c := &Conn{userID: claims.UserID, roomID: roomID, conn: conn, send: make(chan json.RawMessage, 256)}

	hub.Lock()
	hub.conns[roomID][c] = true
	hub.Unlock()

	// read loop
	go func() {
		defer func() {
			hub.Lock()
			delete(hub.conns[roomID], c)
			hub.Unlock()
			conn.Close()
		}()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// broadcast to everyone else in this room
			hub.RLock()
			for other := range hub.conns[roomID] {
				if other == c {
					continue
				}
				select {
				case other.send <- msg:
				default:
					// slow client, drop
				}
			}
			hub.RUnlock()
		}
	}()

	// write loop
	go func() {
		for msg := range c.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				break
			}
		}
	}()
}

/*
	----------------------------------------------------------
	  6.  ROUTES

----------------------------------------------------------
*/
func main() {
	r := mux.NewRouter()
	// REST
	r.HandleFunc("/rooms", createRoom).Methods("POST")
	r.HandleFunc("/rooms", listRooms).Methods("GET")
	r.HandleFunc("/rooms/{room_id}/members", addMember).Methods("POST")
	r.HandleFunc("/rooms/{room_id}/members/{user_id}", removeMember).Methods("DELETE")
	// WS
	r.HandleFunc("/rooms/{room_id}/ws", wsHandler)
	log.Fatal(http.ListenAndServe(":8080", r))
}
