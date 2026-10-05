# syntax=docker/dockerfile:1.6

# ---------- build stage ----------
FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/node \
    ./cmd/node

# ---------- runtime stage ----------
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata bash netcat-openbsd

COPY --from=build /out/node /usr/local/bin/node

ENV NODE_STATE_DIR=/state \
    LISTEN_HOST=0.0.0.0 \
    LISTEN_PORT=9001 \
    LOG_LEVEL=INFO

RUN mkdir -p /state /logs /metrics

WORKDIR /
ENTRYPOINT ["/usr/local/bin/node"]
