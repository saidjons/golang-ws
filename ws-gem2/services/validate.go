package services

import "fmt"

// Returns userID, username, error
func ValidateToken(token string) (string, string, error) {
	// 1. Check against a "Database" (Mock logic)
	if token == "12345" {
		return "user_1", "Alice", nil
	}
	if token == "67890" {
		return "user_2", "Bob", nil
	}

	// 2. Reject everyone else
	return "", "", fmt.Errorf("invalid token")
}
