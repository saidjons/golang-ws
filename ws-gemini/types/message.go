package types

import "time"

// 1. Define the Message Types
const (
	MsgTypeText  = "text"
	MsgTypeImage = "image"
	MsgTypeAudio = "audio"
)

type WSMessage struct {
	Type    string `json:"type"`    // "text", "image"
	Content string `json:"content"` // "Hello World"
	Sender  string `json:"sender"`  // "User 127.0.0.1"
	Room    string `json:"room"`
}

// --- 2. The Internal Hub Data (What stays in the server) ---
type Message struct {
	Timestamp time.Time
	Client    *Client   // <--- The pointer to the User connection
	Payload   WSMessage // <--- The actual JSON data
}
