package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type User struct {
	ID       string
	Username string
}

// ChannelRule defines a function that checks if a user can join a channel.
// This mimics Laravel's: Broadcast::channel('name', function ($user) { ... })
type ChannelRule func(user *User, channelName string) bool

type Service struct {
	jwtSecret []byte
	appSecret []byte // Used to sign socket auth requests
	rules     map[string]ChannelRule
}

func NewService(jwtSecret, appSecret []byte) *Service {
	return &Service{
		jwtSecret: jwtSecret,
		appSecret: appSecret,
		rules:     make(map[string]ChannelRule),
	}
}

// RegisterRule mimics Laravel's Broadcast::channel()
func (s *Service) RegisterRule(prefix string, rule ChannelRule) {
	s.rules[prefix] = rule
}

func (s *Service) ValidateToken(tokenStr string) (*User, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	claims := token.Claims.(jwt.MapClaims)
	return &User{
		ID:       claims["sub"].(string),
		Username: claims["username"].(string),
	}, nil
}

func (s *Service) ValidateUserFromHeader(r *http.Request) (*User, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, errors.New("missing token")
	}
	return s.ValidateToken(strings.TrimPrefix(authHeader, "Bearer "))
}

func (s *Service) GenerateToken(user *User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":      user.ID,
		"username": user.Username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})
	return token.SignedString(s.jwtSecret)
}

// CanJoinChannel checks Laravel-like channel rules
func (s *Service) CanJoinChannel(user *User, channelName string) bool {
	parts := strings.SplitN(channelName, ":", 2)
	if len(parts) != 2 {
		return false
	}
	prefix := parts[0]

	if prefix == "public" {
		return true // Public channels are always allowed
	}

	if rule, exists := s.rules[prefix]; exists {
		return rule(user, channelName) // Execute your custom logic
	}
	return false // Default deny for unknown private/presence channels
}

// GenerateSignature creates the Pusher/Laravel style auth signature
func (s *Service) GenerateSignature(socketID, channelName string) string {
	stringToSign := fmt.Sprintf("%s:%s", socketID, channelName)
	mac := hmac.New(sha256.New, s.appSecret)
	mac.Write([]byte(stringToSign))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) VerifySignature(socketID, channelName, signature string) bool {
	expected := s.GenerateSignature(socketID, channelName)
	return hmac.Equal([]byte(expected), []byte(signature))
}
