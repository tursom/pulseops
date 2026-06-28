# PulseOps 插件系统设计方案

## 1. 背景

PulseOps 当前已经有两类扩展雏形：

- 任务驱动：后端通过 `task.Driver` 注册 `http_check`、`tcp_check`、`script_exec`、`process_check`、`scenario_check`、`data_process`、`ai_analyze` 等任务类型。
- AI 数据源插件：`ai_analyze` 支持从插件目录加载 C ABI `.so`，扩展 `DataSourceRegistry`。

这两类机制还没有形成平台级插件系统：

- 没有统一插件清单、插件身份、版本、权限和状态。
- 没有插件中心 API，也没有前端可见的插件管理页面。
- 任务模板、AI 数据源、输出写入器、事件 Hook、通知 Sink 等扩展点分散。
- 外部插件的启停、校验、回滚、运行观测和安全隔离没有统一约束。

本方案目标是在不破坏现有任务模型的前提下，为 PulseOps 增加一套可治理、可观测、可渐进增强的插件系统。

## 2. 目标

插件系统需要支持以下能力：

1. 插件包统一安装到固定目录，由 manifest 描述身份、版本、能力、权限和默认配置。
2. 内置能力和外部插件能力统一进入 Plugin Catalog，前端可查看、启停、重载和诊断。
3. 插件可以扩展任务模板、任务驱动、AI 数据源、输出写入器、业务 evaluator、事件 Hook、通知 Sink、前端入口。
4. 外部插件优先使用隔离运行时，不默认把不可信代码加载进 PulseOps 主进程。
5. 插件能力可以被任务创建向导消费，例如新增模板卡片、AI 数据源选项、参数 schema。
6. 插件运行要有权限声明、超时、资源限制、错误隔离和审计留痕。
7. 插件升级、禁用、加载失败不能影响已有核心任务调度和管理页面启动。

## 3. 非目标

第一阶段不做这些事：

- 不把通用 Go `.so` 作为任务驱动热加载到主进程。现有 AI C ABI 数据源插件可以兼容保留，但不扩展为所有插件的推荐形态。
- 不允许插件直接拿到 PostgreSQL、MinIO、AI API Key 等全量内部凭证。
- 不在前端直接执行插件提供的任意 JS。前端扩展优先使用 schema 驱动表单、菜单入口和受控 iframe。
- 不做公开插件市场。先支持本地目录安装和内部插件包治理。

## 4. 总体架构

```text
plugins/
  steam-price-diagnostics/
    releases/
      1.0.0/
        pulseops.plugin.toml
        bin/diagnose
        README.md
      1.1.0/
        pulseops.plugin.toml
        bin/diagnose
        README.md
    active -> releases/1.0.0
  webhook-notifier/
    releases/2026.06.27-1/
      pulseops.plugin.toml
      bin/notifier

PulseOps
  config.LoadGlobal
      |
      v
  plugin.Manager
      |-- 注册内置插件包
      |-- 扫描 plugins/*/releases/*/pulseops.plugin.toml
      |-- 读取 DB 中 active version 和 release 状态
      |-- 校验权限、版本、能力冲突
      v
  Plugin Catalog
      |-- Task Driver Registry
      |-- AI DataSource Registry
      |-- Output Writer Registry
      |-- Evaluator Registry
      |-- Hook Bus
      |-- Frontend Extension API
```

核心原则：

- **Catalog 先行**：所有能力先进入统一 catalog，再按能力类型注册到运行时。
- **候选注册表替换**：activate、disable、rollback 时构建候选 registry generation，通过校验后原子替换 active generation；失败时保留旧 generation。
- **能力可见但受控**：插件可以声明很多能力，但只有启用且校验通过的能力会进入运行时。
- **运行时隔离优先**：外部任务驱动和数据源优先走 process/http 协议，主进程只负责协议编排。
- **零停机发布优先**：插件上架、下架、升级和回滚都不得要求重启 PulseOps；新旧版本通过 registry generation 并存，旧版本 draining 完成后再回收。

## 5. 插件包结构

每个插件包是一个目录：

```text
plugins/<plugin-id>/
  releases/<version>/
    pulseops.plugin.toml
    bin/
    schemas/
    static/
    README.md
  active -> releases/<active-version>
```

约束：

- 插件版本目录一旦进入 `validated` 或 `active` 状态后不可原地修改。
- 新版本必须写入新的 release 目录，例如 `releases/1.1.0/` 或 `releases/2026.06.27-1/`。
- `active` 软链接只用于人工排查和离线工具，运行时切换以 DB 中的 `active_version` 为准。
- 删除 release 前必须确认没有 registry generation 和 in-flight run 仍引用它。
- 容器部署时，插件目录必须挂载为可写持久卷；镜像内置插件只能作为 bundled official release，不作为在线更新目录。

`pulseops.plugin.toml` 示例：

