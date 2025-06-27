package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

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
	// Label keys for session metadata
	SessionLabelKey       = "opencode.ai/session-id"
	UserLabelKey          = "opencode.ai/user-id"
	SessionTypeLabelKey   = "opencode.ai/session-type"
	SessionStateLabelKey  = "opencode.ai/session-state"  // New: for state-based queries
	CreatedAtLabelKey     = "opencode.ai/created-at"
	LastAccessedLabelKey  = "opencode.ai/last-accessed"
	SessionTitleLabelKey  = "opencode.ai/session-title"
	
	// Session type constant
	SessionTypeValue = "opencode-session"
	
	// ConfigMap data keys
	SessionDataKey = "session.json"
	MetadataKey    = "metadata.json"
	
	// Annotations for richer metadata
	AnnotationNamespace        = "opencode.ai/"
	AnnotationCreatedAtRFC3339 = AnnotationNamespace + "created-at-rfc3339"
	AnnotationUpdatedAtRFC3339 = AnnotationNamespace + "updated-at-rfc3339"
	AnnotationLastAccessedRFC3339 = AnnotationNamespace + "last-accessed-rfc3339"
	AnnotationSessionName      = AnnotationNamespace + "session-name"
	AnnotationUserEmail        = AnnotationNamespace + "user-email"
	AnnotationRepository       = AnnotationNamespace + "repository"
	AnnotationBranch          = AnnotationNamespace + "branch"
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
	sessionID := sessionReq.GetUserId() + "-" + fmt.Sprintf("%d", now.GetSeconds())
	
	session := &orchestratorpb.Session{
		Id:           sessionID,
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
				CreatedAtLabelKey:    strconv.FormatInt(now.GetSeconds(), 10),
				LastAccessedLabelKey: strconv.FormatInt(now.GetSeconds(), 10),
				SessionTitleLabelKey: k.sanitizeLabelValue(session.Name),
			},
			Annotations: map[string]string{
				AnnotationSessionName:     session.Name,
				AnnotationCreatedAtRFC3339: now.AsTime().Format(time.RFC3339),
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
	existingConfigMap.Labels[SessionTitleLabelKey] = k.sanitizeLabelValue(session.Name)
	existingConfigMap.Labels[LastAccessedLabelKey] = strconv.FormatInt(session.UpdatedAt.GetSeconds(), 10)

	// Update annotations
	if existingConfigMap.Annotations == nil {
		existingConfigMap.Annotations = make(map[string]string)
	}
	existingConfigMap.Annotations[AnnotationSessionName] = session.Name
	existingConfigMap.Annotations[AnnotationUpdatedAtRFC3339] = session.UpdatedAt.AsTime().Format(time.RFC3339)

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

	// Update annotation
	if existingConfigMap.Annotations == nil {
		existingConfigMap.Annotations = make(map[string]string)
	}
	existingConfigMap.Annotations[AnnotationLastAccessedRFC3339] = now.Format(time.RFC3339)

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
		// Check repository annotation
		if repoURL, exists := cm.Annotations[AnnotationRepository]; exists && repoURL == repositoryURL {
			session, err := k.parseSessionFromConfigMap(&cm)
			if err != nil {
				continue
			}
			sessions = append(sessions, session)
		}
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
		"total": len(configMapList.Items),
		"by_state": make(map[string]int),
		"by_user": make(map[string]int),
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
	// Kubernetes labels have restrictions, so we need to sanitize
	// For now, just truncate and remove invalid characters
	if len(value) > 63 {
		value = value[:60] + "..."
	}
	
	// TODO: Implement proper sanitization for Kubernetes label values
	// Replace invalid characters with valid ones
	
	return value
}
