# syntax=docker/dockerfile:1.4
# ===========================================================================
# PulseOps — 三阶段构建 (Frontend + Backend → Runtime)
# ===========================================================================
# 构建指令:
#   DOCKER_BUILDKIT=1 docker build -t pulseops:latest .
# ===========================================================================

# ---------------------------------------------------------------------------
# 阶段 1: 前端构建 (Node)
# ---------------------------------------------------------------------------
FROM node:alpine AS frontend

WORKDIR /app

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ .
RUN npm run build

# ---------------------------------------------------------------------------
# 阶段 2: 后端构建 (Go)
# ---------------------------------------------------------------------------
FROM golang:alpine AS backend

ENV TZ="Asia/Shanghai"
ENV GOWORK=off

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o pulseops ./cmd/pulseops

# ---------------------------------------------------------------------------
# 阶段 3: 运行时
# ---------------------------------------------------------------------------
FROM alpine:latest

ENV TZ="Asia/Shanghai"

RUN apk add --no-cache tzdata ca-certificates coreutils \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

WORKDIR /app

COPY --from=backend /app/pulseops .
COPY --from=backend /app/configs ./configs
COPY --from=frontend /app/dist ./static

EXPOSE 8080

ENTRYPOINT ["./pulseops"]
CMD ["--config", "configs/pulseops.toml", "--static-dir", "static"]
