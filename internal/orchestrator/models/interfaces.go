package models

import (
	"context"
	"time"

	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

// Runtime defines the interface for different execution environments
// This abstraction allows the orchestrator to work with different runtimes
// like Kubernetes, Docker, Firecracker, etc.
type Runtime interface {
	// CreateSession creates a new session in the runtime environment
	CreateSession(ctx context.Context, session *orchestratorpb.Session) error

	// GetSessionStatus returns the current status of a session
	GetSessionStatus(ctx context.Context, sessionID string) (*orchestratorpb.SessionStatus, error)

	// DeleteSession removes a session from the runtime environment
	DeleteSession(ctx context.Context, sessionID string) error

	// WaitForSessionReady waits for a session to become ready
	WaitForSessionReady(ctx context.Context, sessionID string) error

	// GetSessionEndpoint returns the network endpoint for the session
	GetSessionEndpoint(ctx context.Context, sessionID string) (string, error)

	// ListSessions returns all sessions managed by this runtime
	ListSessions(ctx context.Context) ([]*orchestratorpb.Session, error)

	// HealthCheck performs a health check of the runtime
	HealthCheck(ctx context.Context) error

	// Close cleans up runtime resources
	Close() error
}

// RuntimeConfig is a marker interface for runtime-specific configurations
type RuntimeConfig interface {
	// GetType returns the runtime type (kubernetes, docker, firecracker, etc.)
	GetType() string
}

type ResourceList struct {
	CPU    string `json:"cpu,omitempty"`    // CPU resources (e.g., "500m" for 0.5 CPU)
	Memory string `json:"memory,omitempty"` // Memory resources (e.g., "512Mi" for 512 MiB)
}

type ResourceRequirements struct {
	Requests ResourceList `json:"requests,omitempty"` // Resource requests
	Limits   ResourceList `json:"limits,omitempty"`   // Resource limits
}

// KubernetesConfig holds Kubernetes-specific configuration
type KubernetesConfig struct {
	Namespace   string
	Kubeconfig  string
	Image       string
	Resources   ResourceRequirements
	StorageSize string
}

// GetType returns the runtime type
func (k *KubernetesConfig) GetType() string {
	return "kubernetes"
}

// Config holds the orchestrator configuration
type Config struct {
	RuntimeConfig RuntimeConfig
	SessionTTL    time.Duration
}

// SessionManager defines the interface for session persistence
type SessionManager interface {
	// CreateSession creates a new session
	CreateSession(ctx context.Context, session *orchestratorpb.CreateSessionRequest) (*orchestratorpb.Session, error)

	// GetSession retrieves a session by ID
	GetSession(ctx context.Context, sessionID string, userID string) (*orchestratorpb.Session, error)

	// GetSessionByUser retrieves a session by ID and verifies user ownership
	GetSessionByUser(ctx context.Context, sessionID, userID string) (*orchestratorpb.Session, error)

	// ListSessions lists all sessions for a user with pagination
	ListSessions(ctx context.Context, userID string, limit int32, pageToken string) ([]*orchestratorpb.Session, string, error)

	// UpdateSession updates an existing session
	UpdateSession(ctx context.Context, session *orchestratorpb.Session) error

	// DeleteSession removes a session
	DeleteSession(ctx context.Context, sessionID string, userID string, force bool) error

	// CountSessions returns the total number of sessions for a user
	CountSessions(ctx context.Context, userID string) (int, error)

	// ListExpiredSessions returns sessions that haven't been accessed within the TTL
	ListExpiredSessions(ctx context.Context, ttl int64) ([]*orchestratorpb.Session, error)

	// UpdateLastAccessed updates the last accessed time for a session
	UpdateLastAccessed(ctx context.Context, sessionID string) error

	// Close closes the session store connection
	Close() error
}

// ProxyManager handles request proxying to sessions
type ProxyManager interface {
	ProxyHTTP(ctx context.Context, sessionID, userID string, req *orchestratorpb.ProxyHTTPRequest) (*orchestratorpb.ProxyHTTPResponse, error)
	GetSessionEndpoint(ctx context.Context, sessionID string) (string, error)
}