```toml
schema_version = "pulseops.plugin/v1"
id = "steam-price-diagnostics"
name = "Steam 价格诊断"
version = "1.0.0"
description = "提供 Steam 价格异常诊断任务模板和 AI 数据源"
author = "YYM"
homepage = "https://internal.example.com/pulseops/plugins/steam-price-diagnostics"
enabled = true
permissions = ["network:outbound", "runs:read", "ai:read"]

[[task_templates]]
id = "steam-price-diagnose-template"
kind = "ai_analyze"
title = "Steam 价格异常诊断"
description = "基于上游价格检查结果、历史运行和历史 AI 分析生成诊断结论"

[task_templates.defaults]
trigger = "on_run"
timeout = "45s"

[task_templates.params]
analysis_type = "diagnose"

[[data_sources]]
name = "grpc_game_inventory"
title = "gRPC 游戏库存查询"
protocol = "grpc"
runtime = "builtin"
permissions = ["network:outbound", "grpc:call"]

[data_sources.schema]
endpoint = { type = "string", required = true, description = "gRPC 服务地址，如 inventory.service:9090" }
service = { type = "string", required = true, description = "完整服务名，如 yym.inventory.v1.InventoryService" }
method = { type = "string", required = true, description = "Unary 方法名，如 GetInventory" }
request = { type = "object", required = true, description = "请求体 JSON，将按 proto schema 转换" }

[[ai_data_sources]]
name = "steam_price_context"
title = "Steam 价格上下文"
runtime = "process"
entrypoint = "bin/steam-price-context"
permissions = ["runs:read", "network:outbound"]

[[ui_extensions]]
id = "steam-price-help"
title = "Steam 价格诊断说明"
path = "/plugins/steam-price-diagnostics/help"
```

## 6. 能力模型

统一能力类型：

| type | 用途 | 第一阶段 |
| --- | --- | --- |
| `task_template` | 给任务创建向导提供模板、默认参数、表单 schema | 支持 |
| `task_driver` | 新增任务执行类型 | 第二阶段支持 process/http |
| `data_source` | 平台通用数据源，供 `data_process`、`ai_analyze`、后续 `scenario_check` 复用 | 第二阶段 |
| `ai_data_source` | AI 专用数据源扩展；保留现有 `ai_analyze` 兼容能力 | 支持 manifest 展示，第二阶段运行时接入 |
| `output_writer` | 扩展 AI 输出写入方式 | 第二阶段 |
| `evaluator` | 扩展 `scenario_check` 业务评估器 | 第二阶段 |
| `trace_sink` | 扩展留痕/通知目标 | 第三阶段 |
| `hook` | 监听 run/task/plugin 事件 | 第三阶段 |
| `ui_extension` | 前端菜单、帮助页、iframe 入口 | 第一阶段展示，第二阶段受控接入 |

当前内置任务驱动不再作为“特殊内置分支”暴露，而是迁移为随 PulseOps 发布的官方插件。官方插件仍然可以在主仓库内实现和编译，但必须通过同一套 manifest、catalog、capability registry 和 generation 机制注册。

```json
{
  "id": "@pulseops/core-tasks",
  "name": "PulseOps 官方基础任务插件",
  "official": true,
  "bundled": true,
  "enabled": true,
  "capabilities": [
    { "type": "task_driver", "name": "http_check" },
    { "type": "task_driver", "name": "tcp_check" },
    { "type": "task_driver", "name": "script_exec" },
    { "type": "task_driver", "name": "process_check" }
  ]
}
```

### 6.1 官方插件拆分

官方插件使用 `@pulseops/` 命名空间，随平台版本发布，默认启用。它们不是第三方插件，但不允许绕过插件系统直接注册到运行时。

建议拆分：

| 官方插件 | 能力 | 当前来源 | 默认状态 |
| --- | --- | --- | --- |
| `@pulseops/core-tasks` | `http_check`、`tcp_check`、`script_exec`、`process_check` | `internal/task` | enabled |
| `@pulseops/scenario` | `scenario_check`、官方 evaluator 模板 | `internal/task`、`internal/scenario`、`internal/evaluator` | enabled |
| `@pulseops/data-process` | `data_process`、run/artifact 数据源模板 | `internal/task/upstream_data.go` | enabled |
| `@pulseops/ai` | `ai_analyze`、内置 AI 数据源、内置 output writer | `internal/ai` | 跟随 `[ai].enabled` |
| `@pulseops/trace-sinks` | `postgres`、`webhook` trace sink | `internal/trace` | enabled |
| `@pulseops/grpc-source` | `grpc` 通用数据源 adapter | 新增官方插件 | 可配置 enabled |

官方插件目录形态：

```text
internal/plugin/bundled/
  core-tasks/pulseops.plugin.toml
  scenario/pulseops.plugin.toml
  data-process/pulseops.plugin.toml
  ai/pulseops.plugin.toml
  trace-sinks/pulseops.plugin.toml
  grpc-source/pulseops.plugin.toml
```

也可以把 manifest embed 到 Go 二进制中，但 catalog 中仍然要展示 manifest 内容、版本、能力和状态。

官方插件约束：

- 官方插件默认随平台升级，不走本地 `plugins/` release 目录。
- 官方插件也有 release/version，通常等于 PulseOps 版本或模块版本。
- 官方插件能力注册必须经过 plugin manager，不能在 `app.New` 中直接手写 driver list。
- 官方插件可以被平台配置禁用，但需要区分“可禁用”和“核心必需”：
  - `script_exec`、`process_check` 可以禁用。
  - `http_check`、`tcp_check` 可以禁用，但已有任务会进入 `plugin_disabled`。
  - `@pulseops/core-runtime` 这类未来核心插件不可禁用。
