# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the main OpenCode binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o opencode .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/opencode .

# Expose ports
EXPOSE 8080 8081

# Environment variables with defaults
ENV GRPC_PORT=8080
ENV HTTP_PORT=8081
ENV OPENCODE_DEBUG=false

# Run the server subcommand
CMD ["./opencode", "server"]
