package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/opencode-ai/opencode/internal/app"
	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/db"
	"github.com/opencode-ai/opencode/internal/logging"
	pb "github.com/opencode-ai/opencode/internal/proto/v1"
	"github.com/opencode-ai/opencode/internal/server"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the OpenCode gRPC and HTTP API server",
	Long: `Start the OpenCode API server in single-session-per-container mode.
This provides both gRPC and HTTP REST endpoints for external control.`,
	Example: `
  # Start server with default ports (gRPC: 8080, HTTP: 8081)
  opencode server

  # Start server with custom ports
  opencode server --grpc-port 9090 --http-port 9091

  # Start server with debug logging
  opencode server -d
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get flags
		debug, _ := cmd.Flags().GetBool("debug")
		cwd, _ := cmd.Flags().GetString("cwd")
		grpcPort, _ := cmd.Flags().GetString("grpc-port")
		httpPort, _ := cmd.Flags().GetString("http-port")

		// Set working directory
		if cwd != "" {
			err := os.Chdir(cwd)
			if err != nil {
				return fmt.Errorf("failed to change directory: %v", err)
			}
		}
		if cwd == "" {
			c, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %v", err)
			}
			cwd = c
		}

		// Load configuration
		_, err := config.Load(cwd, debug)
		if err != nil {
			return fmt.Errorf("failed to load config: %v", err)
		}

		// Connect to database
		conn, err := db.Connect()
		if err != nil {
			return fmt.Errorf("failed to connect to database: %v", err)
		}
		defer conn.Close()

		// Create application
		ctx := context.Background()
		app, err := app.New(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to create app: %v", err)
		}
		defer app.Shutdown()

		// Start servers
		return startServers(ctx, app, grpcPort, httpPort)
	},
}

func startServers(ctx context.Context, app *app.App, grpcPort, httpPort string) error {
	// Create channels for server errors
	grpcErrCh := make(chan error, 1)
	httpErrCh := make(chan error, 1)

	// Start gRPC server
	go func() {
		grpcErrCh <- startGRPCServer(app, grpcPort)
	}()

	// Start HTTP gateway server
	go func() {
		httpErrCh <- startHTTPGateway(ctx, grpcPort, httpPort)
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either error or signal
	select {
	case err := <-grpcErrCh:
		return fmt.Errorf("gRPC server error: %v", err)
	case err := <-httpErrCh:
		return fmt.Errorf("HTTP gateway error: %v", err)
	case sig := <-sigCh:
		logging.Info("Received signal, shutting down", "signal", sig)
		return nil
	}
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

func init() {
	rootCmd.AddCommand(serverCmd)

	// Add server-specific flags
	serverCmd.Flags().String("grpc-port", "8080", "Port for gRPC server")
	serverCmd.Flags().String("http-port", "8081", "Port for HTTP gateway server")
	serverCmd.Flags().BoolP("debug", "d", false, "Enable debug logging")
	serverCmd.Flags().StringP("cwd", "c", "", "Working directory")
}