- 官方插件的 enable/disable 也生成新的 registry generation，保持和第三方插件一致的零停机语义。

官方插件分两种发布形态：

| 形态 | 说明 | 是否可独立零停机更新代码 |
| --- | --- | --- |
| `bundled_official` | 随 PulseOps 二进制编译和发布，适合当前内置任务迁移的第一阶段 | 否；代码更新跟随平台部署，但启停/切 generation 不停机 |
| `external_official` | 由 PulseOps 团队发布到 `plugins/@pulseops/<name>/releases/<version>/`，通过 process/http/grpc adapter 运行 | 是；按 release/generation/draining 流程在线更新 |

迁移当前内置任务时先采用 `bundled_official`，避免改动执行语义；后续如果某个官方任务驱动需要独立发版，再迁移为 `external_official`。

迁移后，`app.New` 只负责装配插件系统：

```text
old:
  driverList := []task.Driver{HTTPCheckDriver{}, TCPCheckDriver{}, ...}
  drivers.Register(driver)

new:
  pluginManager.RegisterBundled(@pulseops/core-tasks)
  pluginManager.RegisterBundled(@pulseops/scenario)
  pluginManager.RegisterBundled(@pulseops/data-process)
  pluginManager.RegisterBundled(@pulseops/ai)
  activeGeneration := pluginManager.BuildGeneration()
  runtime.Manager 使用 activeGeneration.DriverRegistry()
```

## 7. 外部运行时协议

外部插件推荐两种运行时：

- `process`：PulseOps 启动子进程，通过 stdin/stdout 传 JSON envelope。适合内部插件、私有部署、低延迟任务。
- `http`：PulseOps 调用插件服务 HTTP endpoint。适合独立部署、跨语言、资源隔离更强的插件。

现有 AI `.so` 作为兼容运行时：

- `c_abi`：仅用于 AI 数据源，保持当前 `plugin_name` / `plugin_fetch` / `plugin_free` 协议。

Process 协议请求：

```json
{
  "protocol": "pulseops.plugin/v1",
  "call_id": "01J...",
  "plugin_id": "steam-price-diagnostics",
  "capability": "steam_price_context",
  "action": "fetch",
  "timeout_ms": 30000,
  "context": {
    "task_id": "diagnose-steam-price",
    "run_id": "run-123",
    "trigger_type": "dependent"
  },
  "config": {
    "source_task_id": "steam-price-check"
  },
  "input": {
    "trigger_run": {
      "task_id": "steam-price-check",
      "check_status": "fail"
    }
  }
}
```

Process 协议响应：

```json
{
  "ok": true,
  "data": {
    "price_diff_count": 3,
    "sample_goods": ["730-xxx"]
  },
  "summary": {
    "source": "steam_price_context"
  }
}
```

错误响应：

```json
{
  "ok": false,
  "error": {
    "code": "upstream_timeout",
    "message": "Steam API timeout after 5s",
    "retryable": true
  }
}
```

### 7.1 数据源协议适配器

`data_source` 是平台通用能力，负责把外部系统数据标准化为 JSON-like 数据对象，供 `data_process`、`ai_analyze` 和后续 `scenario_check` 复用。

第一批建议支持的数据源协议：

| protocol | 用途 | 运行方式 |
| --- | --- | --- |
| `http_json` | 调 HTTP/REST 接口并解析 JSON | builtin adapter |
| `grpc` | 调 gRPC Unary 方法并把响应转为 JSON | builtin adapter |
| `run_record` | 读取 PulseOps 任务运行记录、summary、payload、artifact | builtin adapter |
| `process` | 调插件子进程，由插件自行采集数据 | process runtime |
| `http_plugin` | 调插件服务 HTTP endpoint，由插件自行采集数据 | http runtime |

gRPC 数据源不要求插件作者写 Go 代码。插件可以只声明一个 `protocol = "grpc"` 的 `data_source`，PulseOps 使用内置 gRPC adapter 执行调用。

gRPC 数据源最小配置：

```toml
[[data_sources]]
name = "grpc_game_inventory"
title = "gRPC 游戏库存查询"
protocol = "grpc"
runtime = "builtin"
permissions = ["network:outbound", "grpc:call"]

[data_sources.defaults]
timeout = "5s"
max_receive_bytes = 1048576
use_reflection = true

[data_sources.schema]
endpoint = { type = "string", required = true }
service = { type = "string", required = true }
method = { type = "string", required = true }
request = { type = "object", required = true }
metadata = { type = "object", required = false }
tls = { type = "object", required = false }
```

任务中引用示例：

```json
{
  "type": "grpc_game_inventory",
  "alias": "inventory",
  "config": {
    "endpoint": "inventory.service:9090",
    "service": "yym.inventory.v1.InventoryService",
    "method": "GetInventory",
    "request": {
      "user_id": "{{ .Run.Labels.user_id }}"
    },
    "metadata": {
      "x-env": "prod"
    },
    "timeout": "3s"
  },
  "on_error": "fail"
}
```

gRPC adapter 约束：

