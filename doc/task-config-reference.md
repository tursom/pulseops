# PulseOps 任务配置参考

## 1. 通用字段

所有任务类型（`kind`）共用 `TaskSpec` 中的顶层字段。下表列出每个字段的类型、默认值及说明：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `id` | `string` | **必填** | 任务唯一标识，不可重复 |
| `name` | `string` | `= id` | 任务显示名称；不填时自动使用 `id` 的值 |
| `kind` | `string` | **必填** | 任务类型：`http_check` / `tcp_check` / `script_exec` / `process_check` / `scenario_check` / `ai_analyze` |
| `enabled` | `bool` | `false` | 是否启用；仅在 `true` 时调度器才会启动该任务 |
| `interval` | `duration` | — | 执行间隔，Go duration 字符串格式（如 `"30s"`、`"5m"`）。与 `cron` 互斥 |
| `cron` | `string` | — | Cron 表达式（如 `"0 */6 * * *"`）。与 `interval` 互斥 |
| `timeout` | `duration` | `10s` | 单次执行超时。任务级未设置时继承全局 `task.default_timeout` |
| `trigger` | `string` | `"scheduled"` | 触发方式：`scheduled`（定时）/ `manual`（仅 API）/ `on_run`（依赖触发） |
| `watch_task` | `string` | — | 当 `trigger = "on_run"` 时监听的源任务 ID |
| `watch_condition` | `string` | — | 触发条件表达式，格式 `"field == 'value'"`，如 `"check_status == 'fail'"` |
| `labels` | `map[string]string` | `{}` | 自定义标签，用于分类、过滤和告警分组 |

### 最小 TOML 示例

```toml
id = "my-health-check"
name = "我的健康检查"
kind = "http_check"
enabled = true
interval = "30s"
timeout = "5s"

[labels]
env = "prod"
service = "gateway"
```

### 1.1 `[trace]` 留痕配置

每个任务可通过 `[trace]` 段声明执行留痕策略。所有字段来自 `TracePolicy` 结构体：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | `bool` | `false` | 是否启用留痕。当 `level`、`sinks`、`store_result_payload`、`store_stdout` 或 `store_stderr` 任一为非零值时自动置 `true` |
| `level` | `string` | 全局 `default_trace_level`（默认 `"summary"`） | 留痕级别：`none` / `summary` / `detail` / `debug` |
| `sinks` | `[]string` | 全局第一个可用 sink | 留痕存储目标名称列表，引用 `[trace.sinks.<name>]` 中定义的 sink |
| `retain_days` | `int` | `0`（不自动清理） | 保留天数 |
| `store_stdout` | `bool` | `false` | 是否记录标准输出 |
| `store_stderr` | `bool` | `false` | 是否记录标准错误 |
| `store_result_payload` | `bool` | `false` | 是否记录结果负载（`Payload`） |
| `max_payload_bytes` | `int` | `4096` | 单条结果体内联写入 PostgreSQL 的最大字节数；超出部分外存到对象存储 |
| `mask_fields` | `[]string` | `[]` | 敏感字段脱敏列表，如 `["authorization", "token", "password"]` |

```toml
[trace]
level = "detail"
sinks = ["postgres_main"]
retain_days = 30
store_stdout = false
store_stderr = false
store_result_payload = true
max_payload_bytes = 4096
mask_fields = ["authorization", "token", "password"]
```

### 1.2 `[alert]` 告警配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `consecutive_failures` | `int` | `0`（不告警） | 连续失败阈值，达到后触发告警 |
| `channels` | `[]string` | `[]` | 通知渠道名称列表 |
| `recover_notify` | `bool` | `false` | 故障恢复后是否发送恢复通知 |

```toml
[alert]
consecutive_failures = 3
channels = ["feishu"]
recover_notify = true
```

---

## 2. `http_check` — HTTP 健康检查

对指定 URL 发起 HTTP 请求，根据状态码和响应体内容判定通过/失败。

驱动注册名：`"http_check"`。参数结构：`HTTPCheckParams`。

### 参数字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `url` | `string` | **必填** | 请求目标 URL |
| `method` | `string` | `"GET"` | HTTP 方法（`GET` / `POST` / `PUT` 等） |
| `headers` | `map[string]string` | `{}` | 自定义请求头 |
| `body` | `map[string]any` | — | 请求体（非 GET 方法时可用）；自动序列化为 JSON |
| `expect_status` | `[]int` | — | 期望的 HTTP 状态码列表；不在此列表内则 `check_status = "fail"` |
| `expect_body_contains` | `string` | — | 期望响应体中包含的字符串；不存在则 `check_status = "fail"` |

