# syntax=docker/dockerfile:1

# --- Stage 1: build the dashboard SPA ---
FROM node:22-alpine AS web
WORKDIR /web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install
COPY web/ ./
RUN pnpm build

# --- Stage 2: build the Go binary with embedded assets ---
FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Embed the freshly built dashboard.
COPY --from=web /web/dist ./internal/server/webui/dist
ARG VERSION=docker
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X github.com/AymanYouss/relay/internal/app.Version=${VERSION}" \
    -o /out/relay ./cmd/relay

# --- Stage 3: minimal runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/relay /app/relay
COPY relay.example.yaml /app/relay.yaml
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/app/relay"]
CMD ["-config", "/app/relay.yaml"]