- 第一阶段只支持 Unary RPC；server streaming、client streaming、bidirectional streaming 后续单独设计。
- 优先使用 gRPC reflection；生产环境也允许配置 `proto_descriptor_set` 或 `proto_files`，避免依赖线上 reflection。
- 请求体用 JSON 表达，adapter 根据 proto schema 转换为 protobuf message。
- 响应统一转为 JSON object，写入 `DataSources.<alias>`。
- 支持 TLS、mTLS、metadata、deadline、max receive bytes。
- metadata 中的 token、authorization 等敏感字段必须走 secret reference，不直接落 manifest。
- gRPC 调用错误需要标准化为 `{code,message,retryable,details}`，并遵守数据源的 `on_error` 策略。

## 8. 权限和安全

插件 manifest 必须声明权限。PulseOps 启动、validate、activate 时校验权限是否被允许。

建议权限命名：

| 权限 | 含义 |
| --- | --- |
| `runs:read` | 读取运行记录、summary、payload |
| `runs:write` | 写入运行结果或扩展输出 |
| `artifacts:read` | 读取对象存储 artifact |
| `artifacts:write` | 写入 artifact |
| `tasks:read` | 读取任务定义 |
| `tasks:write` | 修改任务定义 |
| `settings:read` | 读取平台设置摘要 |
| `network:outbound` | 发起外部网络请求 |
| `grpc:call` | 发起 gRPC 调用；通常还需要 `network:outbound` |
| `process:exec` | 执行本地子进程 |
| `ai:read` | 读取 AI 分析记录 |
| `ai:write` | 写入 AI 分析结果 |

隔离策略：

- 默认不向插件透传环境变量，只透传 allowlist。
- 插件不直接读取主配置中的密钥，使用 secret reference。
- 每次调用都有 timeout 和最大输出字节数。
- process 插件工作目录固定在插件包目录。
- 插件 stdout/stderr 纳入运行留痕，但需要截断。
- 插件加载失败只标记 plugin/capability error，不阻断平台启动，除非 `[plugins].strict = true`。

## 9. 全局配置

建议新增：

```toml
[plugins]
enabled = true
dir = "plugins"
strict = false
allow_process = true
allow_http = true
allow_grpc = true
default_timeout = "30s"
max_output_bytes = 1048576
env_allowlist = ["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"]
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `enabled` | 是否扫描外部插件目录 |
| `dir` | 插件安装目录 |
| `strict` | true 时插件加载失败会阻断启动；默认 false |
| `allow_process` | 是否允许 process 运行时 |
| `allow_http` | 是否允许 http 运行时 |
| `allow_grpc` | 是否允许内置 gRPC 数据源 adapter 发起 RPC |
| `default_timeout` | 插件调用默认超时 |
| `max_output_bytes` | 单次插件响应最大字节数 |
| `env_allowlist` | 允许传给 process 插件的环境变量 |

兼容策略：

- `[ai].plugin_dir` 保留一个版本周期。
- 若 `[plugins].dir` 未配置，默认仍使用 `plugins`。
- AI C ABI 插件先从 `[ai].plugin_dir` 加载，后续迁移到 manifest 的 `ai_data_sources.runtime = "c_abi"`。

## 10. 后端 API

新增 API：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/plugins` | 获取插件 catalog |
| `GET` | `/api/plugins/{id}` | 获取单个插件详情 |
| `GET` | `/api/plugins/{id}/releases` | 获取插件版本列表和每个版本的引用状态 |
| `POST` | `/api/plugins/install` | 写入或登记一个 staged release，不影响当前运行版本 |
| `POST` | `/api/plugins/{id}/releases/{version}/validate` | 校验指定 release 的 manifest、权限、入口和 readiness |
| `POST` | `/api/plugins/{id}/releases/{version}/activate` | 将指定 release 切为 active，生成新 registry generation |
| `POST` | `/api/plugins/{id}/rollback` | 回滚到上一个可用 release，生成新 registry generation |
| `POST` | `/api/plugins/{id}/disable` | 下架插件：停止新任务使用并让旧 generation draining |
| `POST` | `/api/plugins/{id}/enable` | 重新启用当前 active release |
| `POST` | `/api/plugins/reload` | 重新扫描插件目录，只构建候选 catalog，不直接覆盖 active version |
| `GET` | `/api/plugin-capabilities` | 按 type/kind 查询能力 |
| `POST` | `/api/plugins/gc` | 清理无引用的 retired release 和过期 generation |

Catalog 响应示例：

```json
{
  "generated_at": "2026-06-27T10:00:00Z",
  "plugin_dir": "/app/plugins",
  "status": "ready",
  "stats": {
    "total": 4,
    "enabled": 3,
    "disabled": 1,
    "errors": 0,
    "capabilities": 12
  },
  "plugins": []
}
```

零停机更新需要持久化 release 和 generation 状态，不能只靠 `kv_metadata`：

| 表 | 用途 |
| --- | --- |
| `plugin_packages` | 插件身份、名称、当前启用状态、是否官方、是否 bundled |
| `plugin_releases` | 每个插件版本的 manifest、路径、校验结果、状态、checksum |
| `plugin_active_versions` | 每个插件当前 active release，使用事务或 CAS 更新 |
| `plugin_generations` | 每次 registry 构建生成的不可变 generation |
| `plugin_generation_refs` | generation 被运行中 run 引用的计数和最后释放时间 |
| `plugin_events` | install、validate、activate、disable、rollback、gc 的审计事件 |

