package orchestrator

import (
	"context"

	"github.com/opencode-ai/opencode/internal/orchestrator/models"
	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

// SimpleProxyManager implements ProxyManager using the runtime interface
type SimpleProxyManager struct {
	runtime models.Runtime
}

// ProxyHTTP forwards HTTP requests to the session 
func (p *SimpleProxyManager) ProxyHTTP(ctx context.Context, sessionID, userID string, req *orchestratorpb.ProxyHTTPRequest) (*orchestratorpb.ProxyHTTPResponse, error) {
	// For now, just return a simple response
	// In a full implementation, this would forward the HTTP request to the session endpoint
	return &orchestratorpb.ProxyHTTPResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       []byte("Proxy not yet fully implemented"),
	}, nil
}

// GetSessionEndpoint returns the internal endpoint for a session
func (p *SimpleProxyManager) GetSessionEndpoint(ctx context.Context, sessionID string) (string, error) {
	return p.runtime.GetSessionEndpoint(ctx, sessionID)
}