### 执行逻辑

1. 构造 HTTP 请求（GET 方法时不发送 body，非 GET 方法且 body 非空时以 JSON 发送）
2. 发送请求并读取响应体
3. 若设定了 `expect_status` 且状态码不在列表中，判定 `fail`
4. 若设定了 `expect_body_contains` 且响应体中不包含该字符串，判定 `fail`
5. 其余情况判定 `pass`

### 完整 TOML 示例

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
level = "summary"
sinks = ["postgres_main"]
store_result_payload = true
max_payload_bytes = 4096

[alert]
consecutive_failures = 3
channels = ["feishu"]
recover_notify = true
```

带请求体和响应体校验的示例：

```toml
id = "steam-price-api"
name = "Steam 价格接口检查"
kind = "http_check"
enabled = true
interval = "1m"
timeout = "10s"

[params]
url = "https://partner.steam-api.com/ISteamEconomy/GetAssetPrices/v1/"
method = "POST"
expect_status = [200]
expect_body_contains = '"success":true'

[params.headers]
Authorization = "Bearer {{STEAM_API_KEY}}"

[params.body]
appid = 730
currency = "CNY"

[trace]
level = "detail"
sinks = ["postgres_main"]
store_result_payload = true
max_payload_bytes = 16384
mask_fields = ["Authorization"]
```

---

## 3. `tcp_check` — TCP 端口连通性检查

检查指定地址的 TCP 端口是否可达。

驱动注册名：`"tcp_check"`。参数结构：`TCPCheckParams`。

### 参数字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `address` | `string` | **必填** | 目标地址，格式 `host:port`（如 `"127.0.0.1:5432"`、`"redis.example.com:6379"`） |

### 执行逻辑

1. 通过 `net.DialContext` 建立 TCP 连接到 `address`
2. 连接成功 → 关闭连接，判定 `pass`
3. 连接失败 → 返回错误，任务执行失败

### 完整 TOML 示例

```toml
id = "prod-db-ping"
name = "生产数据库端口检查"
kind = "tcp_check"
enabled = true
interval = "15s"
timeout = "3s"

[labels]
env = "prod"
service = "database"

[params]
address = "10.0.1.50:5432"

[trace]
level = "summary"
sinks = ["postgres_main"]

[alert]
consecutive_failures = 3
channels = ["feishu"]
recover_notify = true
```

---

## 4. `script_exec` — 脚本执行

在平台所在主机上执行指定命令或脚本，通过退出码判定通过/失败。

驱动注册名：`"script_exec"`。参数结构：`ScriptExecParams`。

### 参数字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `command` | `string` | **必填** | 要执行的命令或脚本路径 |
| `args` | `[]string` | `[]` | 命令参数列表 |
| `workdir` | `string` | — | 工作目录；相对路径基于平台 `BaseDir` 解析 |
| `env` | `map[string]string` | `{}` | 附加环境变量（会追加到系统环境变量之后） |

### 执行逻辑

1. 通过 `exec.CommandContext` 启动子进程
2. 设置工作目录和环境变量
3. 捕获 `stdout` 和 `stderr`
4. 退出码 `0` → `check_status = "pass"`，`exit_code` 写入 Summary
5. 非零退出码 → `check_status = "fail"`，`exit_code` 写入 Summary，任务本身不报错
6. 启动失败（如命令不存在）→ 返回错误

### 完整 TOML 示例

```toml
id = "disk-usage-check"
name = "磁盘使用率检查"
kind = "script_exec"
enabled = true
interval = "5m"
timeout = "15s"

[labels]
env = "prod"
service = "host"

[params]
command = "/usr/local/bin/check_disk.sh"
args = ["--warn", "80", "--crit", "95"]
workdir = "/var/opt/checks"
env = { LOG_LEVEL = "info", RETRY_COUNT = "2" }

[trace]
level = "detail"
sinks = ["postgres_main"]
store_stdout = true
store_stderr = true

