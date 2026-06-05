package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"webs12/internal/auth"
	"webs12/internal/ws"
)

func main() {
	// 1. Initialize Services
	authService := auth.NewService(
		[]byte("super-secret-jwt-key"),
		[]byte("super-secret-app-key"), // Used for signing socket auth
	)

	// 2. Register Laravel-like Channel Rules
	// Rule for "private:chat.{id}"
	authService.RegisterRule("private", func(user *auth.User, channelName string) bool {
		if channelName == "private:general" {
			return true
		}
		// Add your DB checks here (e.g., does user have access to this room?)
		return user.Username == "admin"
	})

	// Rule for "presence:room.{id}"
	authService.RegisterRule("presence", func(user *auth.User, channelName string) bool {
		return true // Anyone authenticated can join presence channels
	})

	// 3. Initialize Hub
	hub := ws.NewHub(authService)
	go hub.Run()

	// 4. Setup HTTP Routes
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) { handleLogin(w, r, authService) })
	http.HandleFunc("/ws/auth", func(w http.ResponseWriter, r *http.Request) { handleWsAuth(w, r, authService) })
	http.HandleFunc("/ws", hub.HandleWS)

	fmt.Println("🚀 Clean WebSocket Server Running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleLogin(w http.ResponseWriter, r *http.Request, authService *auth.Service) {
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

	if creds.Password != "password123" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	user := &auth.User{ID: "1", Username: creds.Username}
	token, _ := authService.GenerateToken(user)
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// handleWsAuth mimics Laravel's /broadcasting/auth endpoint
func handleWsAuth(w http.ResponseWriter, r *http.Request, authService *auth.Service) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Validate JWT from Header
	user, err := authService.ValidateUserFromHeader(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Parse request
	var req struct {
		SocketID    string `json:"socket_id"`
		ChannelName string `json:"channel_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 3. Check permissions (Laravel's channels.php logic)
	if !authService.CanJoinChannel(user, req.ChannelName) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 4. Generate signature
	signature := authService.GenerateSignature(req.SocketID, req.ChannelName)

	// 5. Return auth payload
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"auth": req.SocketID + ":" + signature,
	})
}
