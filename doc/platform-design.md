# PulseOps 平台设计方案

## 1. 项目定位

`pulseops` 定位为一个基于配置驱动的多功能运维平台。

平台目标不是一开始就做成一个大而全的监控系统，而是先提供一套稳定的任务运行底座，让平台可以：

- 通过独立配置文件定义和管理任务
- 动态加载、更新、卸载任务
- 统一执行任务调度、状态管理和执行留痕
- 通过驱动机制扩展不同类型的运维任务

V1 优先解决以下核心问题：

- 任务配置独立存放，便于增删改查
- 平台运行时可以自动发现配置变更并热更新
- 每个任务都具备可配置的执行留痕与存储方向
- 平台提供统一状态视图和基础运维接口

## 2. 设计原则

- 配置驱动：任务行为由配置声明，平台负责解析、校验、执行和治理
- 驱动扩展：不同任务类型通过统一驱动接口接入，而不是在主流程中散落分支逻辑
- 业务巡检一等能力：对“拉列表 -> 抽样 -> fan-out 查询 -> 规则评估 -> 聚合结果”类任务提供内建模型，避免业务逻辑散落到脚本
- 运行稳定：配置更新失败时不影响旧任务继续运行
- 留痕优先：执行记录、结果摘要、错误信息和关键输出统一沉淀
- 渐进增强：V1 先支持核心调度、留痕和状态查询，后续再增加告警、工作流和前端

## 3. 范围说明

### 3.1 V1 范围

- 平台进程启动与全局配置加载
- 基于目录扫描和文件监听的任务动态管理
- 任务驱动注册与基础任务运行框架
- 受控多步业务巡检驱动 `scenario_check`
- 执行留痕的统一抽象和多 sink 存储
- HTTP 状态接口和 Prometheus 指标导出

### 3.2 非目标

- 不在 V1 中实现动态加载 Go 插件 `.so`
- 不在 V1 中实现复杂可视化前端
- 不在 V1 中实现完整的告警编排平台
- 不在 V1 中实现任意 DAG 工作流编排或用户自定义节点脚本链

V1 的“动态”仅针对任务配置，不针对代码模块。新增能力通过平台内置驱动注册表扩展。

V1 在基础探测任务之外，额外支持一类受控的业务巡检任务 `scenario_check`。这类任务适合“先取列表，再抽样，再按样本并发调详情，最后根据业务 evaluator 做一致性判断”的场景，但仍然不扩展为通用工作流引擎。

## 4. 目录结构建议

建议项目结构如下：

```text
pulseops/
  cmd/pulseops/main.go
  configs/pulseops.toml
  configs/tasks/
    prod-api-health.toml
    prod-redis-ping.toml
    prod-disk-check.toml
  data/
  doc/
    platform-design.md
  internal/
    api/
    app/
    config/
    evaluator/
    runtime/
    scenario/
    store/
    task/
    trace/
    watch/
```

目录职责建议如下：

- `cmd/pulseops`：程序入口
- `configs/pulseops.toml`：平台级配置
- `configs/tasks/`：任务配置目录，一文件一任务
- `internal/config`：配置模型、解析和校验
- `internal/evaluator`：业务规则评估器注册与实现
- `internal/task`：任务驱动接口和任务实例定义
- `internal/runtime`：任务管理、调度和生命周期控制
- `internal/scenario`：多步巡检执行器、抽样器、fan-out 编排
- `internal/watch`：目录监听和变更事件收敛
- `internal/trace`：执行留痕模型与 sink 管理
- `internal/store`：状态、结果、历史记录持久化
- `internal/api`：健康检查、任务状态、管理接口

## 5. 配置模型设计

平台配置分为两层：

- 全局配置：定义平台运行参数、可用存储目标、接口监听、默认策略
- 任务配置：定义具体任务的类型、参数、调度方式和留痕策略

### 5.1 全局配置

建议使用 TOML，示例：