`kv_metadata` 只可用于早期本地开发或 catalog-only 原型；只要要求不停机更新，就必须有 release/generation 级别的持久状态。

## 11. 前端设计

新增一级页面：`插件中心`。

页面模块：

- 概览：总插件数、启用数、错误数、能力数。
- 插件列表：名称、active version、状态、draining 数、能力数量、权限、最近错误。
- 插件详情：manifest 信息、release 列表、generation 引用、能力列表、权限、入口文件、默认配置。
- 操作：安装 release、校验、激活、回滚、禁用、卸载、GC。
- 能力视图：按 `task_template`、`task_driver`、`data_source`、`ai_data_source`、`hook` 分组。
- 发布状态：展示当前 active generation、旧 generation refcount、release 是否可删除。

任务创建向导改造：

- 模板卡片从内置 `KIND_OPTIONS` 扩展为后端返回的 `task_template`。
- 通用数据源类型从硬编码数组扩展为 `/api/plugin-capabilities?type=data_source`。
- `ai_analyze` 继续兼容 `/api/plugin-capabilities?type=ai_data_source`，但优先复用通用 `data_source`。
- 插件提供的参数表单优先使用 JSON Schema 渲染，不执行插件 JS。
- 插件模板可声明推荐触发方式、默认 labels、默认 params 和是否支持依赖触发。

平台设置页改造：

- AI 配置中的“插件目录”迁移到插件中心。
- 设置页只保留全局开关和运行安全配置。

## 12. 零停机上下架与更新方案

### 12.1 零停机不变量

插件系统必须满足以下不变量：

1. PulseOps 主进程不因插件安装、启用、禁用、升级、回滚而重启。
2. 新 release 校验失败时，当前 active release 和当前 registry generation 不变。
3. 任意运行中的任务只引用它启动时绑定的 registry generation，不被中途切换。
4. 新任务和新 run 只使用当前 active generation。
5. 旧 release 进入 `draining` 后不再接收新 run，但必须允许已有 run 正常结束。
6. 只有当旧 release 的引用计数为 0，且超过最小保留时间后，才允许 GC 删除。
7. 插件下架不等于立即杀死运行中插件进程；默认行为是停止新流量并等待 draining。
8. 所有 activate、disable、rollback 都必须有审计事件，并可在插件中心看到结果。

### 12.2 状态机

插件 release 状态：

```text
uploaded
  -> staged
  -> validating
  -> validated
  -> active
  -> draining
  -> retired
  -> deleted

任意状态 -> failed
failed -> staged        # 修复 manifest 或重新上传后再次校验
active -> failed        # 运行时健康检查持续失败，但不自动删除
draining -> active      # 回滚时可重新激活仍保留的旧 release
```

状态含义：

| 状态 | 含义 | 是否可接新 run |
| --- | --- | --- |
| `uploaded` | 文件已上传或目录已写入，尚未登记完成 | 否 |
| `staged` | release 已登记，可执行 validate | 否 |
| `validating` | 正在校验 manifest、权限和运行时 readiness | 否 |
| `validated` | 校验通过，可被 activate | 否 |
| `active` | 当前线上版本，新 run 可使用 | 是 |
| `draining` | 已被新版本替换或被下架，只服务已开始的 run | 否 |
| `retired` | 无引用，等待保留期后清理 | 否 |
| `deleted` | 已从插件目录和 DB release 状态中删除 | 否 |
| `failed` | 校验或运行时健康检查失败 | 否 |

插件包状态：

| 状态 | 含义 |
| --- | --- |
| `enabled` | 有 active release，能力对新任务可见 |
| `disabled` | 人工下架，不对新任务和新 run 暴露能力 |
| `degraded` | active release 存在，但部分能力或 readiness 异常 |
| `not_installed` | 无任何 release |

### 12.3 Registry Generation

每次插件 catalog 成功构建后生成一个不可变 `generation_id`：

```text
generation_id = "plugin-gen-20260627-153000-00042"
active map:
  steam-price-diagnostics -> 1.1.0
  webhook-notifier -> 2026.06.27-1
capabilities:
  steam-price-diagnostics:ai_data_source:steam_price_context
  webhook-notifier:hook:run_finished
```

运行规则：

- runtime manager 持有 `atomic.Value` 或等价机制，指向当前 active generation。
- 任务开始执行时从 active generation 拿到 driver/source/hook 引用，并增加 generation refcount。
- 任务结束时释放 refcount。
- activate/disable/rollback 只替换 active generation 指针，不修改旧 generation。
- GC 只处理 refcount 为 0 且超过 `generation_retention` 的 generation 和 release。

这保证更新时不会抢占正在运行的任务，也不会把运行中的调用切到新插件版本。

### 12.4 上架流程

上架指引入一个新插件或一个新 release，但不影响线上流量。

流程：

