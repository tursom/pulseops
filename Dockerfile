# syntax=docker/dockerfile:1.4
# ===========================================================================
# PulseOps — 多阶段构建
# ===========================================================================
# 构建指令:
#   DOCKER_BUILDKIT=1 docker build -t pulseops:latest .
# ===========================================================================

# ---------------------------------------------------------------------------
# 阶段 1: 构建
# ---------------------------------------------------------------------------
FROM golang:alpine AS builder

ENV TZ="Asia/Shanghai"
ENV GOWORK=off

WORKDIR /app

RUN apk add --no-cache git ca-certificates

# 利用缓存: 先复制依赖文件
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 复制源码并编译
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o pulseops ./cmd/pulseops

# ---------------------------------------------------------------------------
# 阶段 2: 运行
# ---------------------------------------------------------------------------
FROM alpine:latest

ENV TZ="Asia/Shanghai"

RUN apk add --no-cache tzdata ca-certificates coreutils \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

WORKDIR /app

COPY --from=builder /app/pulseops .

# 复制配置文件目录 (运行时需要热重载任务配置)
COPY --from=builder /app/configs ./configs

EXPOSE 8080

ENTRYPOINT ["./pulseops"]
CMD ["--config", "configs/pulseops.toml"]
