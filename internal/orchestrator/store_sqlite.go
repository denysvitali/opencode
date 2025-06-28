package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "modernc.org/sqlite"

	"github.com/opencode-ai/opencode/internal/orchestrator/session"
	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

// SQLiteSessionStore implements SessionStore using SQLite
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore creates a new SQLite session store
func NewSQLiteSessionStore(config *session.Config) (*SQLiteSessionStore, error) {
	db, err := sql.Open("sqlite", config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteSessionStore{db: db}

	if err := store.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return store, nil
}

// createTables creates the sessions table
func (s *SQLiteSessionStore) createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		name TEXT,
		state INTEGER NOT NULL,
		config_json TEXT,
		status_json TEXT,
		labels_json TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		last_accessed INTEGER NOT NULL
	);
	
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_last_accessed ON sessions(last_accessed);
	CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
	`

	_, err := s.db.Exec(query)
	return err
}

// CreateSession creates a new session
func (s *SQLiteSessionStore) CreateSession(ctx context.Context, session *orchestratorpb.Session) error {
	configJSON, err := protojson.Marshal(session.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	statusJSON, err := protojson.Marshal(session.Status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	labelsJSON, err := json.Marshal(session.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	query := `
	INSERT INTO sessions (id, user_id, name, state, config_json, status_json, labels_json, created_at, updated_at, last_accessed)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		session.Id,
		session.UserId,
		session.Name,
		int(session.State),
		string(configJSON),
		string(statusJSON),
		string(labelsJSON),
		session.CreatedAt.AsTime().Unix(),
		session.UpdatedAt.AsTime().Unix(),
		session.LastAccessed.AsTime().Unix(),
	)

	if err != nil {
		return fmt.Errorf("failed to insert session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID
func (s *SQLiteSessionStore) GetSession(ctx context.Context, sessionID string) (*orchestratorpb.Session, error) {
	query := `
	SELECT id, user_id, name, state, config_json, status_json, labels_json, created_at, updated_at, last_accessed
	FROM sessions WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, sessionID)
	return s.scanSession(row)
}

// GetSessionByUser retrieves a session by ID and verifies user ownership
func (s *SQLiteSessionStore) GetSessionByUser(ctx context.Context, sessionID, userID string) (*orchestratorpb.Session, error) {
	query := `
	SELECT id, user_id, name, state, config_json, status_json, labels_json, created_at, updated_at, last_accessed
	FROM sessions WHERE id = ? AND user_id = ?
	`

	row := s.db.QueryRowContext(ctx, query, sessionID, userID)
	return s.scanSession(row)
}

// ListSessions lists all sessions for a user with pagination
func (s *SQLiteSessionStore) ListSessions(ctx context.Context, userID string, limit int, offset int) ([]*orchestratorpb.Session, error) {
	var query string
	var args []interface{}

	if userID == "" {
		query = `
		SELECT id, user_id, name, state, config_json, status_json, labels_json, created_at, updated_at, last_accessed
		FROM sessions ORDER BY created_at DESC LIMIT ? OFFSET ?
		`
		args = []interface{}{limit, offset}
	} else {
		query = `
		SELECT id, user_id, name, state, config_json, status_json, labels_json, created_at, updated_at, last_accessed
		FROM sessions WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, limit, offset}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*orchestratorpb.Session
	for rows.Next() {
		session, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// UpdateSession updates an existing session
func (s *SQLiteSessionStore) UpdateSession(ctx context.Context, session *orchestratorpb.Session) error {
	configJSON, err := protojson.Marshal(session.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	statusJSON, err := protojson.Marshal(session.Status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	labelsJSON, err := json.Marshal(session.Labels)
	if err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	}

	session.UpdatedAt = timestamppb.Now()

	query := `
	UPDATE sessions SET name = ?, state = ?, config_json = ?, status_json = ?, labels_json = ?, updated_at = ?
	WHERE id = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		session.Name,
		int(session.State),
		string(configJSON),
		string(statusJSON),
		string(labelsJSON),
		session.UpdatedAt.AsTime().Unix(),
		session.Id,
	)

	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

// DeleteSession removes a session
func (s *SQLiteSessionStore) DeleteSession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM sessions WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

// CountSessions returns the total number of sessions for a user
func (s *SQLiteSessionStore) CountSessions(ctx context.Context, userID string) (int, error) {
	var query string
	var args []interface{}

	if userID == "" {
		query = `SELECT COUNT(*) FROM sessions`
		args = []interface{}{}
	} else {
		query = `SELECT COUNT(*) FROM sessions WHERE user_id = ?`
		args = []interface{}{userID}
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count sessions: %w", err)
	}

	return count, nil
}

// ListExpiredSessions returns sessions that haven't been accessed within the TTL
func (s *SQLiteSessionStore) ListExpiredSessions(ctx context.Context, ttlSeconds int64) ([]*orchestratorpb.Session, error) {
	cutoff := time.Now().Add(-time.Duration(ttlSeconds) * time.Second).Unix()

	query := `
	SELECT id, user_id, name, state, config_json, status_json, labels_json, created_at, updated_at, last_accessed
	FROM sessions WHERE last_accessed < ?
	`

	rows, err := s.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*orchestratorpb.Session
	for rows.Next() {
		session, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// UpdateLastAccessed updates the last accessed time for a session
func (s *SQLiteSessionStore) UpdateLastAccessed(ctx context.Context, sessionID string) error {
	query := `UPDATE sessions SET last_accessed = ? WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, time.Now().Unix(), sessionID)
	if err != nil {
		return fmt.Errorf("failed to update last accessed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteSessionStore) Close() error {
	return s.db.Close()
}

// scanSession is a helper interface that can be either *sql.Row or *sql.Rows
type scannable interface {
	Scan(dest ...interface{}) error
}

// scanSession scans a session from a database row
func (s *SQLiteSessionStore) scanSession(row scannable) (*orchestratorpb.Session, error) {
	var (
		id, userID, name                   string
		state                              int
		configJSON, statusJSON, labelsJSON string
		createdAt, updatedAt, lastAccessed int64
	)

	err := row.Scan(&id, &userID, &name, &state, &configJSON, &statusJSON, &labelsJSON, &createdAt, &updatedAt, &lastAccessed)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("failed to scan session: %w", err)
	}

	// Unmarshal config
	config := &orchestratorpb.SessionConfig{}
	if err := protojson.Unmarshal([]byte(configJSON), config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Unmarshal status
	status := &orchestratorpb.SessionStatus{}
	if err := protojson.Unmarshal([]byte(statusJSON), status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status: %w", err)
	}

	// Unmarshal labels
	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		return nil, fmt.Errorf("failed to unmarshal labels: %w", err)
	}

	session := &orchestratorpb.Session{
		Id:           id,
		UserId:       userID,
		Name:         name,
		State:        orchestratorpb.SessionState(state),
		Config:       config,
		Status:       status,
		Labels:       labels,
		CreatedAt:    timestamppb.New(time.Unix(createdAt, 0)),
		UpdatedAt:    timestamppb.New(time.Unix(updatedAt, 0)),
		LastAccessed: timestamppb.New(time.Unix(lastAccessed, 0)),
	}

	return session, nil
}
