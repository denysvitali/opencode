package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/opencode-ai/opencode/internal/orchestrator/models"
	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
	opencodepb "github.com/opencode-ai/opencode/internal/proto/v1"
)

// ProxyManager handles gRPC proxying to sessions
type ProxyManager struct {
	runtime       models.Runtime
	grpcClients   sync.Map // sessionID -> *grpc.ClientConn
	sessionMgr    models.SessionManager
}

// sessionClient holds gRPC connection information
type sessionClient struct {
	conn         *grpc.ClientConn
	client       opencodepb.OpenCodeServiceClient
	lastUsed     time.Time
	mu           sync.Mutex
}

// NewProxyManager creates a new gRPC proxy manager
func NewProxyManager(runtime models.Runtime, sessionMgr models.SessionManager) *ProxyManager {
	return &ProxyManager{
		runtime:    runtime,
		sessionMgr: sessionMgr,
	}
}

// ProxyHTTP is a simple HTTP proxy - frontend can still use HTTP/WebSocket
// while orchestrator can use gRPC internally when needed for orchestration
func (m *ProxyManager) ProxyHTTP(ctx context.Context, sessionID, userID string, req *orchestratorpb.ProxyHTTPRequest) (*orchestratorpb.ProxyHTTPResponse, error) {
	// Get the session endpoint
	endpoint, err := m.runtime.GetSessionEndpoint(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session endpoint: %w", err)
	}

	// For most cases, we'll just pass through to the session's HTTP endpoint
	// The frontend continues to use HTTP/WebSocket as before
	// Only use gRPC internally when the orchestrator needs direct typed access
	
	// Simple proxy response - in a real implementation you'd make the HTTP request
	return &orchestratorpb.ProxyHTTPResponse{
		StatusCode: 200,
		Headers:    map[string]string{
			"Content-Type":                "application/json",
			"X-OpenCode-Session-Endpoint": endpoint,
		},
		Body: []byte(fmt.Sprintf(`{"message": "proxy to %s", "session_id": "%s"}`, endpoint, sessionID)),
	}, nil
}

// getOrCreateClient gets or creates a gRPC client for a session
func (m *ProxyManager) getOrCreateClient(ctx context.Context, sessionID string) (opencodepb.OpenCodeServiceClient, error) {
	// Check if we have a cached client
	if cached, ok := m.grpcClients.Load(sessionID); ok {
		sessionClient := cached.(*sessionClient)
		sessionClient.mu.Lock()
		sessionClient.lastUsed = time.Now()
		client := sessionClient.client
		sessionClient.mu.Unlock()
		return client, nil
	}

	// Get endpoint from runtime
	endpoint, err := m.runtime.GetSessionEndpoint(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session endpoint: %w", err)
	}

	// Create gRPC connection
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	// Create client
	client := opencodepb.NewOpenCodeServiceClient(conn)

	// Store in cache
	sessionClient := &sessionClient{
		conn:     conn,
		client:   client,
		lastUsed: time.Now(),
	}
	m.grpcClients.Store(sessionID, sessionClient)

	return client, nil
}

// GetSessionClient returns a typed gRPC client for when the orchestrator 
// needs direct access to session operations (health checks, session management, etc.)
func (m *ProxyManager) GetSessionClient(ctx context.Context, sessionID string) (opencodepb.OpenCodeServiceClient, error) {
	return m.getOrCreateClient(ctx, sessionID)
}

// CheckSessionHealth uses gRPC to check if a session is healthy
func (m *ProxyManager) CheckSessionHealth(ctx context.Context, sessionID string) error {
	client, err := m.getOrCreateClient(ctx, sessionID)
	if err != nil {
		return err
	}
	
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	_, err = client.Health(healthCtx, &opencodepb.HealthRequest{})
	return err
}

// MonitorSessionHealth starts background health monitoring for all sessions
func (m *ProxyManager) MonitorSessionHealth(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks checks health of all cached clients
func (m *ProxyManager) performHealthChecks(ctx context.Context) {
	m.grpcClients.Range(func(key, value interface{}) bool {
		sessionID := key.(string)
		sessionClient := value.(*sessionClient)
		
		// Check if client needs health check
		sessionClient.mu.Lock()
		lastUsed := sessionClient.lastUsed
		sessionClient.mu.Unlock()
		
		if time.Since(lastUsed) > 30*time.Second {
			// Use the typed health check method
			if err := m.CheckSessionHealth(ctx, sessionID); err != nil {
				// Remove unhealthy client
				sessionClient.conn.Close()
				m.grpcClients.Delete(sessionID)
			}
		}
		
		return true
	})
}

// GetHealthyEndpoints returns all healthy session endpoints
func (m *ProxyManager) GetHealthyEndpoints() map[string]string {
	endpoints := make(map[string]string)
	
	m.grpcClients.Range(func(key, value interface{}) bool {
		sessionID := key.(string)
		sessionClient := value.(*sessionClient)
		
		// Try a quick health check
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		_, err := sessionClient.client.Health(ctx, &opencodepb.HealthRequest{})
		if err == nil {
			// Get endpoint from runtime
			endpoint, _ := m.runtime.GetSessionEndpoint(context.Background(), sessionID)
			if endpoint != "" {
				endpoints[sessionID] = endpoint
			}
		}
		
		return true
	})
	
	return endpoints
}

// Close cleans up all gRPC connections
func (m *ProxyManager) Close() error {
	m.grpcClients.Range(func(key, value interface{}) bool {
		sessionClient := value.(*sessionClient)
		sessionClient.conn.Close()
		return true
	})
	return nil
}
