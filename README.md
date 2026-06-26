# PulseOps — 配置驱动的遥测与可观测性平台

## 简介

PulseOps 是一个配置驱动的服务遥测与可观测性平台。通过 TOML 配置文件声明式定义健康检查、业务巡检、数据校验任务，自动采集执行留痕，支持热重载配置。V2 新增 AI 分析能力，可将遥测数据喂给大模型进行智能诊断和趋势预测。

## 功能特性

- 🔧 **配置驱动**: 所有任务通过 TOML 文件定义，无需编写代码
- 🏗 **5 种任务类型**: HTTP 检查、TCP 端口检查、脚本执行、进程检查、多步骤业务巡检
- 🤖 **AI 分析** (NEW): 基于 DeepSeek/OpenAI 的实时诊断、趋势分析、业务数据校验
- 📦 **大对象存储**: 自动将超大 payload 外存至 MinIO/S3
- 🔄 **热重载**: 修改任务配置文件后自动加载，无需重启
- 📊 **Prometheus 指标**: 内置任务执行次数和耗时指标
- 🧩 **插件化驱动**: 实现 Driver 接口即可扩展新任务类型
- 🏥 **失败检测**: 支持连续失败告警和恢复通知

## 快速开始

### 环境依赖

- Go 1.24+
- PostgreSQL 14+
- MinIO（或 S3 兼容存储）

### 安装

```bash
cd pulseops
go build ./cmd/pulseops
```

### 配置

```bash
cp configs/pulseops.example.toml configs/pulseops.toml
# 编辑 configs/pulseops.toml，修改数据库和 MinIO 连接信息
```

### 运行

```bash
./pulseops --config configs/pulseops.toml
```

### Docker 部署

```bash
# 1. 准备配置
cp configs/pulseops.example.toml configs/pulseops.toml
# 编辑 configs/pulseops.toml，将 state.dsn 和 artifact_store.endpoint
# 指向你的 PostgreSQL 和 MinIO/S3 地址

# 2. 构建并启动
docker compose up -d

# 3. 验证
curl http://localhost:8088/healthz
```

> **注意**：docker-compose.yml 仅包含 pulseops 服务本身，PostgreSQL 和 MinIO 需要外部提供。
> 配置文件 `configs/pulseops.toml` 已被 .gitignore 忽略，不会提交到仓库。

### 创建第一个任务

```bash
cp configs/tasks/example-http.toml configs/tasks/my-check.toml
# 编辑目标 URL，保存后自动加载
```

### 启用 AI 分析

在 `configs/pulseops.toml` 中添加：

```toml
[ai]
enabled = true
endpoint = "http://127.0.0.1:8000/v1"
model = "deepseek-chat"
```

然后创建 AI 分析任务：

```bash
cp configs/tasks/diagnose-example.toml configs/tasks/my-diagnose.toml
```

## 目录结构

```
pulseops/
├── cmd/pulseops/          # 入口
├── internal/
│   ├── ai/                # AI 分析引擎
│   ├── api/               # HTTP API
│   ├── app/               # 应用装配
│   ├── config/            # 配置模型和加载
│   ├── evaluator/         # 业务规则评估器
│   ├── runtime/           # 任务调度和生命周期
│   ├── scenario/          # 多步骤巡检引擎
│   ├── store/             # 持久化 (PostgreSQL + MinIO)
│   ├── task/              # 任务驱动注册和接口
│   ├── trace/             # 执行留痕处理
│   └── watch/             # 配置文件热重载
├── configs/
│   ├── tasks/             # 任务配置文件目录
│   ├── pulseops.toml      # 主配置（gitignore）
│   └── pulseops.example.toml
├── Dockerfile             # 多阶段构建
├── docker-compose.yml     # Docker Compose 启动
├── .dockerignore
└── doc/                   # 文档
```

## 任务类型一览

| Kind | 用途 | 示例 |
|------|------|------|
| `http_check` | HTTP 健康检查 | 检查 API 端点的状态码和响应体 |
| `tcp_check` | TCP 端口检查 | 检查数据库端口可达 |
| `script_exec` | 脚本执行 | 运行自定义检查脚本 |
| `process_check` | 进程检查 | 检查进程是否运行 |
| `scenario_check` | 多步骤业务巡检 | 采样数据 → 调用详情接口 → 比较差异 |
| `ai_analyze` | AI 分析 | 对遥测数据进行智能诊断和趋势预测 |

## 文档

- [AI 集成设计和使用指南](doc/ai-integration.md)
- [任务配置参考](doc/task-config-reference.md)
- [平台设计文档](doc/platform-design.md)
- [前端优化功能需求对齐稿](doc/frontend-redesign-requirements.md)

## 开发

```bash
# 本地构建
GOWORK=off go build ./cmd/pulseops

# 测试
GOWORK=off go test ./...

# 代码检查
GOWORK=off go vet ./...

# Docker 构建
DOCKER_BUILDKIT=1 docker build -t pulseops:latest .
```
