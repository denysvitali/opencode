package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

// Config holds the orchestrator configuration needed by proxy manager
type Config struct {
	Namespace string
}

// HTTPProxyManager implements ProxyManager using HTTP forwarding
type HTTPProxyManager struct {
	config *Config
	client *http.Client
}

// NewHTTPProxyManager creates a new HTTP proxy manager
func NewHTTPProxyManager(config *Config) (*HTTPProxyManager, error) {
	return &HTTPProxyManager{
		config: config,
		client: &http.Client{
			Timeout: 0, // No timeout for streaming
		},
	}, nil
}

// ProxyHTTP forwards HTTP requests to the session pod
func (m *HTTPProxyManager) ProxyHTTP(ctx context.Context, sessionID, userID string, req *orchestratorpb.ProxyHTTPRequest) (*orchestratorpb.ProxyHTTPResponse, error) {
	// Get session endpoint
	endpoint, err := m.GetSessionEndpoint(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session endpoint: %w", err)
	}
	
	// Build target URL
	targetURL := fmt.Sprintf("http://%s%s", endpoint, req.Path)
	
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}
	
	// Execute request
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Build response headers
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	
	return &orchestratorpb.ProxyHTTPResponse{
		StatusCode: int32(resp.StatusCode),
		Headers:    headers,
		Body:       body,
	}, nil
}

// GetSessionEndpoint returns the internal endpoint for a session
func (m *HTTPProxyManager) GetSessionEndpoint(ctx context.Context, sessionID string) (string, error) {
	// For now, use the conventional service endpoint
	// In production, this should query Kubernetes service/endpoint
	podName := fmt.Sprintf("opencode-session-%s", sessionID[:8])
	return fmt.Sprintf("%s.%s.svc.cluster.local:8081", podName, m.config.Namespace), nil
}
