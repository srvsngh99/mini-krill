# Stage 1: Build the binary
FROM golang:1.22-alpine AS builder

LABEL maintainer="Sourav Singh <sourav@souravailabs.com>"

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum* ./
RUN go mod download

# Copy source and VERSION
COPY . .

# Build statically-linked binary with version baked in
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/srvsngh99/mini-krill/internal/core.Version=$(cat VERSION)" \
    -o /minikrill ./cmd/minikrill

# Stage 2: Minimal runtime image
FROM alpine:latest

LABEL maintainer="Sourav Singh <sourav@souravailabs.com>"
LABEL description="Mini Krill - lightweight AI agent CLI"
LABEL version="0.1.0"

RUN apk add --no-cache ca-certificates

COPY --from=builder /minikrill /usr/local/bin/minikrill

ENTRYPOINT ["minikrill"]