[alert]
consecutive_failures = 2
channels = ["feishu"]
recover_notify = true
```

---

## 5. `process_check` — 进程检查

检查指定名称的进程是否正在运行（通过扫描 `/proc` 文件系统）。

驱动注册名：`"process_check"`。参数结构：`ProcessCheckParams`。

### 参数字段

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `name` | `string` | **必填** | 进程名称关键字；匹配 `/proc/<pid>/cmdline` 中包含该字符串的进程 |

### 执行逻辑

1. 遍历 `/proc` 目录下的数字 PID 目录
2. 读取每个进程的 `cmdline` 文件
3. 统计 `cmdline` 中包含 `name` 的进程数量
4. `count > 0` → `check_status = "pass"`；`count == 0` → `check_status = "fail"`
5. `process_count` 写入 Summary

### 完整 TOML 示例

```toml
id = "prod-nginx-check"
name = "生产 Nginx 进程检查"
kind = "process_check"
enabled = true
interval = "30s"
timeout = "5s"

[labels]
env = "prod"
service = "nginx"

[params]
name = "nginx"

[trace]
level = "summary"
sinks = ["postgres_main"]

[alert]
consecutive_failures = 2
channels = ["feishu"]
recover_notify = true
```

---

## 6. `scenario_check` — 多步骤业务巡检

受控的多步业务巡检任务，适用于"拉列表 → 抽样 → 逐条查询详情 → 业务规则评估 → 聚合结果"的场景。

驱动注册名：`"scenario_check"`。参数结构：`scenario.Params`。

执行链路固定为：**source → sample → fanout → evaluator → aggregate**。

### 6.1 `[params.source]` — 入口数据源

定义一个 HTTP JSON 接口作为列表数据来源。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `kind` | `string` | `"http_json"` | 数据源类型（当前仅支持 `http_json`） |
| `method` | `string` | `"GET"` | HTTP 方法 |
| `url` | `string` | **必填** | 列表接口地址 |
| `headers` | `map[string]string` | `{}` | 自定义请求头 |
| `body` | `map[string]any` | — | 请求体（JSON） |
| `items_path` | `string` | `"$"` | JSON 路径，指定响应中列表数组的位置，如 `"$.data.goods"` |

### 6.2 `[params.sample]` — 抽样策略

从列表中选取子集进行巡检。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `strategy` | `string` | `"random"` | 抽样策略：`random` / `first_n` / `fixed_ids` |
| `count` | `int` | 全量 | 抽样数量 |
| `seed_mode` | `string` | 随机 | 种子模式：`"fixed"`（固定种子 → 可复现）/ `"recorded"`（每次随机并记录） |
| `seed` | `int64` | — | 固定种子值（仅在 `seed_mode = "fixed"` 时使用） |
| `fixed_ids` | `[]string` | — | 指定 ID 列表（仅在 `strategy = "fixed_ids"` 时使用）；匹配逻辑优先 `goods_id` → `id` → `sku_id` → `item_id` |

### 6.3 `[params.fanout]` — 详情并发查询

对每个抽样项并发调用详情接口。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `kind` | `string` | `"http_json"` | 查询类型（当前仅支持 `http_json`） |
| `method` | `string` | `"GET"` | HTTP 方法 |
| `url` | `string` | **必填** | 详情接口地址 |
| `headers` | `map[string]string` | `{}` | 自定义请求头 |
| `body_template` | `map[string]string` | — | 请求体模板，支持 `{{ item.field }}` 占位符 |
| `query_template` | `map[string]string` | — | URL 查询参数模板，支持 `{{ item.field }}` 占位符 |
| `concurrency` | `int` | `5` | 并发查询上限 |

### 6.4 `[params.evaluator]` — 业务评估器

指定在 evaluator 注册表中的评估器。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `name` | `string` | **必填** | evaluator 注册名（如 `"steam_game_price_consistency"`、`"ai"`） |
| `params` | `map[string]any` | — | 传递给 evaluator 的自定义参数 |

### 6.5 `[params.thresholds]` — 阈值判定

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `max_mismatch_count` | `int` | `0` | 允许的最大不一致数；超出则 `check_status = "fail"` |
| `max_error_count` | `int` | `0` | 允许的最大查询错误数；超出则 `check_status = "fail"` |

### 完整 TOML 示例（Steam 价格校验场景）

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
level = "detail"
sinks = ["postgres_main"]
store_result_payload = true
max_payload_bytes = 65536

[alert]
consecutive_failures = 1
channels = ["feishu"]
recover_notify = true
```

---

## 7. `ai_analyze` — AI 分析任务

