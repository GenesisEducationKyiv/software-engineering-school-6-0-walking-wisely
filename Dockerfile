# ==========================================
# 1. PROTOBUF: Generate code from .proto files
# ==========================================
FROM bufbuild/buf:1.67 AS proto
WORKDIR /app
COPY buf.yaml buf.gen.yaml ./
COPY proto/ proto/
RUN buf dep update && buf generate

# ==========================================
# 2. BUILDER: Compile the production binary
# ==========================================
FROM golang:1.25-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=proto /app/gen/ ./gen/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /main ./cmd/server

# ==========================================
# 3. PROD: Minimal runtime image
# ==========================================
FROM alpine:3.20 AS prod
RUN apk add --no-cache ca-certificates
WORKDIR /

COPY --from=builder /main /main

EXPOSE 8080
CMD ["/main"]