package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/opencode-ai/opencode/internal/orchestrator/models"
)

// ConnectionPool manages HTTP connections to session endpoints
type ConnectionPool struct {
	runtime      models.Runtime
	pools        sync.Map // sessionID -> *sessionPool
	poolConfig   PoolConfig
	healthChecker *HealthChecker
}

// PoolConfig holds configuration for connection pooling
type PoolConfig struct {
	MaxIdleConns        int           `json:"max_idle_conns"`
	MaxIdleConnsPerHost int           `json:"max_idle_conns_per_host"`
	IdleConnTimeout     time.Duration `json:"idle_conn_timeout"`
	RequestTimeout      time.Duration `json:"request_timeout"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	MaxRetries          int           `json:"max_retries"`
}

// DefaultPoolConfig returns sensible defaults for connection pooling
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		RequestTimeout:      30 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		MaxRetries:          3,
	}
}

// sessionPool holds connections for a specific session
type sessionPool struct {
	sessionID  string
	endpoint   string
	client     *http.Client
	healthy    bool
	lastCheck  time.Time
	lastAccess time.Time
	accessCount int64
	mu         sync.RWMutex
}

// NewConnectionPool creates a new connection pool manager
func NewConnectionPool(runtime models.Runtime, config PoolConfig) *ConnectionPool {
	cp := &ConnectionPool{
		runtime:    runtime,
		poolConfig: config,
	}

	// Initialize health checker
	cp.healthChecker = NewHealthChecker(config.HealthCheckInterval)
	
	// Start background cleanup
	go cp.cleanupIdlePools()
	
	return cp
}

// GetClient returns an HTTP client for the given session
func (cp *ConnectionPool) GetClient(ctx context.Context, sessionID string) (*http.Client, error) {
	// Get or create session pool
	pool, err := cp.getOrCreatePool(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	// Check if pool is healthy
	if !pool.healthy || time.Since(pool.lastCheck) > cp.poolConfig.HealthCheckInterval {
		healthy := cp.healthChecker.CheckHealth(ctx, pool.endpoint)
		pool.healthy = healthy
		pool.lastCheck = time.Now()
		
		if !healthy {
			return nil, fmt.Errorf("session %s is not healthy", sessionID)
		}
	}

	// Update access statistics
	pool.lastAccess = time.Now()
	pool.accessCount++

	return pool.client, nil
}

// getOrCreatePool gets an existing pool or creates a new one
func (cp *ConnectionPool) getOrCreatePool(ctx context.Context, sessionID string) (*sessionPool, error) {
	// Check if pool already exists
	if existing, ok := cp.pools.Load(sessionID); ok {
		return existing.(*sessionPool), nil
	}

	// Get session endpoint
	endpoint, err := cp.runtime.GetSessionEndpoint(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session endpoint: %w", err)
	}

	// Create new pool
	pool := &sessionPool{
		sessionID:  sessionID,
		endpoint:   endpoint,
		lastAccess: time.Now(),
		client: &http.Client{
			Timeout: cp.poolConfig.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        cp.poolConfig.MaxIdleConns,
				MaxIdleConnsPerHost: cp.poolConfig.MaxIdleConnsPerHost,
				IdleConnTimeout:     cp.poolConfig.IdleConnTimeout,
			},
		},
	}

	// Store the pool
	cp.pools.Store(sessionID, pool)
	
	// Initial health check
	pool.healthy = cp.healthChecker.CheckHealth(ctx, endpoint)
	pool.lastCheck = time.Now()

	return pool, nil
}

// RemovePool removes a session pool
func (cp *ConnectionPool) RemovePool(sessionID string) {
	if existing, ok := cp.pools.LoadAndDelete(sessionID); ok {
		pool := existing.(*sessionPool)
		// Close idle connections
		if transport, ok := pool.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

// GetPoolStats returns statistics about the connection pools
func (cp *ConnectionPool) GetPoolStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_pools": 0,
		"healthy_pools": 0,
		"total_access": int64(0),
	}

	totalPools := 0
	healthyPools := 0
	totalAccess := int64(0)

	cp.pools.Range(func(key, value interface{}) bool {
		totalPools++
		pool := value.(*sessionPool)
		
		pool.mu.RLock()
		if pool.healthy {
			healthyPools++
		}
		totalAccess += pool.accessCount
		pool.mu.RUnlock()
		
		return true
	})

	stats["total_pools"] = totalPools
	stats["healthy_pools"] = healthyPools
	stats["total_access"] = totalAccess

	return stats
}

// cleanupIdlePools removes pools that haven't been accessed recently
func (cp *ConnectionPool) cleanupIdlePools() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute) // Remove pools idle for 10+ minutes
		
		var toDelete []string
		cp.pools.Range(func(key, value interface{}) bool {
			sessionID := key.(string)
			pool := value.(*sessionPool)
			
			pool.mu.RLock()
			shouldDelete := pool.lastAccess.Before(cutoff)
			pool.mu.RUnlock()
			
			if shouldDelete {
				toDelete = append(toDelete, sessionID)
			}
			
			return true
		})
		
		for _, sessionID := range toDelete {
			cp.RemovePool(sessionID)
		}
	}
}

// Close cleans up all connection pools
func (cp *ConnectionPool) Close() {
	cp.pools.Range(func(key, value interface{}) bool {
		sessionID := key.(string)
		cp.RemovePool(sessionID)
		return true
	})
	
	if cp.healthChecker != nil {
		cp.healthChecker.Close()
	}
}

// HealthChecker performs health checks on session endpoints
type HealthChecker struct {
	interval time.Duration
	client   *http.Client
	stopCh   chan struct{}
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(interval time.Duration) *HealthChecker {
	return &HealthChecker{
		interval: interval,
		client: &http.Client{
			Timeout: 5 * time.Second, // Quick health check timeout
		},
		stopCh: make(chan struct{}),
	}
}

// CheckHealth performs a health check on the given endpoint
func (hc *HealthChecker) CheckHealth(ctx context.Context, endpoint string) bool {
	healthURL := endpoint + "/health"
	
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return false
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Close stops the health checker
func (hc *HealthChecker) Close() {
	close(hc.stopCh)
}
