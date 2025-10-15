# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files first (for better caching)
COPY go.mod go.sum ./

# Download dependencies (this layer will be cached if go.mod/go.sum don't change)
RUN go mod download

# Copy only source code (excluding unnecessary files)
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY docs/ ./docs/

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o main cmd/api/main.go

# Final stage - use distroless for smaller image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder
COPY --from=builder /app/main .
COPY --from=builder /app/docs ./docs
COPY --from=builder /app/internal/templates ./internal/templates

# Note: .env is provided at runtime via docker-compose env_file or environment variables

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["./main"]
