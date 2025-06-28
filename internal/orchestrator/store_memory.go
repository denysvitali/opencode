package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

// InMemorySessionStore implements SessionStore using in-memory storage
type InMemorySessionStore struct {
	sessions map[string]*orchestratorpb.Session
	mu       sync.RWMutex
}

// NewInMemorySessionStore creates a new in-memory session store
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[string]*orchestratorpb.Session),
	}
}

// CreateSession creates a new session
func (s *InMemorySessionStore) CreateSession(ctx context.Context, session *orchestratorpb.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.Id]; exists {
		return fmt.Errorf("session with ID %s already exists", session.Id)
	}

	s.sessions[session.Id] = session
	return nil
}

// GetSession retrieves a session by ID
func (s *InMemorySessionStore) GetSession(ctx context.Context, sessionID string) (*orchestratorpb.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	return session, nil
}

// GetSessionByUser retrieves a session by ID and verifies user ownership
func (s *InMemorySessionStore) GetSessionByUser(ctx context.Context, sessionID, userID string) (*orchestratorpb.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}

	if session.UserId != userID {
		return nil, fmt.Errorf("session not found")
	}

	return session, nil
}

// ListSessions lists all sessions for a user with pagination
func (s *InMemorySessionStore) ListSessions(ctx context.Context, userID string, limit int, offset int) ([]*orchestratorpb.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var userSessions []*orchestratorpb.Session
	for _, session := range s.sessions {
		if userID == "" || session.UserId == userID {
			userSessions = append(userSessions, session)
		}
	}

	// Apply pagination
	if offset >= len(userSessions) {
		return []*orchestratorpb.Session{}, nil
	}

	end := offset + limit
	if end > len(userSessions) {
		end = len(userSessions)
	}

	return userSessions[offset:end], nil
}

// UpdateSession updates an existing session
func (s *InMemorySessionStore) UpdateSession(ctx context.Context, session *orchestratorpb.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.Id]; !exists {
		return fmt.Errorf("session not found")
	}

	session.UpdatedAt = timestamppb.Now()
	s.sessions[session.Id] = session
	return nil
}

// DeleteSession removes a session
func (s *InMemorySessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[sessionID]; !exists {
		return fmt.Errorf("session not found")
	}

	delete(s.sessions, sessionID)
	return nil
}

// CountSessions returns the total number of sessions for a user
func (s *InMemorySessionStore) CountSessions(ctx context.Context, userID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if userID == "" {
		return len(s.sessions), nil
	}

	count := 0
	for _, session := range s.sessions {
		if session.UserId == userID {
			count++
		}
	}

	return count, nil
}

// ListExpiredSessions returns sessions that haven't been accessed within the TTL
func (s *InMemorySessionStore) ListExpiredSessions(ctx context.Context, ttlSeconds int64) ([]*orchestratorpb.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var expiredSessions []*orchestratorpb.Session
	cutoff := time.Now().Add(-time.Duration(ttlSeconds) * time.Second)

	for _, session := range s.sessions {
		if session.LastAccessed != nil {
			lastAccessed := session.LastAccessed.AsTime()
			if lastAccessed.Before(cutoff) {
				expiredSessions = append(expiredSessions, session)
			}
		}
	}

	return expiredSessions, nil
}

// UpdateLastAccessed updates the last accessed time for a session
func (s *InMemorySessionStore) UpdateLastAccessed(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found")
	}

	session.LastAccessed = timestamppb.Now()
	return nil
}

// Close closes the session store connection (no-op for in-memory)
func (s *InMemorySessionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]*orchestratorpb.Session)
	return nil
}