1. 将插件写入临时目录：`plugins/.staging/<plugin-id>/<version>/`。
2. 计算 checksum，校验目录完整性。
3. 解析 `pulseops.plugin.toml`，校验 `id`、`version`、schema version、能力唯一性。
4. 原子重命名到 `plugins/<plugin-id>/releases/<version>/`。
5. 写入 `plugin_releases`，状态为 `staged`。
6. 运行 validate：manifest、权限、入口文件、process 可执行位、http endpoint 格式、schema。
7. 对 process/http 插件执行 readiness check。
8. 校验通过后状态变为 `validated`。

上架完成后，新 release 只出现在插件中心，不会自动承接新 run。必须显式 activate 才会上线。

### 12.5 更新流程

更新指把同一个插件从旧 active release 切到新 release。

流程：

1. 按上架流程把新版本推进到 `validated`。
2. 构建候选 catalog 和候选 registry generation。
3. 对候选 generation 做 dry-run validate：
   - 所有 active 插件能力无冲突。
   - 所有启用任务的 driver/source/evaluator 仍可解析。
   - 新插件能力的 schema 和默认参数可被任务向导解析。
4. 对新版本 process/http runtime 做预热：
   - process worker pool：先启动新 worker pool。
   - http runtime：调用 `/healthz` 或 manifest 声明的 readiness endpoint。
   - per-call process：验证入口可执行并执行 `action=validate_runtime`。
5. 在 DB 事务中 CAS 更新 `plugin_active_versions`：
   - 条件：当前 active 仍是旧版本。
   - 写入新 active version。
   - 创建新 `plugin_generations`。
   - 写入 `plugin_events.activate`。
6. 原子替换 runtime manager 的 active generation 指针。
7. 将旧 active release 标记为 `draining`。
8. 新 run 使用新 generation；旧 run 继续使用旧 generation。
9. draining release refcount 归零后标记为 `retired`。

失败处理：

- 步骤 1-4 失败：不触碰 active version。
- 步骤 5 CAS 失败：说明有并发发布，放弃本次 activate 并要求重新读取状态。
- 步骤 6 后发现新版本健康检查失败：触发人工或自动 rollback，生成新的 generation 指回旧 release。

### 12.6 下架流程

下架分两种：

| 操作 | 用途 | 行为 |
| --- | --- | --- |
| `disable` | 临时下架，可回滚 | 停止新任务和新 run 使用该插件，旧 run draining |
| `uninstall` | 永久卸载 | 只有所有 release retired 后才允许删除文件和 DB 状态 |

disable 流程：

1. 校验插件存在 active release。
2. 构建候选 generation，移除该插件的能力。
3. 校验现有任务影响面：
   - 依赖该插件能力的启用任务标记为 `plugin_disabled`。
   - 不启动新的依赖任务 run。
   - 已经开始的 run 不受影响。
4. DB 事务中将 `plugin_packages.status` 设为 `disabled`。
5. 创建新 generation 并原子替换 active generation。
6. active release 进入 `draining`。
7. refcount 归零后进入 `retired`。

uninstall 流程：

1. 插件必须处于 `disabled` 或 `retired`。
2. 所有 release refcount 必须为 0。
3. 没有任务定义仍引用该插件能力；若有引用，必须先删除任务或确认强制卸载。
4. 删除 release 文件，写入 `plugin_events.uninstall`。

### 12.7 回滚流程

回滚是一次特殊 activate，不是恢复旧目录：

1. 从 `plugin_releases` 找到最近一个 `validated`、`draining` 或 `retired` 且文件仍存在的旧 release。
2. 对旧 release 重新执行快速 readiness check。
3. 构建候选 generation。
4. CAS 更新 active version 指向旧 release。
5. 原子替换 active generation。
6. 当前坏版本进入 `draining` 或 `failed`。

回滚要求旧 release 在 `retention` 内不被 GC。默认建议保留最近 3 个 release 或至少保留 7 天。

### 12.8 C ABI 插件约束

现有 AI `.so` 插件只能作为兼容能力，不能作为强零停机更新的推荐路径：

- 不允许原地覆盖已加载 `.so` 文件。
- `.so` release 必须放在版本目录中，文件名可带版本或 checksum。
- 新 `.so` 只允许在新 generation 中加载。
- 旧 `.so` 是否能卸载取决于运行时和库自身行为；即使调用 `Dlclose`，也不能把它作为稳定资源回收保证。
- 需要强零停机和可回收能力的插件，应使用 process/http runtime。

### 12.9 并发和一致性

发布操作必须串行化到插件级别：

- 同一个 `plugin_id` 同一时刻只允许一个 install/activate/disable/rollback。
- 不同插件可以并行 validate，但 activate 时必须对全局 generation 做一次能力冲突校验。
- active version 更新使用 DB 事务和 CAS 条件，避免两个发布互相覆盖。
- `/api/plugins/reload` 只能刷新 catalog 视图，不能绕过 activate 流程直接上线 release。

任务运行一致性：

- run record 中记录 `plugin_generation_id`。
- 插件调用日志中记录 `plugin_id`、`plugin_version`、`capability_id`。
- 任务详情页能展示“当前任务引用的插件能力是否仍 active”。

### 12.10 场景矩阵

