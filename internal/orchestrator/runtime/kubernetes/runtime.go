package kubernetes

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/opencode-ai/opencode/internal/orchestrator/models"
	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

// Runtime implements the runtime interface for Kubernetes
type Runtime struct {
	client kubernetes.Interface
	config *models.KubernetesConfig
}

// NewRuntime creates a new Kubernetes runtime
func NewRuntime(config *models.KubernetesConfig) (*Runtime, error) {
	// Check if we're in a Kubernetes cluster
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// Try to use kubectl config
		cfg, err = clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Runtime{
		client: client,
		config: config,
	}, nil
}

// CreateSession creates a new session in Kubernetes
func (r *Runtime) CreateSession(ctx context.Context, session *orchestratorpb.Session) error {
	// Create PVC first
	if err := r.createPVC(ctx, session); err != nil {
		return fmt.Errorf("failed to create PVC: %w", err)
	}

	// Create pod
	if err := r.createPod(ctx, session); err != nil {
		// Cleanup PVC on pod creation failure
		_ = r.deletePVC(ctx, session.Id)
		return fmt.Errorf("failed to create pod: %w", err)
	}

	return nil
}

// GetSessionStatus returns the current status of a session
func (r *Runtime) GetSessionStatus(ctx context.Context, sessionID string) (*orchestratorpb.SessionStatus, error) {
	podName := fmt.Sprintf("opencode-session-%s", sessionID[:8])

	pod, err := r.client.CoreV1().Pods(r.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	status := &orchestratorpb.SessionStatus{
		PodName:          podName,
		PodNamespace:     r.config.Namespace,
		PvcName:          fmt.Sprintf("opencode-workspace-%s", sessionID[:8]),
		InternalEndpoint: fmt.Sprintf("%s.%s.svc.cluster.local:8081", podName, r.config.Namespace),
		Ready:            false,
		Message:          string(pod.Status.Phase),
	}

	// Check if pod is ready
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			status.Ready = true
			status.ReadyAt = timestamppb.Now()
			break
		}
	}

	return status, nil
}

// DeleteSession removes a session from Kubernetes
func (r *Runtime) DeleteSession(ctx context.Context, sessionID string) error {
	if err := r.deletePod(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}
	if err := r.deletePVC(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete PVC: %w", err)
	}
	return nil
}

// WaitForSessionReady waits for a session to become ready
func (r *Runtime) WaitForSessionReady(ctx context.Context, sessionID string) error {
	podName := fmt.Sprintf("opencode-session-%s", sessionID[:8])
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pod, err := r.client.CoreV1().Pods(r.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

// GetSessionEndpoint returns the network endpoint for the session
func (r *Runtime) GetSessionEndpoint(ctx context.Context, sessionID string) (string, error) {
	podName := fmt.Sprintf("opencode-session-%s", sessionID[:8])
	return fmt.Sprintf("%s.%s.svc.cluster.local:8081", podName, r.config.Namespace), nil
}

// ListSessions returns all sessions managed by this runtime
func (r *Runtime) ListSessions(ctx context.Context) ([]*orchestratorpb.Session, error) {
	pods, err := r.client.CoreV1().Pods(r.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=opencode-session",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var sessions []*orchestratorpb.Session
	for _, pod := range pods.Items {
		sessionID := pod.Labels["session-id"]
		userID := pod.Labels["user-id"]

		if sessionID == "" || userID == "" {
			continue
		}

		session := &orchestratorpb.Session{
			Id:     sessionID,
			UserId: userID,
			State:  r.podPhaseToSessionState(pod.Status.Phase),
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// HealthCheck performs a health check of the runtime
func (r *Runtime) HealthCheck(ctx context.Context) error {
	// Check if we can list nodes (basic connectivity test)
	_, err := r.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

// Close cleans up runtime resources
func (r *Runtime) Close() error {
	// No cleanup needed for Kubernetes client
	return nil
}

// Helper methods for pod and PVC management

func (r *Runtime) createPod(ctx context.Context, session *orchestratorpb.Session) error {
	podName := fmt.Sprintf("opencode-session-%s", session.Id[:8])
	pvcName := fmt.Sprintf("opencode-workspace-%s", session.Id[:8])

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: r.config.Namespace,
			Labels: map[string]string{
				"app":        "opencode-session",
				"session-id": session.Id,
				"user-id":    session.UserId,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "opencode",
					Image: r.config.Image,
					Ports: []corev1.ContainerPort{
						{
							Name:          "grpc",
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
						{
							Name:          "http",
							ContainerPort: 8081,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Resources: r.parseResources(),
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "workspace",
							MountPath: "/workspace",
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "GRPC_PORT",
							Value: "8080",
						},
						{
							Name:  "HTTP_PORT",
							Value: "8081",
						},
					},
					Command: []string{"./opencode", "server"},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromInt(8081),
							},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       5,
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromInt(8081),
							},
						},
						InitialDelaySeconds: 15,
						PeriodSeconds:       20,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "workspace",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	// Add environment variables from session config
	if session.Config != nil && session.Config.Environment != nil {
		for key, value := range session.Config.Environment {
			pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
				Name:  key,
				Value: value,
			})
		}
	}

	_, err := r.client.CoreV1().Pods(r.config.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func (r *Runtime) parseResources() corev1.ResourceRequirements {
	resReq := corev1.ResourceRequirements{
		Requests: make(corev1.ResourceList),
		Limits:   make(corev1.ResourceList),
	}

	// Helper function to add resources if they exist
	addResource := func(target corev1.ResourceList, res models.ResourceList) {
		if res.CPU != "" {
			target[corev1.ResourceCPU] = resource.MustParse(res.CPU)
		}
		if res.Memory != "" {
			target[corev1.ResourceMemory] = resource.MustParse(res.Memory)
		}
	}

	addResource(resReq.Requests, r.config.Resources.Requests)
	addResource(resReq.Limits, r.config.Resources.Limits)

	return resReq
}

func (r *Runtime) deletePod(ctx context.Context, sessionID string) error {
	podName := fmt.Sprintf("opencode-session-%s", sessionID[:8])
	return r.client.CoreV1().Pods(r.config.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

func (r *Runtime) createPVC(ctx context.Context, session *orchestratorpb.Session) error {
	pvcName := fmt.Sprintf("opencode-workspace-%s", session.Id)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: r.config.Namespace,
			Labels: map[string]string{
				"app":        "opencode-session",
				"session-id": session.Id,
				"user-id":    session.UserId,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(r.config.StorageSize),
				},
			},
		},
	}

	_, err := r.client.CoreV1().PersistentVolumeClaims(r.config.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	return err
}

func (r *Runtime) deletePVC(ctx context.Context, sessionID string) error {
	pvcName := fmt.Sprintf("opencode-workspace-%s", sessionID[:8])
	return r.client.CoreV1().PersistentVolumeClaims(r.config.Namespace).Delete(ctx, pvcName, metav1.DeleteOptions{})
}

func (r *Runtime) podPhaseToSessionState(phase corev1.PodPhase) orchestratorpb.SessionState {
	switch phase {
	case corev1.PodPending:
		return orchestratorpb.SessionState_SESSION_STATE_CREATING
	case corev1.PodRunning:
		return orchestratorpb.SessionState_SESSION_STATE_RUNNING
	case corev1.PodSucceeded, corev1.PodFailed:
		return orchestratorpb.SessionState_SESSION_STATE_STOPPING
	default:
		return orchestratorpb.SessionState_SESSION_STATE_UNKNOWN
	}
}
