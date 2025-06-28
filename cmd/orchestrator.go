package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"github.com/opencode-ai/opencode/internal/orchestrator"
	"github.com/opencode-ai/opencode/internal/orchestrator/models"
	orchestratorpb "github.com/opencode-ai/opencode/internal/proto/orchestrator/v1"
)

var (
	grpcPort     int
	httpPort     int
	namespace    string
	kubeconfig   string
	runtimeImage string
	cpuReq       string
	memoryReq    string
	memoryLimit  string
	storageSize  string
)

func init() {
	orchestratorCmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "OpenCode Kubernetes Orchestrator",
		Long:  "Manages OpenCode sessions as Kubernetes pods with persistent storage",
		RunE:  runOrchestrator,
	}

	orchestratorCmd.Flags().IntVar(&grpcPort, "grpc-port", 9090, "gRPC server port")
	orchestratorCmd.Flags().IntVar(&httpPort, "http-port", 9091, "HTTP gateway port")
	orchestratorCmd.Flags().StringVar(&namespace, "namespace", "opencode-sessions", "Kubernetes namespace for sessions")
	orchestratorCmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (empty for in-cluster)")
	orchestratorCmd.Flags().StringVar(&runtimeImage, "runtime-image", "ghcr.io/denysvitali/opencode:latest", "Containr image for OpenCode runtime environment")
	orchestratorCmd.Flags().StringVar(&cpuReq, "cpu-request", "50m", "CPU request for session pods")
	orchestratorCmd.Flags().StringVar(&memoryReq, "memory-request", "128Mi", "Memory request for session pods")
	orchestratorCmd.Flags().StringVar(&memoryLimit, "memory-limit", "256Mi", "Memory limit for session pods")
	orchestratorCmd.Flags().StringVar(&storageSize, "storage-size", "10Gi", "Persistent storage size for session pods")

	rootCmd.AddCommand(orchestratorCmd)
}

func runOrchestrator(*cobra.Command, []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	// OTEL autoexport setup (console or collector, depending on env)
	exp, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to set up OTEL exporter: %w", err)
	}

	provider := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(resource.Default()),
	)
	otel.SetTracerProvider(provider)
	defer provider.Shutdown(ctx)
	defer cancel()
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())

	// Create Kubernetes runtime configuration
	kubeConfig := &models.KubernetesConfig{
		Namespace:  namespace,
		Kubeconfig: kubeconfig,
		Image:      runtimeImage,
		Resources: models.ResourceRequirements{
			Requests: models.ResourceList{
				CPU:    cpuReq,
				Memory: memoryReq,
			},
			Limits: models.ResourceList{
				// We don't pass CPU limits on purpose - see https://home.robusta.dev/blog/stop-using-cpu-limits
				Memory: memoryLimit,
			},
		},
		StorageSize: storageSize,
	}

	orchestratorSvc, err := orchestrator.NewService(ctx, &models.Config{
		RuntimeConfig: kubeConfig,
		SessionTTL:    24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("failed to create orchestrator service: %w", err)
	}

	// Start gRPC server
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	orchestratorpb.RegisterOrchestratorServiceServer(grpcServer, orchestratorSvc)
	reflection.Register(grpcServer)

	grpcAddr := fmt.Sprintf(":%d", grpcPort)
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", grpcAddr, err)
	}

	// Start HTTP gateway with CORS support
	gwMux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			// Allow OpenTelemetry trace headers to pass through
			switch key {
			case "traceparent", "tracestate", "x-trace-id", "x-span-id":
				return key, true
			default:
				return runtime.DefaultHeaderMatcher(key)
			}
		}),
	)
	// Custom gRPC-Gateway otel middleware with client instrumentation
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
	if err := orchestratorpb.RegisterOrchestratorServiceHandlerFromEndpoint(ctx, gwMux, grpcAddr, opts); err != nil {
		return fmt.Errorf("failed to register gateway: %w", err)
	}

	// Create CORS-enabled handler
	corsHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set CORS headers to allow access from any origin
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With")
			w.Header().Set("Access-Control-Expose-Headers", "Link")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "300")

			// Handle preflight OPTIONS request
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Continue with the actual request
			h.ServeHTTP(w, r)
		})
	}

	httpServer := &http.Server{
		Addr: fmt.Sprintf(":%d", httpPort),
		Handler: otelhttp.NewHandler(corsHandler(gwMux), "grpc-gateway",
			otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
				return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			}),
			otelhttp.WithFilter(func(r *http.Request) bool {
				return r.URL.Path != "/health"
			}),
		),
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start servers
	go func() {
		log.Printf("Starting gRPC server on %s", grpcAddr)
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	go func() {
		log.Printf("Starting HTTP gateway on :%d", httpPort)
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	grpcServer.GracefulStop()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Orchestrator stopped")
	return nil
}