```toml
[server]
addr = ":8080"
read_timeout = "5s"
write_timeout = "10s"

[task]
config_dir = "configs/tasks"
reload_debounce = "500ms"
default_timeout = "10s"
default_trace_level = "summary"

[state]
backend = "postgres"
dsn = "postgres://pulseops:secret@127.0.0.1:5432/pulseops?sslmode=disable"

[artifact_store]
kind = "s3"
provider = "r2"
bucket = "pulseops-artifacts"
endpoint = "https://<account-id>.r2.cloudflarestorage.com"
region = "auto"
key_prefix = "prod/"
force_path_style = false
presign_ttl = "15m"

[trace.sinks.postgres_main]
kind = "postgres"
dsn = "postgres://pulseops:secret@127.0.0.1:5432/pulseops?sslmode=disable"

[trace.sinks.webhook_audit]
kind = "webhook"
url = "http://audit-service/internal/task-trace"
timeout = "3s"
```

全局配置主要负责：

- 指定任务配置目录
- 定义平台默认超时、默认留痕级别
- 定义状态与历史记录的 PostgreSQL 存储
- 定义大体积 artifact 的对象存储后端
- 定义可被任务引用的 trace sink

### 5.2 任务配置

每个任务一个配置文件，示例：

```toml
id = "prod-api-health"
name = "生产 API 健康检查"
kind = "http_check"
enabled = true
interval = "30s"
timeout = "3s"

[labels]
env = "prod"
service = "gateway"

[params]
url = "https://api.example.com/healthz"
method = "GET"
expect_status = [200]

[trace]
enabled = true
level = "summary"
sinks = ["postgres_main"]
retain_days = 30
store_stdout = false
store_stderr = false
store_result_payload = true
max_payload_bytes = 4096
mask_fields = ["authorization", "token", "password"]

[alert]
consecutive_failures = 3
channels = ["feishu"]
recover_notify = true
```

对于多步业务巡检任务，建议单独提供 `scenario_check` 配置形态，例如：

```toml
id = "buyer-goods-price-audit"
name = "买家游戏外围价格一致性巡检"
kind = "scenario_check"
enabled = true
interval = "10m"
timeout = "30s"

[labels]
env = "prod"
service = "game-trade"
domain = "pricing"

[params.source]
kind = "http_json"
method = "GET"
url = "http://gateway/api/game-trade-server/goods/v1/GetBuyerGoodsList"
items_path = "$.data.goods"

[params.sample]
strategy = "random"
count = 5
seed_mode = "recorded"

[params.fanout]
kind = "http_json"
method = "GET"
url = "http://gateway/api/goods-serve/games/v1/GoodsDetail"
query_template = { goods_id = "{{ item.goods_id }}" }
concurrency = 5

[params.evaluator]
name = "steam_game_price_consistency"

[params.thresholds]
max_mismatch_count = 0
max_error_count = 0

[trace]
enabled = true
level = "detail"
sinks = ["postgres_main"]
store_result_payload = true
max_payload_bytes = 65536
```

### 5.3 任务配置字段建议

基础字段：

- `id`：任务唯一标识
- `name`：任务名称
- `kind`：任务驱动类型
- `enabled`：是否启用
- `interval` 或 `cron`：调度方式
- `timeout`：单次执行超时
- `labels`：标签，用于分类、过滤、告警分组

驱动字段：

- `params`：驱动自己的参数配置

留痕字段：

- `trace.enabled`：是否开启留痕
- `trace.level`：留痕级别
- `trace.sinks`：留痕存储目标
- `trace.retain_days`：保留天数
- `trace.store_stdout`：是否记录标准输出
- `trace.store_stderr`：是否记录标准错误
- `trace.store_result_payload`：是否记录结果负载
- `trace.max_payload_bytes`：单条结果体允许内联写入 PostgreSQL 的最大大小
- `trace.mask_fields`：敏感字段脱敏列表

告警字段：

- `alert.consecutive_failures`：连续失败阈值
- `alert.channels`：通知渠道
- `alert.recover_notify`：是否在恢复时通知

### 5.4 `scenario_check` 参数建议

`scenario_check` 建议采用固定阶段模型，而不是让用户自由拼接任意节点：

- `params.source`：定义入口数据源，例如 HTTP 列表接口、请求方法、认证信息和列表提取路径
- `params.sample`：定义抽样策略，例如 `random`、`first_n`、`fixed_ids`
- `params.sample.count`：单次巡检抽样数
- `params.sample.seed_mode`：随机种子策略，建议支持 `recorded`、`fixed`
- `params.fanout`：定义对每个样本的详情查询方式、并发度、超时和参数模板
- `params.evaluator`：指定业务规则评估器名称和参数
- `params.thresholds`：定义 mismatch、error、跳过样本等阈值如何映射为告警或失败

