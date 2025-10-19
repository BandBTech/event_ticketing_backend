# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files first (for better caching)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy only necessary files for building
# First copy all go files and project structure
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY docs/ ./docs/

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o main ./cmd/api

# Run stage - using a minimal scratch image
FROM alpine:latest

# Install ca-certificates for HTTPS and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Create a non-root user to run the application
RUN adduser -D -H -h /app appuser

WORKDIR /app

# Copy only the compiled binary from builder
COPY --from=builder /app/main .

# Copy only necessary template files
COPY --from=builder /app/internal/templates ./internal/templates

# Use non-root user
USER appuser

# Expose the application port
EXPOSE 8082

# Set environment variables
ENV GIN_MODE=release

# Run the application
CMD ["./main"]
