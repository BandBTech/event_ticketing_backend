# -------- Build stage (FAST: Debian/glibc) --------
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Install swag for Swagger documentation generation
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Copy source code
COPY . .

# Generate Swagger documentation
RUN swag init -g cmd/api/main.go --output docs/

# Build binary
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -o main -buildvcs=false ./cmd/api


# -------- Run stage (small) --------
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary
COPY --from=builder /app/main .

# Copy templates
COPY --from=builder /app/internal/templates ./internal/templates

# Copy Swagger documentation
COPY --from=builder /app/docs ./docs

EXPOSE 8082

ENTRYPOINT ["./main"]