推荐执行链路固定为：

`source -> sample -> fanout -> evaluator -> aggregate`

这样既能覆盖“随机抽几个游戏，再逐个校验详情”的真实场景，又不会把平台推向难以治理的通用编排系统。

对于输出控制，建议补充以下约定：

- 小型结构化结果内联写入 PostgreSQL，便于查询和聚合
- 超过 `trace.max_payload_bytes` 的响应体、AI 分析全文、截图、压缩包等写入 `artifact_store`
- 任务只在元数据中保留 artifact 引用、摘要和校验信息

## 6. 任务类型与驱动机制

平台不直接把所有任务逻辑写死在管理器里，而是通过统一驱动注册。

建议 V1 任务类型包括：

- `http_check`：HTTP 健康检查
- `tcp_check`：TCP 端口连通性检查
- `scenario_check`：受控多步业务巡检，适合“拉列表 -> 抽样 -> 详情查询 -> 规则校验 -> 聚合结果”
- `script_exec`：执行本地脚本或命令
- `process_check`：检查指定进程是否存在

后续可扩展：

- `db_ping`
- `redis_ping`
- `workflow`
- `webhook_call`

### 6.1 驱动接口建议

```go
type Driver interface {
    Kind() string
    Validate(spec TaskSpec) error
    NewRunner(spec TaskSpec) (Runner, error)
}

type Runner interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Snapshot() RuntimeSnapshot
}
```

职责说明：

- `Driver` 负责描述一种任务类型的能力
- `Validate` 负责校验任务配置是否满足该驱动要求
- `NewRunner` 负责基于配置构造运行实例
- `Runner` 负责具体执行、停止和暴露运行时状态

### 6.2 `scenario_check` 的边界

`scenario_check` 是一类受控的多步任务，不等价于工作流引擎。建议限制为：

- 只支持线性阶段：`source -> sample -> fanout -> evaluator -> aggregate`
- 不支持配置内嵌脚本、任意循环、任意条件分支
- 业务判断通过平台注册的 evaluator 完成，而不是直接在 TOML 中写表达式树
- fan-out 仅用于对样本执行同构动作，例如逐个调详情接口

这样可以在保持平台可治理性的前提下，原生支持业务巡检需求。

### 6.3 Evaluator 抽象建议

建议在驱动之外增加业务评估器抽象：

```go
type ScenarioEvaluator interface {
    Name() string
    Evaluate(ctx context.Context, input EvaluationInput) (EvaluationResult, error)
}
```

职责建议：

- `ScenarioEvaluator` 负责解释业务口径，例如 DLC、`package_id`、展示价匹配规则
- `EvaluationInput` 负责承载列表样本、详情响应、任务参数和运行上下文
- `EvaluationResult` 负责输出聚合统计、差异明细和最终判定

对于“外围价格是否正确”这类需求，推荐把规则落在内建 evaluator 中，例如 `steam_game_price_consistency`，而不是退回 `script_exec`。

## 7. 动态加载、更新与卸载机制

### 7.1 启动阶段

平台启动后执行以下流程：

1. 加载全局配置
2. 扫描 `configs/tasks/` 下的任务文件
3. 逐个解析并校验配置
4. 为合法任务创建运行实例
5. 启动启用态任务
6. 启动目录监听器，进入运行态

### 7.2 运行期变更监听

使用 `fsnotify` 监听任务配置目录变更。

事件处理规则：

- 新增文件：解析并校验成功后，注册并启动任务
- 修改文件：构造新实例，成功后替换旧实例
- 删除文件：停止任务并从管理器移除

### 7.3 防抖与幂等

文件监听需要解决编辑器多次触发的问题，建议加入：

- `300ms` 到 `1s` 的 debounce
- 基于文件内容 hash 的幂等判断

只有当配置内容实际变化时才执行重载。

### 7.4 热更新安全策略

配置更新时建议使用“先构造、后切换”的安全流程：

1. 读取新配置
2. 校验新配置
3. 用新配置构造新任务实例
4. 新实例创建成功后，再停止旧实例并切换

