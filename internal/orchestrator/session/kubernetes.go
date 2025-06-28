package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/opencode-ai/opencode/internal/orchestrator/models"
	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

const (
	// Label keys for technical metadata only
	SessionLabelKey      = "opencode.ai/session-id"
	UserLabelKey         = "opencode.ai/user-id"
	SessionTypeLabelKey  = "opencode.ai/session-type"
	SessionStateLabelKey = "opencode.ai/session-state"
	CreatedAtLabelKey    = "opencode.ai/created-at"
	LastAccessedLabelKey = "opencode.ai/last-accessed"

	// Session type constant
	SessionTypeValue = "opencode-session"

	// ConfigMap data keys - all user-facing data goes here
	SessionDataKey = "session.json"
	MetadataKey    = "metadata.json"
)

// KubernetesSessionStore implements SessionManager using Kubernetes ConfigMaps
type KubernetesSessionStore struct {
	client    kubernetes.Interface
	namespace string
}

var _ models.SessionManager = (*KubernetesSessionStore)(nil)

// SessionMetadata holds additional session metadata
type SessionMetadata struct {
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Custom      map[string]string `json:"custom,omitempty"`
}

// NewKubernetesSessionStore creates a new Kubernetes session store
func NewKubernetesSessionStore(config *Config) (*KubernetesSessionStore, error) {
	var kubeConfig *rest.Config
	var err error

	if config.KubeConfigPath != "" {
		// Use provided kubeconfig
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
	} else {
		// Use in-cluster config
		kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build in-cluster config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &KubernetesSessionStore{
		client:    client,
		namespace: config.Namespace,
	}, nil
}

// CreateSession creates a new session by creating a ConfigMap
func (k *KubernetesSessionStore) CreateSession(ctx context.Context, sessionReq *orchestratorpb.CreateSessionRequest) (*orchestratorpb.Session, error) {
	now := timestamppb.Now()

	// Generate session ID if not provided
	sessionID := uuid.New()

	session := &orchestratorpb.Session{
		Id:           sessionID.String(),
		UserId:       sessionReq.UserId,
		Name:         sessionReq.Name,
		State:        orchestratorpb.SessionState_SESSION_STATE_CREATING,
		Config:       sessionReq.Config,
		Labels:       sessionReq.Labels,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
		Status: &orchestratorpb.SessionStatus{
			Ready:   false,
			Message: "Session created",
		},
	}

	// Create metadata
	metadata := SessionMetadata{
		Description: sessionReq.GetLabels()["description"],
		Tags:        []string{}, // Can be extracted from labels if needed
		Custom:      sessionReq.Labels,
	}

	// Marshal session data
	sessionData, err := protojson.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session: %w", err)
	}

	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Create ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.sessionConfigMapName(session.Id),
			Namespace: k.namespace,
			Labels: map[string]string{
				SessionLabelKey:      session.Id,
				UserLabelKey:         session.UserId,
				SessionTypeLabelKey:  SessionTypeValue,
				SessionStateLabelKey: session.State.String(),
				CreatedAtLabelKey:    strconv.FormatInt(now.GetSeconds(), 10),
				LastAccessedLabelKey: strconv.FormatInt(now.GetSeconds(), 10),
			},
		},
		Data: map[string]string{
			SessionDataKey: string(sessionData),
			MetadataKey:    string(metadataData),
		},
	}

	// Add custom labels from request
	for key, value := range sessionReq.Labels {
		configMap.Labels["custom."+key] = k.sanitizeLabelValue(value)
	}

	// Create the ConfigMap
	createdConfigMap, err := k.client.CoreV1().ConfigMaps(k.namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create session ConfigMap: %w", err)
	}

	// Update session with creation timestamp from Kubernetes
	session.CreatedAt = timestamppb.New(createdConfigMap.CreationTimestamp.Time)
	session.UpdatedAt = session.CreatedAt

	return session, nil
}

