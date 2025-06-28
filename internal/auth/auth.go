package auth

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// User represents an authenticated user
type User struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
	TenantID string   `json:"tenant_id"`
}

// Provider defines the interface for authentication providers
type Provider interface {
	// ValidateToken validates an authentication token and returns user info
	ValidateToken(ctx context.Context, token string) (*User, error)

	// RefreshToken refreshes an authentication token
	RefreshToken(ctx context.Context, refreshToken string) (string, error)

	// GetUserByID retrieves user information by ID
	GetUserByID(ctx context.Context, userID string) (*User, error)
}

// Permission represents a permission check
type Permission struct {
	Resource string
	Action   string
	TenantID string
}

// AuthorizationProvider defines the interface for authorization
type AuthorizationProvider interface {
	// CheckPermission checks if a user has permission for an action
	CheckPermission(ctx context.Context, user *User, permission Permission) (bool, error)

	// GetUserPermissions returns all permissions for a user
	GetUserPermissions(ctx context.Context, user *User) ([]Permission, error)
}

// Interceptor provides gRPC authentication and authorization
type Interceptor struct {
	authProvider  Provider
	authzProvider AuthorizationProvider
	publicMethods map[string]bool
}

// NewAuthInterceptor creates a new authentication interceptor
func NewAuthInterceptor(authProvider Provider, authzProvider AuthorizationProvider) *Interceptor {
	publicMethods := map[string]bool{
		"/opencode.orchestrator.v1.OrchestratorService/Health": true,
	}

	return &Interceptor{
		authProvider:  authProvider,
		authzProvider: authzProvider,
		publicMethods: publicMethods,
	}
}

// UnaryInterceptor provides authentication for unary gRPC calls
func (ai *Interceptor) UnaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	// Skip authentication for public methods
	if ai.publicMethods[info.FullMethod] {
		return handler(ctx, req)
	}

	// Extract and validate token
	user, err := ai.authenticateRequest(ctx)
	if err != nil {
		return nil, err
	}

	// Add user to context
	ctx = context.WithValue(ctx, "user", user)

	// Check authorization
	if err := ai.authorizeRequest(ctx, user, info.FullMethod); err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

// StreamInterceptor provides authentication for streaming gRPC calls
func (ai *Interceptor) StreamInterceptor(
	srv interface{},
	stream grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	// Skip authentication for public methods
	if ai.publicMethods[info.FullMethod] {
		return handler(srv, stream)
	}

	// Extract and validate token
	user, err := ai.authenticateRequest(stream.Context())
	if err != nil {
		return err
	}

	// Check authorization
	if err := ai.authorizeRequest(stream.Context(), user, info.FullMethod); err != nil {
		return err
	}

	// Create new stream with user context
	wrappedStream := &authenticatedStream{
		ServerStream: stream,
		ctx:          context.WithValue(stream.Context(), "user", user),
	}

	return handler(srv, wrappedStream)
}

// authenticateRequest extracts and validates authentication token
func (ai *Interceptor) authenticateRequest(ctx context.Context) (*User, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	// Extract token from Authorization header
	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	authHeader := authHeaders[0]
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, status.Error(codes.Unauthenticated, "invalid authorization header format")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		return nil, status.Error(codes.Unauthenticated, "missing token")
	}

	// Validate token
	user, err := ai.authProvider.ValidateToken(ctx, token)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	return user, nil
}

// authorizeRequest checks if user is authorized for the requested method
func (ai *Interceptor) authorizeRequest(ctx context.Context, user *User, method string) error {
	// Map gRPC methods to permissions
	permission := ai.methodToPermission(method)
	if permission == nil {
		// No specific permission required
		return nil
	}

	// Set tenant context
	permission.TenantID = user.TenantID

	// Check permission
	authorized, err := ai.authzProvider.CheckPermission(ctx, user, *permission)
	if err != nil {
		return status.Errorf(codes.Internal, "authorization check failed: %v", err)
	}

	if !authorized {
		return status.Errorf(codes.PermissionDenied, "insufficient permissions for %s", method)
	}

	return nil
}

// methodToPermission maps gRPC methods to permissions
func (ai *Interceptor) methodToPermission(method string) *Permission {
	permissionMap := map[string]Permission{
		"/opencode.orchestrator.v1.OrchestratorService/CreateSession": {
			Resource: "session",
			Action:   "create",
		},
		"/opencode.orchestrator.v1.OrchestratorService/GetSession": {
			Resource: "session",
			Action:   "read",
		},
		"/opencode.orchestrator.v1.OrchestratorService/ListSessions": {
			Resource: "session",
			Action:   "list",
		},
		"/opencode.orchestrator.v1.OrchestratorService/DeleteSession": {
			Resource: "session",
			Action:   "delete",
		},
		"/opencode.orchestrator.v1.OrchestratorService/ProxyHTTP": {
			Resource: "session",
			Action:   "proxy",
		},
	}

	if perm, exists := permissionMap[method]; exists {
		return &perm
	}

	return nil
}

// GetUserFromContext extracts user from context
func GetUserFromContext(ctx context.Context) (*User, error) {
	user, ok := ctx.Value("user").(*User)
	if !ok {
		return nil, errors.New("user not found in context")
	}
	return user, nil
}

// authenticatedStream wraps a gRPC stream with authentication context
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedStream) Context() context.Context {
	return s.ctx
}