如果新配置无效：

- 保留旧实例继续运行
- 记录重载失败状态
- 暴露错误信息到日志和状态接口

### 7.5 删除任务的处理

任务配置文件被删除时：

- 平台优雅停止该任务
- 从内存任务表移除
- 保留历史执行记录

默认不应删除历史留痕，以便审计和排障。

## 8. 执行留痕设计

执行留痕不是附属功能，而是平台的核心能力之一。任务配置除了定义“做什么”，还要定义“执行后留下些什么”和“写到哪里”。

### 8.1 设计目标

- 统一记录任务执行结果
- 支持聚合结果和样本级明细同时留痕
- 支持按任务声明存储方向
- 支持不同留痕级别
- 支持保留期和脱敏策略
- 留痕失败不阻塞主任务执行

### 8.2 两层配置模型

留痕配置分为两层：

- 平台级定义可用 sink
- 任务级声明留痕策略并引用 sink

这样可以避免每个任务重复写存储连接信息，也便于统一切换存储后端。

### 8.3 留痕级别建议

#### `none`

- 不留痕

#### `summary`

记录最小必要信息：

- 任务 ID
- 执行 ID
- 开始时间
- 结束时间
- 执行耗时
- 执行结果
- 错误摘要
- 触发类型

#### `detail`

在 `summary` 基础上增加：

- 关键输入参数快照
- 结构化结果摘要
- 样本抽样结果和差异摘要
- artifact 引用和摘要
- 重试信息
- 截断后的输出内容

#### `debug`

适合短期排障：

- 每个步骤的原始响应摘要
- 完整输出内容
- 完整结果体
- 更细粒度步骤记录

### 8.4 统一执行记录模型

建议平台内部统一抽象一条执行记录：

```go
type ExecutionRecord struct {
    RunID        string
    TaskID       string
    TaskKind     string
    TriggerType  string
    RunStatus    string
    CheckStatus  string
    StartedAt    time.Time
    EndedAt      time.Time
    DurationMS   int64
    ErrorMessage string
    Summary      map[string]any
    Payload      []byte
    ArtifactRefs []ArtifactRef
    Stdout       string
    Stderr       string
    Labels       map[string]string
}
```

平台的各类任务最终都应转换为统一的 `ExecutionRecord`，再交给留痕模块分发。

其中：

- `RunStatus` 表示平台执行是否成功，例如 `success`、`failed`、`timeout`
- `CheckStatus` 表示业务判定结果，例如 `pass`、`warn`、`fail`

对于 `scenario_check`，建议约定：

- `Summary` 至少包含 `sample_count`、`checked_count`、`mismatch_count`、`error_count`
- `Payload` 至少包含 `sample_seed`、`sampled_items`、`findings`
- `findings` 每项至少包含样本主键、展示值、期望值、判定原因和关键信息，如 `is_dlc`、`package_id`
- `ArtifactRefs` 指向大响应体、原始证据、AI 分析全文等外部对象

其中 `ArtifactRef` 建议至少包含：

```go
type ArtifactRef struct {
    ArtifactID   string
    Kind         string
    StorageKind  string
    URI          string
    ContentType  string
    SizeBytes    int64
    SHA256       string
    PreviewText  string
}
```

### 8.5 Sink 抽象

建议设计统一 sink 接口：

```go
type Sink interface {
    Kind() string
    Write(ctx context.Context, record ExecutionRecord) error
}
```

平台可以提供 `TraceManager` 负责：

- 根据任务配置决定是否留痕
- 根据级别裁剪记录内容
- 进行敏感字段脱敏
- 将记录分发到多个 sink

执行链路为：

`任务执行 -> 生成 ExecutionRecord -> TraceManager 处理 -> 写入多个 sink`

### 8.6 存储方向建议

建议把留痕和 artifact 存储分为三类：

#### 元数据存储

适合保存结构化执行摘要：

- `postgres`

建议将以下内容统一存入 PostgreSQL：

- `runs`
- `run_steps`
- `findings`
- `artifacts` 元数据
- 小型 `jsonb` 结果和聚合摘要

PostgreSQL 适合这类“结构化 + 可检索 + 追加写”的运行数据，也便于后续按时间分区、给 `jsonb` 建索引和做全文检索。