// GetSession retrieves a session by ID
func (k *KubernetesSessionStore) GetSession(ctx context.Context, sessionID string, userID string) (*orchestratorpb.Session, error) {
	configMap, err := k.client.CoreV1().ConfigMaps(k.namespace).Get(ctx, k.sessionConfigMapName(sessionID), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("failed to get session ConfigMap: %w", err)
	}

	// Verify user ownership
	if configMap.Labels[UserLabelKey] != userID {
		return nil, fmt.Errorf("session not found or access denied: %s", sessionID)
	}

	return k.parseSessionFromConfigMap(configMap)
}

// GetSessionByUser retrieves a session by ID and verifies user ownership
func (k *KubernetesSessionStore) GetSessionByUser(ctx context.Context, sessionID, userID string) (*orchestratorpb.Session, error) {
	return k.GetSession(ctx, sessionID, userID)
}

// ListSessions lists all sessions for a user with pagination
func (k *KubernetesSessionStore) ListSessions(ctx context.Context, userID string, limit int32, pageToken string) ([]*orchestratorpb.Session, string, error) {
	// Create label selector for user sessions
	selector := labels.Set{
		UserLabelKey:        userID,
		SessionTypeLabelKey: SessionTypeValue,
	}.AsSelector()

	listOptions := metav1.ListOptions{
		LabelSelector: selector.String(),
	}

	if limit > 0 {
		listOptions.Limit = int64(limit)
	}

	if pageToken != "" {
		listOptions.Continue = pageToken
	}

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, listOptions)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list session ConfigMaps: %w", err)
	}

	var sessions []*orchestratorpb.Session
	for _, cm := range configMapList.Items {
		session, err := k.parseSessionFromConfigMap(&cm)
		if err != nil {
			// Log error but continue with other sessions
			continue
		}
		sessions = append(sessions, session)
	}

	return sessions, configMapList.Continue, nil
}

// UpdateSession updates an existing session
func (k *KubernetesSessionStore) UpdateSession(ctx context.Context, session *orchestratorpb.Session) error {
	configMapName := k.sessionConfigMapName(session.Id)

	// Get existing ConfigMap
	existingConfigMap, err := k.client.CoreV1().ConfigMaps(k.namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get existing ConfigMap: %w", err)
	}

	// Update session timestamp
	session.UpdatedAt = timestamppb.Now()

	// Marshal updated session data
	sessionData, err := protojson.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Update ConfigMap data
	existingConfigMap.Data[SessionDataKey] = string(sessionData)

	// Update labels
	if existingConfigMap.Labels == nil {
		existingConfigMap.Labels = make(map[string]string)
	}
	existingConfigMap.Labels[LastAccessedLabelKey] = strconv.FormatInt(session.UpdatedAt.GetSeconds(), 10)
	existingConfigMap.Labels[SessionStateLabelKey] = session.State.String()

	// Update the ConfigMap
	_, err = k.client.CoreV1().ConfigMaps(k.namespace).Update(ctx, existingConfigMap, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update session ConfigMap: %w", err)
	}

	return nil
}

// DeleteSession removes a session
func (k *KubernetesSessionStore) DeleteSession(ctx context.Context, sessionID string, userID string, force bool) error {
	// First verify ownership unless force is true
	if !force {
		_, err := k.GetSession(ctx, sessionID, userID)
		if err != nil {
			return err
		}
	}

	// Delete the ConfigMap
	err := k.client.CoreV1().ConfigMaps(k.namespace).Delete(ctx, k.sessionConfigMapName(sessionID), metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete session ConfigMap: %w", err)
	}

	return nil
}

// CountSessions returns the total number of sessions for a user
func (k *KubernetesSessionStore) CountSessions(ctx context.Context, userID string) (int, error) {
	selector := labels.Set{
		UserLabelKey:        userID,
		SessionTypeLabelKey: SessionTypeValue,
	}.AsSelector()

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to count session ConfigMaps: %w", err)
	}

	return len(configMapList.Items), nil
}

