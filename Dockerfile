# Multi-stage build for Golang Gin application
FROM golang:alpine AS builder

WORKDIR /app

# Install ca-certificates and git
RUN apk add --no-cache ca-certificates git

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server .

# Final stage: minimal runtime
FROM alpine:3.21

WORKDIR /app

# Install ca-certificates and tzdata for HTTPS and correct timestamps
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/server .

# Default configuration environment variables
ENV OPENAI_ENDPOINT=https://generativelanguage.googleapis.com/v1beta/openai \
    OPENAI_MODEL=gemini-3.5-flash-lite \
    RATE_LIMIT_GLOBAL=10000 \
    RATE_LIMIT_AUTH=10000 \
    RATE_LIMIT_AI=1000

# Expose default application port
EXPOSE 8080

# Run the application
CMD ["./server"]
