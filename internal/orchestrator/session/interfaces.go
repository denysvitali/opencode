package session

// Config holds configuration for session stores
type Config struct {
	// For SQLite
	DatabasePath string

	// Common settings
	MaxConnections int
	ConnTimeout    int // seconds
}