| 场景 | 入口操作 | 对新 run 的影响 | 对运行中 run 的影响 | 失败处理 |
| --- | --- | --- | --- | --- |
| 新插件上架 | install + validate | 无影响，直到 activate 才可见 | 无影响 | release 停在 `failed` 或 `staged`，active generation 不变 |
| 新版本更新 | validate + activate | activate 成功后新 run 使用新 generation | 旧 run 继续使用旧 generation | validate/readiness/CAS 失败均不改变 active version |
| 临时下架 | disable | 新 run 不再使用该插件能力 | 旧 run 继续执行直到 draining 完成 | disable generation 构建失败则保持 enabled |
| 永久卸载 | uninstall | 仅允许 retired release 卸载 | 不允许卸载仍有引用的 release | refcount 不为 0 时拒绝 |
| 回滚 | rollback | 新 run 切回旧 release 的新 generation | 已开始的坏版本 run 不被强杀，按策略自然结束或任务级取消 | 旧 release readiness 失败则拒绝回滚 |
| 插件目录 reload | reload | 不切流，只刷新可见 release/catalog | 无影响 | 只记录 catalog error |

## 13. 生命周期

启动流程：

1. 读取全局配置。
2. 初始化 store、artifact store、trace manager。
3. 创建 `plugin.Manager`。
4. 注册 bundled official 插件包。
5. 读取 `plugin_packages`、`plugin_releases`、`plugin_active_versions`。
6. 扫描外部插件目录，只登记 DB 中不存在的 staged release，不自动上线。
7. 根据 active versions 构建启动 generation。
8. 校验 manifest、权限、runtime、能力冲突。
9. 启动 generation 构建失败时：
   - `strict=false`：平台 degraded 启动，插件 catalog 标记错误，核心任务继续可用。
   - `strict=true`：启动失败。
10. 暴露 `/api/plugins`。

Reload 流程：

1. 保留当前 registry。
2. 重新扫描插件目录，发现新增 release 时登记为 `staged`。
3. 重新构建 catalog 视图。
4. 不修改 active version，不替换 active generation。
5. 如果用户执行 activate/disable/rollback，再按零停机流程生成新 generation。

禁用流程：

1. 写入 DB 覆盖状态。
2. 构建不含该插件能力的候选 generation。
3. 校验影响面并提示受影响任务。
4. 原子替换 active generation。
5. 该插件能力不再用于新任务校验和新 run。
6. 已存在任务若依赖被禁用能力，状态展示为 `plugin_disabled` 或 validate 失败。
7. 旧 generation draining 完成后 release 才可 retired。

## 14. 运行观测

新增指标：

- `pulseops_plugins_total{status}`
- `pulseops_plugin_releases_total{plugin_id,status}`
- `pulseops_plugin_generation_refs{generation_id}`
- `pulseops_plugin_capabilities_total{type,status}`
- `pulseops_plugin_calls_total{plugin_id,capability,action,status}`
- `pulseops_plugin_call_duration_seconds{plugin_id,capability,action}`
- `pulseops_plugin_call_output_bytes{plugin_id,capability,action}`

日志字段：

- `plugin_id`
- `plugin_version`
- `plugin_generation_id`
- `capability`
- `capability_type`
- `call_id`
- `runtime`
- `duration_ms`
- `status`
- `error`

UI 中每个插件显示最近加载错误和最近调用错误。

## 15. 与现有模块的关系

| 现有模块 | 改造方式 |
| --- | --- |
| `internal/task` | 保持 `Driver` 接口；当前内置 driver 迁入 `@pulseops/core-tasks`、`@pulseops/scenario`、`@pulseops/data-process` 官方插件 |
| `internal/ai` | 迁入 `@pulseops/ai` 官方插件；保留 C ABI 数据源兼容；新增从 plugin catalog 注册数据源 |
| `internal/evaluator` | 将 evaluator 注册表纳入 capability registry；官方 evaluator 归入 `@pulseops/scenario` |
| `internal/trace` | `postgres`、`webhook` sink 归入 `@pulseops/trace-sinks` 官方插件 |
| `internal/app` | 不再直接拼 `driverList` 注册；只注册 bundled official 插件并从 active generation 取得 driver/evaluator/source registry |
| `internal/api` | 新增 plugin catalog API，不改变现有任务 API |
| `web/src/components/task-form` | 从 API 读取模板和 `data_source` 能力，内置选项作为 fallback |
| `web/src/pages/Settings.tsx` | 插件目录迁移到插件中心或平台配置摘要 |

## 16. 实施计划

### 阶段 1：Catalog 和插件中心

- 新增 `internal/plugin` 包。
- 支持扫描 `plugins/*/releases/*/pulseops.plugin.toml`。
- 新增 `plugin_packages`、`plugin_releases`、`plugin_active_versions`、`plugin_generations`、`plugin_generation_refs`、`plugin_events`。
- 当前内置任务驱动迁移为 bundled official 插件：`@pulseops/core-tasks`、`@pulseops/scenario`、`@pulseops/data-process`。
- AI、trace sink、官方 gRPC 数据源分别注册为 `@pulseops/ai`、`@pulseops/trace-sinks`、`@pulseops/grpc-source` 官方插件。
- 新增 `/api/plugins`、`/api/plugin-capabilities`、release validate/activate/disable/rollback API。
- 前端新增“插件中心”页面。
- 支持 install、validate、activate、disable、rollback、reload，但第一阶段只管理 catalog 和内置能力，不执行外部代码。
- runtime manager 引入 active generation 指针和 run 级 generation 引用记录，为后续外部插件运行做零停机底座。

