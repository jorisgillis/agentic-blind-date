FROM golang:1.26.2-alpine AS builder

WORKDIR /app

# Install SQLite3 dependencies (required for CGO)
RUN apk add --no-cache sqlite sqlite-dev gcc musl-dev

# Copy go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o /app/agentic-blind-date -ldflags "-w -s"

# Final stage
FROM alpine:3.19

WORKDIR /app

# Install SQLite3 runtime
RUN apk add --no-cache sqlite ca-certificates tzdata

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy binary from builder
COPY --from=builder /app/agentic-blind-date /app/agentic-blind-date
COPY --from=builder /app/templates /app/templates

# Create data directory for SQLite database
RUN mkdir -p /data && chown appuser:appgroup /data

# Switch to non-root user
USER appuser

# Database will be stored in /data/blind_date.db
# This volume will be mounted from the host
VOLUME /data

EXPOSE 8080

ENTRYPOINT ["/app/agentic-blind-date"]
