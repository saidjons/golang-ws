package types

import "sync"

// The Hub manages ALL rooms
type Hub struct {
	Rooms map[string]*Room
	mu    sync.RWMutex
}

// Room definition
type Room struct {
	Name      string
	Clients   map[*Client]bool
	Broadcast chan WSMessage
	mu        sync.RWMutex
}

var GlobalHub = &Hub{
	Rooms: make(map[string]*Room),
}

// Helper to safely get/create rooms
func (h *Hub) GetRoom(name string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()

	if room, exists := h.Rooms[name]; exists {
		return room
	}

	newRoom := &Room{
		Name:      name,
		Clients:   make(map[*Client]bool),
		Broadcast: make(chan WSMessage),
	}
	h.Rooms[name] = newRoom

	// Start the room running in background
	go newRoom.Run()
	return newRoom
}

func (r *Room) Run() {
	for msg := range r.Broadcast {
		for client := range r.Clients {

			r.mu.RLock()
			// don't send the message back to the sender
			if client.UserID == msg.Sender {
				continue
			}

			select {
			case client.Send <- msg:
			default:
				close(client.Send)
				delete(r.Clients, client)
			}
			r.mu.RUnlock()
		}
	}
}

func (r *Room) Join(client *Client) {
	r.mu.Lock()
	r.Clients[client] = true
	r.mu.Unlock()
}

// GetOnlineUsers returns a list of usernames in a specific room
func (h *Hub) GetOnlineUsers(roomName string) []string {
	// 1. Find the room
	h.mu.RLock() // Read Lock (allows multiple readers, blocks writers)
	room, exists := h.Rooms[roomName]
	h.mu.RUnlock()

	if !exists {
		return []string{} // Room doesn't exist, return empty list
	}

	// 2. Lock the room to safely read the map
	room.mu.Lock()
	defer room.mu.Unlock()

	var users []string
	for client := range room.Clients {
		users = append(users, client.Username)
	}

	return users
}

func (r *Room) LeaveRoom(client *Client) {
	r.mu.Lock()
	delete(r.Clients, client)
	r.mu.Unlock()
}
