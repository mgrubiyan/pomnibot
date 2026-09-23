# Stage 1: Build frontend with Bun
FROM oven/bun:1.4-alpine AS frontend-builder

WORKDIR /app/miniapp

# Copy package files for dependency caching
COPY miniapp/package.json miniapp/bun.lock ./

# Install dependencies
RUN bun install --frozen-lockfile

# Copy frontend source files
COPY miniapp/ ./

# Build production frontend bundle
RUN bun run build

# Stage 2: Build backend with Go
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app/backend

# Copy Go module files
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

# Copy built frontend assets into the Go embed directory
COPY --from=frontend-builder /app/miniapp/dist ./cmd/server/dist

# Build statically linked Go server binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# Stage 3: Minimal and secure runtime image
FROM alpine:3.21 AS runner

RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

# Install Russian Trusted Root CA into system certificate store
COPY --from=backend-builder /app/backend/internal/bot/certs/rootca.pem /usr/local/share/ca-certificates/russian_root_ca.crt
RUN update-ca-certificates

WORKDIR /app

# Copy the compiled binary
COPY --from=backend-builder /app/server /app/server

USER appuser

EXPOSE 8080

ENV PORT=8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/server"]