验收：

- 没有 plugins 目录时平台正常启动。
- manifest 错误能在插件中心展示，不阻断核心功能。
- `http_check`、`tcp_check`、`script_exec`、`process_check`、`scenario_check`、`data_process` 都来自官方插件能力，不再由 `app.New` 直接注册。
- 官方插件在插件中心展示 `official=true`、`bundled=true`、版本和能力列表。
- 禁用可禁用的官方 task driver 后，新建/保存对应 kind 的任务会校验失败，已有任务状态展示 `plugin_disabled`。
- activate 一个新 manifest release 后，active generation 变化但 PulseOps 进程不重启。
- disable 插件后，新任务不可再选择该插件能力，旧 generation 进入 draining。
- rollback 能把 active version 指回旧 release，并生成新的 generation。

### 阶段 2：模板和数据源接入

- 任务创建向导消费 `task_template`。
- 数据源下拉消费 `data_source`，AI 分析继续兼容 `ai_data_source`。
- 新增内置 `grpc` 数据源 adapter，支持 Unary RPC、reflection/descriptor、TLS、metadata、deadline。
- 支持 `c_abi` AI 数据源 manifest 化。
- 支持 process/http AI 数据源 adapter。
- process/http runtime 接入 readiness check 和 worker pool draining。

验收：

- 插件新增通用数据源后，任务表单可选择。
- 配置 `protocol = "grpc"` 的数据源后，`data_process` 和 `ai_analyze` 都可以通过 alias 读取 gRPC 响应 JSON。
- 禁用插件后，新建/保存依赖该数据源的任务会校验失败。
- 升级 AI 数据源插件时，正在执行的 `ai_analyze` run 继续使用旧 generation，新 run 使用新 generation。
- 新版本 readiness 失败时，不改变当前 active version。

### 阶段 3：外部任务驱动

- 新增 process/http task driver adapter。
- 外部 driver 支持 `validate` 和 `run` 两个 action。
- 统一输出 `task.Result`。
- 加入 timeout、输出大小限制、日志截断。
- 外部 driver 的 activate、disable、rollback 必须复用 release/generation/draining 机制。

验收：

- 插件可以新增一个非内置 `kind`。
- 该 `kind` 可创建任务、validate、test-run、正式运行。
- 插件崩溃不会影响 PulseOps 主进程。
- 升级外部 driver 时，旧版本运行中的任务不被中断，新任务切到新版本。

### 阶段 4：Hook、Evaluator、Output Writer

- 引入事件总线：`run.started`、`run.finished`、`task.updated`、`plugin.loaded`。
- 支持外部 evaluator。
- 支持 AI output writer 扩展。
- 支持 trace/notification sink 扩展。

验收：

- 插件可监听 run finished 并发送通知。
- 插件 evaluator 可被 `scenario_check` 引用。

### 阶段 5：治理增强

- 插件签名和 checksum。
- 插件安装/升级审计表。
- 插件配置页面和 secret reference。
- 插件调用限流。
- 插件包导入/导出。

## 17. 关键风险

| 风险 | 处理 |
| --- | --- |
| 插件破坏主进程稳定性 | 默认 process/http 隔离；`.so` 仅兼容 AI 数据源 |
| 插件权限过大 | manifest 权限声明 + allowlist + secret reference |
| 插件表单不可维护 | 第一阶段只支持 JSON Schema/模板，不执行任意前端 JS |
| 插件 reload 影响运行中任务 | registry generation 不可变，run 启动时绑定 generation，旧 release draining 后再回收 |
| 插件更新时原地覆盖文件 | release 目录不可变，新版本必须写入新目录，active version 通过 DB CAS 切换 |
| bundled official 插件被误认为可单独热更新代码 | 文档和 UI 明确区分 `bundled_official` 与 `external_official`；需要独立发版的官方能力迁移到 external release 包 |
| C ABI `.so` 无法可靠卸载 | 仅作为兼容路径；强零停机能力使用 process/http runtime |
| 插件能力冲突 | capability id 全局唯一，冲突进入 error 状态 |
| 目录不存在导致启动失败 | 默认 warning，不阻断；strict=true 才失败 |

## 18. 推荐决策

建议按以下决策推进：

1. 插件系统第一阶段先做 catalog、manifest、API 和插件中心，不直接执行外部插件代码。
2. 第一阶段必须同时落下 release/generation/draining 状态底座，否则后续零停机更新会返工。
3. 现有内置任务先迁为 `bundled_official` 官方插件，从 catalog 和 active generation 注册。
4. 需要独立零停机更新代码的官方能力，再迁为 `external_official`。
5. 外部运行时优先支持 `process` 和 `http`，不要扩展通用 Go `.so`。
6. 任务创建向导优先接入 `task_template` 和通用 `data_source`，AI 分析复用同一套数据源能力。
7. 现有 `[ai].plugin_dir` 保留兼容，但新插件能力统一迁移到 `[plugins]`。
8. 插件加载失败默认 degraded，不阻断核心平台启动。
