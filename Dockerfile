# ==========================================
# 1. PROTOBUF: Generate code from .proto files
# ==========================================
FROM bufbuild/buf:1.67 AS proto
WORKDIR /app
COPY buf.yaml buf.gen.yaml ./
COPY proto/ proto/
RUN buf dep update && buf generate

# ==========================================
# 2. BUILDER: Compile all production binaries
# ==========================================
FROM golang:1.25-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=proto /app/gen/ ./gen/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api          ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /notifications ./cmd/notifications

# ==========================================
# 3. API: Minimal runtime image for the API service
# ==========================================
FROM alpine:3.20 AS api
RUN apk add --no-cache ca-certificates
WORKDIR /

COPY --from=builder /api /api

EXPOSE 8080
CMD ["/api"]

# ==========================================
# 4. NOTIFICATIONS: Minimal runtime image for the notifications service
# ==========================================
FROM alpine:3.20 AS notifications
RUN apk add --no-cache ca-certificates
WORKDIR /

COPY --from=builder /notifications /notifications

EXPOSE 8081
CMD ["/notifications"]