AI 分析任务将平台采集的遥测数据作为上下文，调用 DeepSeek / OpenAI 兼容 API，由大模型生成分析结论并写回 PulseOps 数据库。

> **完整设计和使用指南请参阅：** [`doc/ai-integration.md`](./ai-integration.md)。

驱动注册名：`"ai_analyze"`。仅在 `[ai].enabled = true` 时可用。

### 参数结构速览

`params` 段被反序列化为 `AIAnalyzeParams`：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `data_sources` | `[]DataSourceSpec` | **是** | 数据源列表，至少需一个 |
| `prompt` | `PromptSpec` | **是** | Prompt 模板，`text` 字段为模板文本 |
| `outputs` | `[]OutputSpec` | 否 | 输出写入器列表；为空时仅做 AI 调用并留痕 |
| `analysis_type` | `string` | `"diagnose"` | 分析类型：`diagnose`（实时诊断）/ `trend`（趋势分析）/ `evaluate`（业务校验，嵌入 `scenario_check` 使用） |

每个 `DataSourceSpec`：

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | `string` | 数据源名称（`run_context` / `run_history` / `previous_analysis`） |
| `config` | `map[string]any` | 数据源配置参数 |

每个 `OutputSpec`：

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | `string` | 输出写入器名称（`summary` / `findings` / `artifact`） |
| `config` | `map[string]any` | 输出写入器配置参数 |

### 触发方式

AI 分析任务支持三种触发方式，推荐与 `analysis_type` 搭配使用：

| trigger | 适用 analysis_type | 说明 |
|---------|-------------------|------|
| `"scheduled"` + `cron` | `trend` | 定期汇总历史数据生成趋势报告 |
| `"on_run"` + `watch_task` | `diagnose` | 在依赖任务（如健康检查）执行完成后自动触发分析 |
| 嵌入 `scenario_check` evaluator | `evaluate` | 在巡检任务的 evaluator 中使用 `name = "ai"`，逐项调用 AI 做一致性判断 |

### 最小 TOML 示例

```toml
id = "diagnose-steam-price"
name = "Steam 价格 AI 诊断"
kind = "ai_analyze"
enabled = true
trigger = "on_run"
watch_task = "steam-price-check"
watch_condition = "check_status == 'fail'"
timeout = "60s"

[labels]
env = "prod"
service = "steam-trade"

[params]
analysis_type = "diagnose"

[[params.data_sources]]
type = "run_context"

[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "steam-price-check"
limit = 5

[params.prompt]
text = """
你是 PulseOps 运维分析助手。请分析以下执行记录并给出诊断结论：

本次状态: {{ .DataSources.run_context.CheckStatus }}
耗时: {{ .DataSources.run_context.DurationMS }}ms

{{ if .DataSources.run_history }}
最近 {{ count .DataSources.run_history }} 次执行:
{{ table .DataSources.run_history "StartedAt" "CheckStatus" "DurationMS" }}
{{ end }}

请以 JSON 格式返回：
{"ai_diagnosis": "诊断结论"}
"""

[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_diagnosis"

[trace]
level = "debug"
sinks = ["postgres_main"]
store_result_payload = true
```

---

## 8. Trace 级别速查表

| Level | Summary | Payload | Findings | Stdout/Stderr |
|-------|---------|---------|----------|---------------|
| `none` | ✗ | ✗ | ✗ | ✗ |
| `summary` | ✓ | ✗ | ✓ | ✗ |
| `detail` | ✓ | ✓ | ✓ | 视 `store_stdout` / `store_stderr` 配置 |
| `debug` | ✓ | ✓ | ✓ | ✓ |

**各级别说明：**

- **`none`**：不记录任何执行信息。
- **`summary`**（推荐默认）：记录任务 ID、执行 ID、开始/结束时间、耗时、执行结果、错误摘要和触发类型。适合大多数生产任务。
- **`detail`**：在 `summary` 基础上增加关键输入参数快照、结构化结果摘要、样本抽样结果和差异摘要、artifact 引用。适合业务巡检类任务。
- **`debug`**：完整记录每一步的原始响应、完整输出、完整结果体。适合短期排障和 AI 分析任务。注意存储成本——此类记录会消耗较多空间。

**注意**：`Stdout` 和 `Stderr` 仅在 `script_exec` 任务中产生。`summary` 级别不会记录这两个输出；`detail` 级别下由 `store_stdout` / `store_stderr` 开关单独控制；`debug` 级别下强制记录。