#### 大对象存储

适合保存较大输出：

- S3
- Cloudflare R2
- MinIO

建议将以下内容写入对象存储：

- 原始 HTTP 响应体
- AI 分析输入输出全文
- 截图、HTML 快照、压缩包
- 较大的中间 JSON 文件

建议统一抽象为 `S3-compatible object storage`：

- 托管优先推荐 `Cloudflare R2`
- 若基础设施已经在 AWS，可直接使用 `S3`
- 私有化部署优先推荐 `MinIO`

平台不建议默认把大 artifact 直接存入 PostgreSQL 大字段或 large objects，而应只在 PostgreSQL 中保留引用、摘要和校验信息。

#### 实时转发

适合接审计或通知系统：

- `webhook`
- `kafka`

V1 可先保留 `webhook` 扩展点。

### 8.7 推荐的 V1 存储组合

建议默认组合如下：

- 结构化运行数据写入 `PostgreSQL`
- 大 artifact 按需写入 `S3-compatible object storage`
- 关键任务可额外推送到 `webhook`

### 8.8 Artifact 存储建议

建议单独抽象 `ArtifactStore`：

```go
type ArtifactStore interface {
    Kind() string
    Put(ctx context.Context, key string, body io.Reader, meta ArtifactMeta) (ArtifactRef, error)
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
    Delete(ctx context.Context, key string) error
}
```

职责建议：

- 负责写入和读取大 artifact 本体
- 负责生成临时访问地址
- 负责校验摘要、大小、MIME 类型
- 与 PostgreSQL 中的 `artifacts` 元数据表配合工作

artifact key 建议包含环境、任务、日期和 run 维度，例如：

`prod/buyer-goods-price-audit/2026/04/27/<run_id>/goods-detail-123.json`

### 8.9 留痕失败策略

留痕模块属于辅助链路，不应影响主任务执行结果。

建议规则：

- sink 写入失败时记录平台日志
- sink 写入失败时更新平台内部错误计数
- 不改变主任务的成功或失败判定
- 对象存储短暂失败时，必要时支持降级写入本地 spool 目录并异步补传

## 9. 运行时状态模型

建议平台维护统一任务状态：

- `loaded`
- `running`
- `stopped`
- `disabled`
- `reload_failed`

每个任务至少维护以下状态信息：

- `last_run_at`
- `next_run_at`
- `last_run_status`
- `last_check_status`
- `last_error`
- `last_duration`
- `last_reload_error`

对于 `scenario_check`，建议额外记录：

- `last_sample_seed`
- `last_sample_count`
- `last_mismatch_count`

这些状态用于：

- API 查询
- 前端展示
- 告警判断
- 运维排障

## 10. 持久化设计

V1 建议拆成两类持久化：

- 配置来源：文件系统
- 状态与历史：PostgreSQL 和对象存储

### 10.1 配置来源

任务配置以文件为准，平台不在数据库中维护任务主配置，便于 Git 管理、变更审计和批量发布。

### 10.2 历史与状态

建议持久化以下信息：

- 任务定义摘要
- 当前任务状态
- 最近执行记录
- 历史执行记录
- 重载失败记录
- 随机抽样任务的样本种子和差异明细
- artifact 元数据和外部对象引用

V1 建议：

- 当前状态、执行摘要、步骤、结论和 artifact 元数据保存在 `PostgreSQL`
- 大输出保存在 `S3-compatible object storage`

### 10.3 推荐的数据表

建议至少包含以下核心表：

- `runs`
- `run_steps`
- `findings`
- `artifacts`
- `task_runtime_state`
- `task_reload_failures`

其中：

- `runs` 保存一次执行的聚合状态和摘要
- `run_steps` 保存步骤级状态、耗时和小型输入输出
- `findings` 保存结构化结论，便于检索、聚合和告警
- `artifacts` 只保存对象存储引用、校验值、大小、预览文本和生命周期状态

### 10.4 Artifact 生命周期

建议明确 artifact 生命周期治理：

- PostgreSQL 记录 artifact 的 `created_at`、`expire_at`、`deleted_at`
- 对象存储 bucket 配置生命周期规则，自动清理过期对象
- 定期执行 GC 任务，修复 PostgreSQL 与对象存储之间的孤儿数据
- 删除 `run` 时默认不立即删除 artifact，而是按保留期异步清理