// ListExpiredSessions returns sessions that haven't been accessed within the TTL
func (k *KubernetesSessionStore) ListExpiredSessions(ctx context.Context, ttl int64) ([]*orchestratorpb.Session, error) {
	// List all sessions
	selector := labels.Set{
		SessionTypeLabelKey: SessionTypeValue,
	}.AsSelector()

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session ConfigMaps: %w", err)
	}

	var expiredSessions []*orchestratorpb.Session
	now := time.Now().Unix()

	for _, cm := range configMapList.Items {
		// Check last accessed time from label
		lastAccessedStr, exists := cm.Labels[LastAccessedLabelKey]
		if !exists {
			// If no last accessed time, use creation time
			lastAccessedStr = cm.Labels[CreatedAtLabelKey]
		}

		lastAccessed, err := strconv.ParseInt(lastAccessedStr, 10, 64)
		if err != nil {
			continue // Skip sessions with invalid timestamps
		}

		if now-lastAccessed > ttl {
			session, err := k.parseSessionFromConfigMap(&cm)
			if err != nil {
				continue // Skip sessions that can't be parsed
			}
			expiredSessions = append(expiredSessions, session)
		}
	}

	return expiredSessions, nil
}

// UpdateLastAccessed updates the last accessed time for a session
func (k *KubernetesSessionStore) UpdateLastAccessed(ctx context.Context, sessionID string) error {
	configMapName := k.sessionConfigMapName(sessionID)

	// Get existing ConfigMap
	existingConfigMap, err := k.client.CoreV1().ConfigMaps(k.namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Update last accessed label
	now := time.Now()
	if existingConfigMap.Labels == nil {
		existingConfigMap.Labels = make(map[string]string)
	}
	existingConfigMap.Labels[LastAccessedLabelKey] = strconv.FormatInt(now.Unix(), 10)

	// Update the ConfigMap
	_, err = k.client.CoreV1().ConfigMaps(k.namespace).Update(ctx, existingConfigMap, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	return nil
}

// Close closes the session store connection (no-op for Kubernetes)
func (k *KubernetesSessionStore) Close() error {
	return nil
}

// Additional helper methods for enhanced Kubernetes session management

// ListSessionsByState returns sessions in a specific state for a user
func (k *KubernetesSessionStore) ListSessionsByState(ctx context.Context, userID string, state orchestratorpb.SessionState) ([]*orchestratorpb.Session, error) {
	selector := labels.Set{
		UserLabelKey:         userID,
		SessionTypeLabelKey:  SessionTypeValue,
		SessionStateLabelKey: state.String(),
	}.AsSelector()

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session ConfigMaps by state: %w", err)
	}

	var sessions []*orchestratorpb.Session
	for _, cm := range configMapList.Items {
		session, err := k.parseSessionFromConfigMap(&cm)
		if err != nil {
			continue // Skip sessions that can't be parsed
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// CleanupExpiredSessions deletes sessions that haven't been accessed within the TTL
// This can be called periodically by the orchestrator for garbage collection
func (k *KubernetesSessionStore) CleanupExpiredSessions(ctx context.Context, ttl int64) (int, error) {
	expiredSessions, err := k.ListExpiredSessions(ctx, ttl)
	if err != nil {
		return 0, fmt.Errorf("failed to list expired sessions: %w", err)
	}

	deletedCount := 0
	for _, session := range expiredSessions {
		err := k.DeleteSession(ctx, session.Id, session.UserId, true) // force=true for cleanup
		if err != nil {
			// Log error but continue with other sessions
			continue
		}
		deletedCount++
	}

	return deletedCount, nil
}

// ListSessionsByRepository returns sessions for a specific repository
// Note: Since repository info is stored in ConfigMap data (not annotations), we need to scan all user sessions
func (k *KubernetesSessionStore) ListSessionsByRepository(ctx context.Context, userID, repositoryURL string) ([]*orchestratorpb.Session, error) {
	// First get all user sessions
	selector := labels.Set{
		UserLabelKey:        userID,
		SessionTypeLabelKey: SessionTypeValue,
	}.AsSelector()

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session ConfigMaps: %w", err)
	}

	var sessions []*orchestratorpb.Session
	for _, cm := range configMapList.Items {
		session, err := k.parseSessionFromConfigMap(&cm)
		if err != nil {
			continue
		}

		// Filter by repository URL - this would need to be implemented based on
		// how repository information is stored in the session data
		// For now, return all sessions for the user
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetSessionStats returns statistics about sessions in the cluster
func (k *KubernetesSessionStore) GetSessionStats(ctx context.Context) (map[string]interface{}, error) {
	selector := labels.Set{
		SessionTypeLabelKey: SessionTypeValue,
	}.AsSelector()

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session ConfigMaps: %w", err)
	}

	stats := map[string]interface{}{
		"total":    len(configMapList.Items),
		"by_state": make(map[string]int),
		"by_user":  make(map[string]int),
	}

	stateStats := stats["by_state"].(map[string]int)
	userStats := stats["by_user"].(map[string]int)

	for _, cm := range configMapList.Items {
		// Count by state
		if state, exists := cm.Labels[SessionStateLabelKey]; exists {
			stateStats[state]++
		}

		// Count by user
		if userID, exists := cm.Labels[UserLabelKey]; exists {
			userStats[userID]++
		}
	}

	return stats, nil
}

// GetAllSessions returns all sessions in the cluster (admin function)
func (k *KubernetesSessionStore) GetAllSessions(ctx context.Context) ([]*orchestratorpb.Session, error) {
	selector := labels.Set{
		SessionTypeLabelKey: SessionTypeValue,
	}.AsSelector()

	configMapList, err := k.client.CoreV1().ConfigMaps(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session ConfigMaps: %w", err)
	}

	var sessions []*orchestratorpb.Session
	for _, cm := range configMapList.Items {
		session, err := k.parseSessionFromConfigMap(&cm)
		if err != nil {
			continue // Skip sessions that can't be parsed
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// Helper methods

func (k *KubernetesSessionStore) sessionConfigMapName(sessionID string) string {
	return fmt.Sprintf("session-%s", sessionID)
}

func (k *KubernetesSessionStore) parseSessionFromConfigMap(cm *corev1.ConfigMap) (*orchestratorpb.Session, error) {
	sessionData, exists := cm.Data[SessionDataKey]
	if !exists {
		return nil, fmt.Errorf("session data not found in ConfigMap")
	}

	var session orchestratorpb.Session
	err := protojson.Unmarshal([]byte(sessionData), &session)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &session, nil
}

func (k *KubernetesSessionStore) sanitizeLabelValue(value string) string {
	// Kubernetes label values have strict requirements:
	// - Must be 63 characters or less
	// - Must begin and end with an alphanumeric character
	// - May contain dashes (-), underscores (_), dots (.), and alphanumerics between

	if value == "" {
		return ""
	}

	// Convert to lowercase and replace invalid characters
	sanitized := ""
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sanitized += string(r)
		} else if r == '-' || r == '_' || r == '.' {
			sanitized += string(r)
		} else {
			sanitized += "-" // Replace invalid chars with dash
		}
	}

	// Ensure it starts and ends with alphanumeric
	if len(sanitized) > 0 {
		// Trim leading non-alphanumeric
		for len(sanitized) > 0 && !isAlphanumeric(sanitized[0]) {
			sanitized = sanitized[1:]
		}
		// Trim trailing non-alphanumeric
		for len(sanitized) > 0 && !isAlphanumeric(sanitized[len(sanitized)-1]) {
			sanitized = sanitized[:len(sanitized)-1]
		}
	}

	// Truncate if too long
	if len(sanitized) > 63 {
		sanitized = sanitized[:60] + "---"
	}

	// If empty after sanitization, use a default value
	if sanitized == "" {
		sanitized = "unknown"
	}

	return sanitized
}

func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
