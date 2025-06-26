package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/opencode-ai/opencode/internal/app"
	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/db"
	"github.com/opencode-ai/opencode/internal/logging"
	pb "github.com/opencode-ai/opencode/internal/proto/v1"
	"github.com/opencode-ai/opencode/internal/server"
)

func main() {
	ctx := context.Background()
	
	// Load configuration
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}
	
	debug := os.Getenv("OPENCODE_DEBUG") == "true"
	_, err = config.Load(cwd, debug)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to database
	conn, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer conn.Close()

	// Create application
	app, err := app.New(ctx, conn)
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}
	defer app.Shutdown()

	// Get ports from environment or use defaults
	grpcPort := getEnvWithDefault("OPENCODE_API_PORT", "50051")
	httpPort := getEnvWithDefault("OPENCODE_HTTP_PORT", "8080")

	// Start gRPC server
	go func() {
		if err := startGRPCServer(app, grpcPort); err != nil {
			log.Fatalf("Failed to start gRPC server: %v", err)
		}
	}()

	// Start HTTP gateway
	go func() {
		if err := startHTTPGateway(ctx, grpcPort, httpPort); err != nil {
			log.Fatalf("Failed to start HTTP gateway: %v", err)
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	logging.Info("Shutting down server...")
}

func startGRPCServer(app *app.App, port string) error {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %v", port, err)
	}

	s := grpc.NewServer()
	
	// Register OpenCode service
	srv := server.NewOpenCodeServer(app)
	pb.RegisterOpenCodeServiceServer(s, srv)

	logging.Info("Starting gRPC server", "port", port)
	return s.Serve(lis)
}

func startHTTPGateway(ctx context.Context, grpcPort, httpPort string) error {
	conn, err := grpc.NewClient(
		"localhost:"+grpcPort,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("failed to dial gRPC server: %v", err)
	}
	defer conn.Close()

	mux := runtime.NewServeMux()
	
	// Register gateway handlers
	err = pb.RegisterOpenCodeServiceHandler(ctx, mux, conn)
	if err != nil {
		return fmt.Errorf("failed to register gateway: %v", err)
	}

	logging.Info("Starting HTTP gateway", "port", httpPort)
	return http.ListenAndServe(":"+httpPort, mux)
}

func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