## 11. 对外接口建议

V1 先提供 HTTP API，不急于上完整前端。

建议接口：

- `GET /healthz`
- `GET /metrics`
- `GET /tasks`
- `GET /tasks/{id}`
- `GET /tasks/{id}/runs`
- `GET /tasks/{id}/runs/{run_id}`
- `GET /tasks/{id}/runs/{run_id}/artifacts`
- `GET /artifacts/{artifact_id}`
- `POST /tasks/{id}/run`
- `POST /tasks/{id}/runs/{run_id}/rerun`
- `POST /tasks/{id}/reload`
- `POST /tasks/{id}/enable`
- `POST /tasks/{id}/disable`

接口用途：

- 健康检查
- 查询任务列表和状态
- 查询单任务历史执行结果
- 查看单次运行的样本、差异和聚合结论
- 查看单次运行关联的 artifact 列表和元数据
- 按 artifact ID 获取元数据或临时下载地址
- 对同一批样本按记录的 seed 或样本集进行复跑
- 手工触发任务重载和启停

`/metrics` 建议对接 Prometheus。

## 12. 安全与治理建议

### 12.1 敏感信息管理

- 不要把密码、token、密钥直接明文写入仓库配置
- 优先支持环境变量引用或 secret 引用
- 留痕阶段要对敏感字段做脱敏
- 对 `scenario_check` 的请求头、Cookie、鉴权参数同样走 secret 引用
- PostgreSQL 和对象存储凭据同样通过 secret 引用注入，不直接写死在仓库

### 12.2 脚本执行约束

如果支持 `script_exec`，需要限制：

- 可执行目录
- 可执行命令白名单
- 单次执行超时
- 输出大小

### 12.3 稳定性保护

- 每类任务配置并发限制
- 单任务超时和重试控制
- `scenario_check` 单次样本数上限
- `scenario_check` fan-out 并发上限
- 单请求响应体大小限制和单次总输出限制
- 出网目标白名单或域名允许列表
- artifact 单对象大小上限和单次 run artifact 总量上限
- 对象存储上传失败重试与幂等 key 保护
- 留痕输出大小限制
- 文件监听防抖和配置校验保护

## 13. V1 落地顺序

建议按以下顺序实施：

1. 搭建目录结构和程序入口
2. 实现全局配置和任务配置模型
3. 实现驱动注册表和任务管理器
4. 实现任务目录扫描和 `fsnotify` 热更新
5. 实现 `http_check`、`tcp_check`、`script_exec` 三个基础驱动
6. 实现 `scenario_check` 驱动、抽样器和首个内建 evaluator
7. 实现 `PostgreSQL` 持久化层和 `S3-compatible ArtifactStore`
8. 实现 `TraceManager`、`postgres` sink 和 artifact 元数据管理
9. 实现 `/healthz`、`/tasks`、`/metrics`、运行详情和 artifact 查询接口
10. 补充告警通道和更复杂任务类型

## 14. 推荐的 V1 默认策略

- 每个任务至少保留 `summary` 级留痕
- 默认 sink 使用 `postgres`
- 大 artifact 默认落对象存储，数据库只保留引用和摘要
- 随机抽样任务默认记录 `sample_seed` 和样本主键，保证可复现
- 业务判定失败与平台执行失败分开记录
- 配置更新失败时保留旧任务继续运行
- 删除任务配置时保留历史执行记录
- 留痕失败不影响任务执行结果

## 15. 后续演进方向

V1 稳定后，可以继续扩展：

- 前端控制台
- 工作流型任务
- 任务依赖编排
- 多环境配置分层
- 分布式执行节点
- 更丰富的告警与通知策略
- 配置版本和发布回滚

## 16. 总结

`pulseops` 的核心不是“又一个监控项目”，而是一套配置驱动的运维任务运行平台。

平台通过“任务配置独立存放 + 驱动扩展 + 动态热更新 + 统一执行留痕”这四个核心能力，能够逐步演进为稳定、可审计、可扩展的运维底座。

V1 除了打稳运行框架、动态加载能力和留痕能力，也应把 `scenario_check` 这类受控业务巡检能力纳入一等支持，再逐步向告警、工作流和前端扩展。
