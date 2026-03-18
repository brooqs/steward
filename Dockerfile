# ── Build ─────────────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /steward ./cmd/steward

# ── Runtime ───────────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /steward /usr/local/bin/steward

RUN mkdir -p /app/data /app/config/integrations
WORKDIR /app

ENTRYPOINT ["steward"]
CMD ["--config", "/app/config/core.yml", "--channel", "telegram"]
