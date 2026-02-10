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
			if client.UserID == msg.Sender {
				continue
			}
			select {
			case client.Send <- msg:
			default:
				close(client.Send)
				delete(r.Clients, client)
			}
		}
	}
}
