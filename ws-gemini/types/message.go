package types

import "time"

// 1. Define the Message Types
const (
	MsgTypeText  = "text"
	MsgTypeImage = "image"
	MsgTypeAudio = "audio"
)

// 2. Define the JSON Structure
// This is what goes over the wire
type WSMessage struct {
	Type    string `json:"type"`    // "text", "image", "file"
	Content string `json:"content"` // The text OR the URL
	Sender  string `json:"sender"`  // The Username (optional)
}

type Message struct {
	Timestamp time.Time
	Sender    *Client
	Payload   WSMessage
}
