# PulseOps AI 分析子系统 — 设计与使用指南

## 1. 概述

PulseOps AI 子系统是 V2 新增的核心能力。它将平台采集的遥测数据（`RunRecord`）作为上下文，调用 DeepSeek / OpenAI 兼容 API，由大模型生成分析结论并写回 PulseOps 数据库，实现无人值守的智能运维分析闭环。

### 1.1 核心能力

AI 子系统提供三种分析模式：

| 模式 | 标识 | 触发方式 | 用途 |
|------|------|----------|------|
| 实时诊断 | `diagnose` | 依赖任务执行后自动触发 | 对任务执行失败、异常结果进行根因分析 |
| 趋势分析 | `trend` | 定时调度（cron） | 定期汇总历史数据，识别趋势和异常模式 |
| 业务校验 | `evaluate` | 嵌入 `scenario_check` 任务 | 对业务巡检的抽样数据逐项调用 AI 做一致性判断 |

### 1.2 数据流

```
                             ┌─────────────┐
                             │  Task Run    │
                             └──────┬──────┘
                                    │ produce
                                    ▼
                             ┌─────────────┐
                             │  RunRecord   │
                             └──────┬──────┘
                                    │ fetch via DataSource
                                    ▼
┌──────────┐   trigger/   ┌─────────────┐   render   ┌──────────────┐
│  Cron    │───schedule──▶│  ai_analyze  │──template─▶│  Prompt Text  │
│ Watch Task│──on_run────▶│   Driver     │           └──────┬───────┘
└──────────┘              └─────────────┘                  │
                                    ▲                      │ HTTP POST
                                    │                      ▼
                                    │              ┌──────────────┐
                                    │              │ DeepSeek API │
                                    │              │   / OpenAI    │
                                    │              └──────┬───────┘
                                    │                     │ ChatResponse
                                    │                     ▼
┌──────────┐   write   ┌─────────────┐   JSON parse   ┌──────────────┐
│ai_analyses│◀─────────┤   Output     │◀──────────────┤  AI Response  │
│   table   │          │  Writers    │               │  (JSON/text)  │
└──────────┘          └─────────────┘               └──────────────┘
```

**流程说明：**

1. **触发** — 调度器 cron 定时触发，或依赖任务完成后通过 `on_run` 触发
2. **采集** — DataSource 从数据库拉取本次执行记录、历史记录、上次分析结果
3. **渲染** — 模板引擎将采集数据注入 Prompt 模板
4. **调用** — HTTP POST 到 DeepSeek / OpenAI 兼容 API
5. **解析** — 对返回的 JSON 做 try-parse
6. **写入** — OutputWriter 将分析结果拆为 summary / findings / 原文写库
7. **留痕** — 完整的 prompt、response、token 消耗记录到 `ai_analyses` 表

---

## 2. 架构设计

AI 子系统采用分层设计，自顶向下分为配置层、驱动层、数据层、输出层和持久层。

```
┌─────────────────────────────────────────────────────────────┐
│                    Config Layer (TOML)                       │
│  ┌──────────┐  ┌──────────────────┐  ┌──────────────────┐  │
│  │[ai] 全局  │  │ ai_analyze Task  │  │ scenario_check   │  │
│  │ 配置     │  │ TOML 配置        │  │ AI Evaluator     │  │
│  └──────────┘  └───────┬──────────┘  └───────┬──────────┘  │
└─────────────────────────┼────────────────────┼──────────────┘
                          │                    │
┌─────────────────────────┼────────────────────┼──────────────┐
│                 Driver Layer                 │               │
│  ┌──────────────────────▼────────────────────▼───────────┐  │
│  │              ai_analyze Driver + Runner                │  │
│  │  · Validate(params) →  must have data_sources + prompt │  │
│  │  · Run(ctx, trigger) →  fetch → render → call → write │  │
│  └──────────────────────┬────────────────────────────────┘  │
└─────────────────────────┼───────────────────────────────────┘
                          │
┌─────────────────────────┼───────────────────────────────────┐
│                 Processing Layer                            │
│  ┌──────────────────┐   ┌──────────────┐  ┌──────────────┐ │
│  │ DataSource       │   │ Template      │  │ AI Client     │ │
│  │ Registry         │   │ Engine        │  │ (HTTP)        │ │
│  │                  │   │              │  │              │ │
│  │ · run_context    │   │ text/template │  │ POST /chat/  │ │
│  │ · run_history    │   │ + 7 内置函数  │  │ completions  │ │
│  │ · previous_analysis│  │              │  │              │ │
│  └────────┬─────────┘   └──────┬───────┘  └──────┬───────┘ │
└───────────┼────────────────────┼─────────────────┼──────────┘
            │                    │                 │
┌───────────┼────────────────────┼─────────────────┼──────────┐
│  Output Layer                  │                 │          │
│  ┌────────┐ ┌────────┐ ┌──────▼─────────────────▼───────┐  │
│  │summary │ │findings│ │       OutputWriter Registry     │  │
│  │Writer  │ │Writer  │ │                                 │  │
│  └───┬────┘ └───┬────┘ │  write → Result{Summary,        │  │
│      │          │      │              Findings}           │  │
│      │          │      └─────────────────────────────────┘  │
└──────┼──────────┼──────────────────────────────────────────┘
       │          │
┌──────▼──────────▼──────────────────────────────────────────┐
│                   Persistence Layer                         │
│  ┌──────────────────────────────┐                           │
│  │       PostgreSQL              │                           │
│  │  · ai_analyses (prompt,      │                           │
│  │    response, tokens, status)  │                           │
│  │  · runs (summary payload)    │                           │
│  │  · findings (结构化结论)      │                           │
│  └──────────────────────────────┘                           │
└─────────────────────────────────────────────────────────────┘
```

**各层职责：**

| 层 | 职责 | 核心类型 |
|----|------|----------|
| Config Layer | 解析并校验任务 TOML 配置，解出 `AIAnalyzeParams` | `config.AIConfig`, `ai.AIAnalyzeParams` |
| Driver Layer | 执行 `ai_analyze` 任务的主逻辑：取数据→渲染模板→调 API→写结果 | `ai.Driver`, `ai.runner` |
| Processing Layer | 提供可插拔的数据源、模板引擎、HTTP 客户端 | `DataSource`, `template.FuncMap`, `Client` |
| Output Layer | 将 AI 返的 JSON 文本按策略写入数据库 | `OutputWriter`, `summaryWriter`, `findingsWriter` |
| Persistence Layer | 持久化 prompt、response、token 消耗和分析结果 | `ai_analyses`, `findings`, `runs` |

---

## 3. `[ai]` 全局配置参考

`[ai]` 段位于平台主配置文件 `configs/pulseops.toml` 中，控制 AI 子系统的全局开关和默认参数。

### 3.1 Go 结构体定义

```go
// internal/config/model.go
type AIConfig struct {
    Enabled        bool     `toml:"enabled"`
    Endpoint       string   `toml:"endpoint"`
    APIKey         string   `toml:"api_key"`
    Model          string   `toml:"model"`
    DefaultTimeout Duration `toml:"default_timeout"`
    MaxTokens      int      `toml:"max_tokens"`
    Temperature    float64  `toml:"temperature"`
}
```

### 3.2 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | `bool` | `false` | AI 子系统总开关。启用后注册 `ai_analyze` 驱动和 `ai` evaluator |
| `endpoint` | `string` | `""` | DeepSeek / OpenAI 兼容 API 端点地址，必须以 `/v1` 结尾 |
| `api_key` | `string` | `""` | API 密钥（**强烈建议通过环境变量注入，不要硬编码到 TOML**） |
| `model` | `string` | `"deepseek-chat"` | 模型名称，支持 DeepSeek / OpenAI 系列 |
| `default_timeout` | `Duration` | `30s` | 单次 AI 调用的默认超时时间 |
| `max_tokens` | `int` | `4096` | 单次 AI 调用最大生成 token 数 |
| `temperature` | `float64` | `0`（未设时由机型决定） | 采样温度（0–2），越低越确定；诊断场景推荐 `0.1` |

### 3.3 支持的提供商与模型

| 提供商 | 推荐模型 | endpoint 示例 |
|--------|----------|---------------|
| DeepSeek | `deepseek-chat`（标准）、`deepseek-reasoner`（推理） | `http://127.0.0.1:8000/v1` |
| OpenAI | `gpt-4o`、`gpt-4o-mini` | `https://api.openai.com/v1` |

> **注意**：PulseOps 通过 `/chat/completions` 路径调用，要求 API 兼容 OpenAI Chat Completions 协议。

### 3.4 安全提示

`api_key` 不应直接写入仓库配置。推荐做法：

```toml
# ❌ 不安全：硬编码
api_key = "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxx"

# ✅ 推荐：启动时通过环境变量注入
# 不在 TOML 中写 api_key，而是在启动脚本中设置：
#   export PULSEOPS_AI_KEY="sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
# 然后通过配置模板或变量替换工具注入到运行时配置
```

### 3.5 完整 TOML 配置示例

```toml
# ===========================================================================
# [ai]  全局 AI 分析配置
# ===========================================================================
[ai]
# [必填] 设为 true 后，ai_analyze 任务和 ai evaluator 才可用
enabled = true

# [必填] DeepSeek / OpenAI 兼容 API 端点
# DeepSeek 示例: http://127.0.0.1:8000/v1
# OpenAI 示例:  https://api.openai.com/v1
endpoint = "http://127.0.0.1:8000/v1"

# [推荐] API 密钥，强烈建议通过环境变量注入
# 生产环境不要在此处填写真实密钥
api_key = ""

# [可选] 模型名称，默认 deepseek-chat
# DeepSeek: deepseek-chat / deepseek-reasoner
# OpenAI:  gpt-4o / gpt-4o-mini
model = "deepseek-chat"

# [可选] 默认超时，格式为 Go duration 字符串
# 若某次 prompt 很大，可考虑调大到 60s-120s
default_timeout = "30s"

# [可选] 最大生成 token 数
# 诊断推荐 2048，趋势分析推荐 4096
max_tokens = 4096

# [可选] 温度 0-2，越低输出越确定
# 诊断场景强烈建议 0.1，避免幻觉
temperature = 0.1
```

---

## 4. `ai_analyze` 任务配置

`ai_analyze` 是 PulseOps 的第六种任务类型（`kind = "ai_analyze"`），由 AI Driver 驱动。

### 4.1 任务基础字段

所有任务共享 `TaskSpec` 中的基础字段（参见 `internal/config/model.go`）：

```go
type TaskSpec struct {
    ID       string            `toml:"id"`
    Name     string            `toml:"name"`
    Kind     string            `toml:"kind"`
    Enabled  bool              `toml:"enabled"`
    Interval Duration          `toml:"interval"`
    Cron     string            `toml:"cron"`
    Timeout  Duration          `toml:"timeout"`
    Labels   map[string]string `toml:"labels"`

    // 触发依赖
    Trigger        string `toml:"trigger"`          // "scheduled" | "manual" | "on_run"
    WatchTaskID    string `toml:"watch_task"`        // trigger=on_run 时必填
    WatchCondition string `toml:"watch_condition"`   // 可选触发条件

    // 驱动专用参数（解码为 AIAnalyzeParams）
    Params map[string]any `toml:"params"`

    Trace TracePolicy `toml:"trace"`
    Alert AlertPolicy `toml:"alert"`
}
```

> 关键区别：`ai_analyze` 不使用 `Interval`（如果是 cron 调度）或使用 `Interval` / `Cron` 做定时触发；也可通过 `trigger = "on_run"` + `watch_task` 做依赖触发。详见第 10 节。

### 4.2 `AIAnalyzeParams` 结构体

`params` 段会被反序列化为以下结构（`internal/ai/models.go`）：

```go
type AIAnalyzeParams struct {
    DataSources  []DataSourceSpec `toml:"data_sources"`
    Prompt       PromptSpec       `toml:"prompt"`
    Outputs      []OutputSpec     `toml:"outputs"`
    AnalysisType string           `toml:"analysis_type"`
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `data_sources` | `[]DataSourceSpec` | 是 | 数据源列表。`Driver.Validate()` 要求至少一个 |
| `prompt` | `PromptSpec` | 是 | Prompt 模板，`text` 字段为模板文本 |
| `outputs` | `[]OutputSpec` | 否 | 输出写入器列表；为空时仅做 AI 调用并留痕 |
| `analysis_type` | `string` | 否 | 分析类型标识：`"diagnose"` / `"trend"` / `"evaluate"`；默认 `"diagnose"` |

**`DataSourceSpec`：**

```go
type DataSourceSpec struct {
    Type   string         `toml:"type"`
    Config map[string]any `toml:"config"`
}
```

**`PromptSpec`：**

```go
type PromptSpec struct {
    Text string `toml:"text"`
}
```

**`OutputSpec`：**

```go
type OutputSpec struct {
    Type   string         `toml:"type"`
    Config map[string]any `toml:"config"`
}
```

### 4.3 最小可用配置

```toml
id = "minimal-ai-task"
name = "最小 AI 任务"
kind = "ai_analyze"
enabled = true
trigger = "scheduled"
cron = "0 */12 * * *"
timeout = "60s"

[params]
analysis_type = "diagnose"

[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "some-source-task"
limit = 10

[params.prompt]
text = "分析以下执行记录：{{ json .DataSources.run_history }}"

[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_result"
```

---

## 5. 数据源参考

数据源（DataSource）是 AI 分析任务的上下文提供者。每个数据源实现统一接口，通过名称注册。

### 5.1 接口与注册表

```go
// internal/ai/datasource.go
type DataSource interface {
    Name() string
    Fetch(ctx context.Context, spec DataSourceSpec, deps FetchDeps) (any, error)
}

type FetchDeps struct {
    TriggerRun   *store.RunRecord   // trigger=on_run 时，触发本次分析的源 RunRecord
    DBRepository store.Repository   // 数据库访问
    HTTPClient   *http.Client       // HTTP 客户端（预留给自定义数据源）
}
```

注册表 `DataSourceRegistry` 在构造时自动注册 3 个内置数据源。自定义数据源可通过 `registry.Register(name, source)` 添加。

### 5.2 内置数据源一：`run_context`

获取触发本次 AI 分析任务的源 `RunRecord`。**仅在 `trigger = "on_run"` 时有效**。

| 配置字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| （无） | — | — | 该数据源不使用 `config` 字段 |

**行为**：从 `ctx.Value(CtxTriggerRun)` 中取出被依赖任务的 `RunRecord`。如果不存在（如定时触发），则返回错误。

```toml
[[params.data_sources]]
type = "run_context"
```

在模板中可通过 `{{ .DataSources.run_context }}` 访问完整的 `RunRecord` 结构体，所有字段均可直接读取：

```
当前执行状态: {{ .DataSources.run_context.CheckStatus }}
耗时: {{ .DataSources.run_context.DurationMS }}ms
摘要: {{ json .DataSources.run_context.Summary }}
```

### 5.3 内置数据源二：`run_history`

获取指定任务的历史执行记录列表。

| 配置字段 | 类型 | 默认值 | 说明 |
|----------|------|--------|------|
| `task_id` | `string` | 触发任务的 ID | 要查询历史记录的目标任务 |
| `watch_task` | `string` | — | `task_id` 的备选写法，功能相同 |
| `limit` | `int` | `20` | 返回的最大条数 |

**回退逻辑**：
1. 先取 `config.task_id`
2. 若为空，取 `config.watch_task`
3. 若仍为空且存在 `TriggerRun`，使用 `TriggerRun.TaskID`
4. 若全为空则返回错误

```toml
[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "steam-price-check"
limit = 50
```

返回的数据是 `[]store.RunRecord` 切片，可在模板中使用 `table`、`filter`、`avg` 等函数处理。

### 5.4 内置数据源三：`previous_analysis`

获取当前 AI 分析任务之前的分析记录，用于上下文衔接。

| 配置字段 | 类型 | 默认值 | 说明 |
|----------|------|--------|------|
| `task_id` | `string` | 触发任务的 ID | 查询哪个任务的 AI 分析记录 |
| `limit` | `int` | `5` | 返回的最大条数 |

```toml
[[params.data_sources]]
type = "previous_analysis"
[params.data_sources.config]
task_id = "diagnose-steam-price"
limit = 1
```

返回的数据是 `[]store.AIAnalysisRecord` 切片，其中 `.Response` 字段包含上次 AI 返回的原文。

---

## 6. 输出写入器参考

输出写入器（OutputWriter）负责将 AI 返回的 JSON 文本解析并写入数据库。每个任务可配置多个输出写入器，按顺序执行。

### 6.1 接口与注册表

```go
// internal/ai/output.go
type OutputWriter interface {
    Name() string
    Write(ctx context.Context, spec OutputSpec, deps OutputDeps, input OutputInput) (*OutputResult, error)
}

type OutputDeps struct {
    DBRepository  store.Repository
    ArtifactStore store.ArtifactStore
    HTTPClient    *http.Client
    CurrentRunID  string
    CurrentTaskID string
}

type OutputInput struct {
    RawResponse string          // AI 原始返回文本
    ParsedJSON  map[string]any  // try-parse 后的 JSON map（可能为 nil）
    RunID       string
    TaskID      string
    TokensIn    int
    TokensOut   int
    DurationMS  int64
}

type OutputResult struct {
    Findings []store.Finding
    Summary  map[string]any
}
```

**合并规则**：所有输出写入器返回的 `Findings` 和 `Summary` 会被合并到最终的任务执行结果（`task.Result`）中。Summary 的 key 会与系统自动添加的 `ai_analysis_id`、`ai_tokens_in`、`ai_tokens_out`、`ai_duration_ms` 合并。

### 6.2 内置输出写入器一：`summary`

从 AI 返回的 JSON 中提取指定字段，放入运行 `Summary`。

| 配置字段 | 类型 | 默认值 | 说明 |
|----------|------|--------|------|
| `field` | `string` | `"ai_analysis"` | 从 ParsedJSON 中提取的字段名 |

**行为**：
1. 如果 AI 返回的是 JSON，提取 `ParsedJSON[field]` 的值写入 Summary
2. 如果找不到该字段，把整个 `RawResponse` 作为该字段的值写入

```toml
[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_diagnosis"
```

若 Prompt 要求 AI 返回如下 JSON：
```json
{"ai_diagnosis": "Steam API 超时导致价格检查失败，建议增加超时时间"}
```
则该文本会以 `"ai_diagnosis"` 为 key 写入本次运行的 `Summary`。

### 6.3 内置输出写入器二：`findings`

将 AI 返回的 JSON 解析为结构化结论（`store.Finding`），写入 `findings` 表。

| 配置字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| （无） | — | — | 该写入器不使用 `config` 字段 |

**`store.Finding` 结构：**

```go
type Finding struct {
    RunID    string `json:"run_id"`
    TaskID   string `json:"task_id"`
    Reason   string `json:"reason"`
    SampleID string `json:"sample_id,omitempty"`
    Data     any    `json:"data,omitempty"`
}
```

**行为**：
- 支持单个 JSON 对象（`{"reason": "...", "sample_id": "...", "data": {...}}`）
- 支持 JSON 数组（`[{"reason": "..."}, ...]`）
- 自动补充 `run_id` 和 `task_id`（如为空）
- JSON 解析失败时静默忽略，不产生错误

```toml
[[params.outputs]]
type = "findings"
```

### 6.4 内置输出写入器三：`artifact`

占位写入器，预留用于将完整响应体存储到 S3 对象存储。当前实现为空操作（no-op）。

```toml
# 预留，当前无实际效果
[[params.outputs]]
type = "artifact"
```

---

## 7. 提示词模板语法

PulseOps 使用 Go 标准库 `text/template` 作为模板引擎，并扩展了 7 个运维分析专用的内置函数。

### 7.1 模板数据上下文

模板可以访问的数据结构：

```
.
├── DataSources
│   ├── <source_name_1>   ← 第一个数据源的返回数据
│   ├── <source_name_2>   ← 第二个数据源的返回数据
│   └── ...
```

使用时通过 `{{ .DataSources.<source_type> }}` 访问，其中 `<source_type>` 就是数据源 TOML 中的 `type` 值。

### 7.2 内置函数详解

#### `{{ json .DataSources.xxx }}`

将任意值格式化为缩进 JSON 字符串。

```
摘要数据:
{{ json .DataSources.run_context.Summary }}
```

输出示例：
```json
{
  "sample_count": 5,
  "mismatch_count": 3
}
```

#### `{{ table .DataSources.xxx "Field1" "Field2" "Field3" }}`

将结构体切片渲染为 Markdown 表格。参数为要展示的字段名列表。

```
| StartedAt | CheckStatus | DurationMS |
| --- | --- | --- |
| 2026-05-07 10:00:00 | pass | 1234 |
| 2026-05-07 09:00:00 | fail | 5678 |
```

#### `{{ len .DataSources.xxx }}`

返回切片、数组、映射或字符串的长度。

```
总执行次数: {{ len .DataSources.run_history }}
```

#### `{{ avg .DataSources.xxx "FieldName" }}`

计算切片中指定数值字段的平均值。支持 `int`、`int64`、`float64` 类型。

```
平均耗时: {{ avg .DataSources.run_history "DurationMS" }}ms
```

#### `{{ count .DataSources.xxx }}`

`len` 的别名，语义上更适合计数场景。

```
{{ count .DataSources.run_history }} 次执行
```

#### `{{ filter .DataSources.xxx "FieldName" "value" }}`

从切片中过滤出指定字段等于指定值的元素，返回子切片。

```
{{ filter .DataSources.run_history "CheckStatus" "fail" }}
```

常见用法：

```
成功次数: {{ len (filter .DataSources.run_history "RunStatus" "success") }}
失败次数: {{ len (filter .DataSources.run_history "RunStatus" "failed") }}
```

#### `{{ failures .DataSources.xxx }}`

便捷过滤函数，返回 `RunStatus == "failed"` 或 `CheckStatus == "fail"` 的元素。

```
{{ $failures := failures .DataSources.run_history }}
{{ if $failures }}
{{ table $failures "StartedAt" "CheckStatus" "DurationMS" }}
{{ end }}
```

### 7.3 完整 Prompt 模板示例

```toml
[params.prompt]
text = """
你是 PulseOps 运维分析助手。以下是 Steam 价格检查任务的执行情况，请进行诊断。

【本次执行】
状态: {{ .DataSources.run_context.CheckStatus }}
耗时: {{ .DataSources.run_context.DurationMS }}ms
触发类型: {{ .DataSources.run_context.TriggerType }}
{{ if .DataSources.run_context.ErrorMessage }}
错误信息: {{ .DataSources.run_context.ErrorMessage }}
{{ end }}
摘要数据:
{{ json .DataSources.run_context.Summary }}

{{ if .DataSources.run_history }}
【最近 {{ len .DataSources.run_history }} 次执行记录】
{{ table .DataSources.run_history "StartedAt" "CheckStatus" "RunStatus" "DurationMS" }}

成功率: {{ len (filter .DataSources.run_history "RunStatus" "success") }}/{{ len .DataSources.run_history }}
平均耗时: {{ printf "%.0f" (avg .DataSources.run_history "DurationMS") }}ms
{{ end }}

{{ if .DataSources.previous_analysis }}
【上次分析结果】
{{ range .DataSources.previous_analysis }}
{{ .Response }}
{{ end }}
{{ end }}

请用中文回答以下问题，以 JSON 格式返回：
{
  "status": "normal|warning|abnormal",
  "root_cause": "根因分析（一句话）",
  "suggestions": ["建议1", "建议2"]
}
"""
```

### 7.4 注意事项

- **缺失字段静默处理**：模板中引用了不存在的 key 时，Go text/template 会输出 `<no value>` 或空值。为避免干扰 AI 判断，建议用 `{{ if .DataSources.xxx }}...{{ end }}` 包裹可选数据块
- **模板语法严格**：所有 `{{ }}` 内是 Go template 表达式，语法错误会导致任务运行报错
- **换行控制**：`{{- ... }}` 会吞掉左侧空白，`{{ ... -}}` 会吞掉右侧空白。多行模板中注意换行符的管理
- **Prompt 长度**：注意 `max_tokens` 限制——prompt + response 的总 token 数不应超过模型的上下文窗口

---

## 8. 三种分析类型

### 8.1 实时诊断（diagnose）

适用于：**任务执行失败/异常后，自动进行根因分析**。

**触发方式**：`trigger = "on_run"`，依赖一个生产任务。

**典型数据源组合**：
- `run_context`：获取本次失败的执行详情
- `run_history`：获取最近 N 次历史记录作为上下文
- `previous_analysis`：获取上次诊断结论，对比是否复现

**典型输出**：`summary`（写入诊断结论到本次运行摘要）

**配置示例**（完整版见附录 A）：

```toml
id = "diagnose-steam-price"
name = "Steam 价格检查 AI 诊断"
kind = "ai_analyze"
enabled = true
trigger = "on_run"
watch_task = "steam-price-check"
watch_condition = "check_status == 'fail'"
timeout = "60s"
labels = { env = "prod", service = "steam-trade" }

[params]
analysis_type = "diagnose"

[[params.data_sources]]
type = "run_context"

[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "steam-price-check"
limit = 5

[[params.data_sources]]
type = "previous_analysis"
[params.data_sources.config]
task_id = "diagnose-steam-price"
limit = 1

[params.prompt]
text = """
...诊断 Prompt 模板...
"""

[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_diagnosis"
```

**关键配置说明**：
- `watch_condition = "check_status == 'fail'"` — 仅在源任务判定为 `fail` 时才触发 AI 诊断，避免对正常运行的每次执行都调用 AI
- `watch_task` — 指向被监控的生产任务 ID

### 8.2 趋势分析（trend）

适用于：**定期汇总大量历史数据，检测趋势、周期性异常、退化模式**。

**触发方式**：`trigger = "scheduled"`，通常搭配 `cron`。

**典型数据源组合**：
- `run_history`（large limit，如 100–500）：提供足量历史数据
- `previous_analysis`：对比上次趋势结论

**典型输出**：`summary` + `findings`（结构化异常发现）

**配置示例**（完整版见附录 B）：

```toml
id = "trend-steam-price"
name = "Steam 价格趋势分析"
kind = "ai_analyze"
enabled = true
trigger = "scheduled"
cron = "0 */6 * * *"
timeout = "60s"

[params]
analysis_type = "trend"

[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "steam-price-check"
limit = 100

[[params.data_sources]]
type = "previous_analysis"
[params.data_sources.config]
task_id = "trend-steam-price"
limit = 1

[params.prompt]
text = """
...趋势分析 Prompt 模板...
"""

[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_trend"

[[params.outputs]]
type = "findings"
```

**cron 建议**：
- 高频任务：每 1–2 小时分析一次
- 中频任务：每 6–12 小时分析一次
- 低频任务：每天一次即可

### 8.3 业务校验（evaluate）

适用于：**在 `scenario_check` 巡检任务中，对抽样数据逐项调用 AI 判断是否一致**。

与 `diagnose` / `trend` 不同，evaluate 不是独立的 `ai_analyze` 任务，而是嵌入到现有的 `scenario_check` 配置中，作为 evaluator 使用。

**配置示例**：

```toml
# scenario_check 任务中启用 AI evaluator
id = "buyer-goods-ai-audit"
name = "买家游戏 AI 一致性校验"
kind = "scenario_check"
enabled = true
interval = "10m"
timeout = "60s"

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
name = "ai"
[params.evaluator.params]
prompt = """
你是业务数据一致性审核员。
请对比以下商品信息，判断是否存在不一致：

商品 ID: {{ .goods_id }}
名称: {{ .goods_name }}
价格: {{ .price }}
详情接口返回:
{{ .Detail }}

请以 JSON 格式返回：
{"status": "normal|abnormal", "reason": "判定原因"}
"""
max_samples = 5
only_on_mismatch = true

[params.thresholds]
max_mismatch_count = 0
max_error_count = 0
```

**关键配置说明**：
- `name = "ai"` — 使用注册名为 `"ai"` 的 `AIEvaluator`
- `prompt` — 使用 `{{ .field }}` 占位符，运行时会替换为样本数据
- `max_samples` — 最多对几个样本调用 AI（默认 5，控制成本）
- `only_on_mismatch` — 仅对已经有 fanout 错误的样本做 AI 校验（跳过正常的样本）

> **注意**：`[params.evaluator]` 段（单数）在 `scenario_check` 中只能指定一个 evaluator。如需同时使用规则校验和 AI 校验，需视业务需求拆分为两个独立任务，或使用复合 evaluator。

---

## 9. AI Evaluator 集成

`AIEvaluator` 是 `ScenarioEvaluator` 接口的 AI 实现，与 `steam_game_price_consistency` 等规则 evaluator 并列共存。

### 9.1 注册机制

```go
// internal/app/app.go
if cfg.AI.Enabled {
    // ...
    if err := evaluators.Register(&ai.AIEvaluator{Client: aiClient}); err != nil {
        return nil, err
    }
}
```

- `AIEvaluator` 仅在 `[ai].enabled = true` 时注册
- 注册名为 `"ai"`
- 可以与 `steam_game_price_consistency` 等规则型 evaluator 共存于同一个 `Registry`

### 9.2 接口实现

```go
// internal/ai/evaluator.go
type AIEvaluator struct {
    Client *Client
}

func (e *AIEvaluator) Name() string { return "ai" }

func (e *AIEvaluator) Evaluate(ctx context.Context, input evaluator.Input) (evaluator.Result, error)
```

### 9.3 评估流程

```
Scenario Executor → sampler → fanout → FanoutItems
                                          │
                    ┌─────────────────────┘
                    ▼
            ┌──────────────┐
            │  AIEvaluator  │
            │  .Evaluate()  │
            └──────┬───────┘
                   │ per item (up to max_samples)
                   ▼
            ┌──────────────┐
            │ buildItemPrompt│  ← 替换 {{ .field }} 占位符
            └──────┬───────┘
                   ▼
            ┌──────────────┐
            │ Client.Chat() │  ← 调用 DeepSeek / OpenAI
            └──────┬───────┘
                   ▼
            ┌──────────────┐
            │tryParseFinding│  ← 解析 JSON 返回
            │   JSON()      │
            └──────┬───────┘
                   │ accumulate
                   ▼
            ┌──────────────┐
            │  Result {     │
            │   CheckStatus │
            │   Summary     │
            │   Findings    │
            │  }            │
            └──────────────┘
```

**逐项处理逻辑**（`evaluateItem`）：

1. 从 `evaluator.Input.FanoutItems` 中依次取出每个 item
2. 最多处理 `max_samples` 个（默认 5）
3. 若 `only_on_mismatch = true`：
   - 跳过 `fi.Error == ""` 的正常 item
   - 只对有 fanout 错误的 item 做 AI 判断
4. 若 `only_on_mismatch = false`：
   - 跳过 `fi.Error != ""` 的错误 item（累加 `errorCount`）
   - 只对正常的 item 做 AI 判断

### 9.4 占位符替换

`buildItemPrompt` 函数将模板中的 `{{ .FieldName }}` 替换为实际数据：

| 占位符 | 替换为 | 说明 |
|--------|--------|------|
| `{{ .field }}` | `item.Item["field"]` 的值 | FanoutItem 的源数据 |
| `{{ .Detail }}` | `json.Marshal(item.Detail)` | 详情接口返回的完整 JSON |
| `{{ .Detail.field }}` | `item.Detail["field"]` 的值 | 详情返回的指定字段 |

**示例**：

```go
// Prompt 模板:
"商品 ID: {{ .goods_id }}, 价格: {{ .price }}
详情: {{ .Detail }}"

// 替换后:
"商品 ID: 12345, 价格: 99.99
详情: {\"goods_price\": 89.99, \"dlc_list\": [...]}"
```

### 9.5 状态判定

```go
// AIEvaluator 的最终判定逻辑
checkStatus := "pass"
if checked == 0 || mismatchCount > 0 || (onlyOnMismatch && errorCount > 0) {
    checkStatus = "fail"
}
```

| 条件 | 判定 |
|------|------|
| 至少一个 item 被 AI 判定为 `"abnormal"` 或 `"warning"` | `fail` |
| 所有被检查的 item 都是 `"normal"` | `pass` |
| `checked == 0`（没有 item 被处理） | `fail` |
| `only_on_mismatch` 模式下有 error item | `fail` |

AI 返回的 `{"status": "abnormal"}` 会被统计到 `mismatchCount`；`{"status": "normal"}` 不计入。

### 9.6 与规则 Evaluator 的对比

| 维度 | Rule Evaluator（如 steam_price） | AI Evaluator |
|------|----------------------------------|--------------|
| 判定方式 | Go 代码硬编码匹配规则 | 提示词驱动，由大模型判断 |
| 一致性 | 百分百确定 | 可能存在幻觉，建议 temperature ≤ 0.1 |
| 灵活性 | 改规则需改代码 + 重编译 | 改提示词即可，热重载生效 |
| 成本 | 零 | 每次检查按 token 计费 |
| 适用场景 | 明确的字段等价匹配 | 语义模糊的"看起来是否合理" |

---

## 10. 触发系统

PulseOps 为 `ai_analyze` 任务提供了三种触发方式和任务依赖机制。

### 10.1 触发方式

| 触发方式 | `trigger` 值 | 调度字段 | 说明 |
|----------|-------------|----------|------|
| 定时调度 | `"scheduled"` | `interval` 或 `cron` | 标准周期调度 |
| 手动触发 | `"manual"` | 无 | 仅通过 API `POST /tasks/{id}/run` 触发 |
| 依赖触发 | `"on_run"` | `watch_task` + `watch_condition` | 依赖任务完成后自动触发 |

### 10.2 定时调度

```toml
# cron 方式（每分钟）
trigger = "scheduled"
cron = "* * * * *"

# interval 方式（每 30 秒）
trigger = "scheduled"
interval = "30s"
```

> `cron` 和 `interval` 互斥，同时设置会校验失败。

### 10.3 依赖触发（`trigger = "on_run"`）

依赖触发是 AI 诊断任务最常见的用法：当源任务（如健康检查、业务巡检）执行完毕后，自动触发 AI 分析。

**必需字段**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `trigger` | `string` | 必须为 `"on_run"` |
| `watch_task` | `string` | 要监听的源任务 ID |

**可选字段**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `watch_condition` | `string` | 触发条件，格式 `"field == 'value'"` |

### 10.4 条件匹配

`watch_condition` 使用简单的 `field == 'value'` 格式：

```toml
# 仅在源任务判定为 fail 时才触发 AI 诊断
watch_condition = "check_status == 'fail'"
```

匹配逻辑：
```go
func matchCondition(condition string, record store.RunRecord) bool {
    // 解析 "field == 'value'" 格式
    // 通过反射得到 record.Field 的值，与 value 比较
    // 如果格式不对或字段不存在，默认返回 true（触发）
}
```

当前支持的字段包括 `RunRecord` 的任意可比较字段，如 `RunStatus`、`CheckStatus`、`TaskKind` 等。

### 10.5 依赖链执行流程

```
源任务执行 → 生成 RunRecord → triggerDepTasks(sourceTaskID, record)
                                       │
                    ┌───────────────────┘
                    ▼
            遍历所有 depTasks[sourceTaskID]
                    │
              ┌─────┴─────┐
              ▼           ▼
        matchCondition?  matchCondition?
         ✅ 触发         ❌ 跳过
              │
              ▼
     context.WithValue(CtxTriggerRun, sourceRecord)
              │
              ▼
     ai_analyze.Run(ctx, TriggerDependent)
```

**传递机制**：
- 源任务的 `RunRecord` 通过 `context.Value(ai.CtxTriggerRun)` 传递给依赖任务
- `run_context` 数据源从 context 中取 `TriggerRun`
- `run_history` 数据源也可从 `TriggerRun.TaskID` 获取默认查询目标

### 10.6 依赖链限制

> ⚠️ **循环依赖不会自动检测**。请手动确保不形成 A → B → A 的依赖环。平台当前不做拓扑排序或环检测。

合理的依赖设计：

```
steam-price-check (scenario_check)
    └── diagnose-steam-price (ai_analyze, on_run, watch_condition: fail)

api-health-check (http_check)
    └── diagnose-api-health (ai_analyze, on_run)
```

---

## 11. 成本与频率控制

### 11.1 通过条件控制调用频率

**最重要的一条实践：搭配 `watch_condition` 降频**。

```toml
# ❌ 每次源任务执行都调 AI，成本失控
trigger = "on_run"

# ✅ 仅在源任务失败时才调 AI
trigger = "on_run"
watch_condition = "check_status == 'fail'"

# ✅ 仅在源任务超时时才调 AI
trigger = "on_run"
watch_condition = "run_status == 'timeout'"
```

### 11.2 Evaluator 的 max_samples

`AIEvaluator` 对每个 item 调用一次 AI，成本随样本数线性增长：

```toml
[params.evaluator.params]
max_samples = 5   # 严格控制，默认值即 5
```

结合 `only_on_mismatch` 可进一步降低调用量：

```toml
only_on_mismatch = true   # 仅对有 fanout 错误的样本调 AI
```

### 11.3 温度建议

| 分析类型 | 推荐 temperature | 原因 |
|----------|-----------------|------|
| diagnose | `0.1` | 诊断需要确定性结论，避免幻觉 |
| trend | `0.1`–`0.3` | 数据驱动，建议保持低温度 |
| evaluate | `0`–`0.1` | 业务校验需要精确判断 |

### 11.4 建议的 max_tokens

| 分析类型 | 推荐 max_tokens | 说明 |
|----------|-----------------|------|
| diagnose | `2048` | 诊断结论通常简短 |
| trend | `4096` | 趋势分析可能产生较长报告 |
| evaluate | `512`–`1024` | 单项判断非常简洁 |

### 11.5 Token 消耗追踪

每次 AI 调用都会记录 `tokens_in`（输入 token）和 `tokens_out`（输出 token）到 `ai_analyses` 表：

```sql
SELECT
    task_id,
    analysis_type,
    SUM(tokens_in)  AS total_prompt_tokens,
    SUM(tokens_out) AS total_completion_tokens,
    COUNT(*)        AS call_count
FROM ai_analyses
WHERE created_at > NOW() - INTERVAL '7 days'
GROUP BY task_id, analysis_type
ORDER BY total_prompt_tokens DESC;
```

### 11.6 成本估算

假设使用 DeepSeek-Chat（约 ¥1 / 百万 token）：

| 场景 | 每次 prompt tokens | 每次 completion tokens | 每次成本 | 月调用量（估算） | 月成本 |
|------|-------------------|----------------------|----------|-----------------|--------|
| 诊断（仅失败时触发） | ~2000 | ~500 | ¥0.0025 | 100 | ¥0.25 |
| 趋势（每 6 小时） | ~8000 | ~2000 | ¥0.01 | 120 | ¥1.20 |
| 业务校验（每 10 分钟，5 样本） | ~5000 | ~1000 | ¥0.006 | 4320 | ¥25.92 |

> 实际成本取决于 prompt 长度、历史数据量，以及 `max_tokens` 设置。

---

## 12. 持久化与留痕

### 12.1 `ai_analyses` 表

每次 AI 调用都会在 `ai_analyses` 表中留下完整记录：

```sql
CREATE TABLE IF NOT EXISTS ai_analyses (
    id           BIGSERIAL PRIMARY KEY,
    run_id       TEXT NOT NULL,              -- 分析任务本次运行的唯一 ID
    task_id      TEXT NOT NULL,              -- AI 分析任务 ID
    analysis_type TEXT NOT NULL DEFAULT 'general', -- diagnose | trend | evaluate
    model        TEXT NOT NULL,              -- 使用的模型名称
    prompt       TEXT NOT NULL,              -- 渲染后的完整 Prompt
    response     TEXT NOT NULL,              -- AI 原始返回文本
    tokens_in    INTEGER NOT NULL DEFAULT 0, -- 输入 token 数
    tokens_out   INTEGER NOT NULL DEFAULT 0, -- 输出 token 数
    duration_ms  BIGINT NOT NULL DEFAULT 0,  -- 调用耗时（毫秒）
    status       TEXT NOT NULL DEFAULT 'success', -- success | error
    error_message TEXT NOT NULL DEFAULT '',  -- 错误信息
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_analyses_run
    ON ai_analyses(run_id);

CREATE INDEX IF NOT EXISTS idx_ai_analyses_task
    ON ai_analyses(task_id, analysis_type, created_at DESC);
```

### 12.2 Go 结构体

```go
// internal/store/postgres.go
type AIAnalysisRecord struct {
    ID           int64     `json:"id"`
    RunID        string    `json:"run_id"`
    TaskID       string    `json:"task_id"`
    AnalysisType string    `json:"analysis_type"`
    Model        string    `json:"model"`
    Prompt       string    `json:"prompt"`
    Response     string    `json:"response"`
    TokensIn     int       `json:"tokens_in"`
    TokensOut    int       `json:"tokens_out"`
    DurationMS   int64     `json:"duration_ms"`
    Status       string    `json:"status"`
    ErrorMessage string    `json:"error_message,omitempty"`
    CreatedAt    time.Time `json:"created_at"`
}
```

### 12.3 写入流程

```go
// 正常流程
record.Status    = "success"
record.Response  = resp.Content
record.TokensIn  = resp.TokensIn
record.TokensOut = resp.TokensOut
store.InsertAIAnalysis(ctx, record)

// 异常流程
record.Status       = "error"
record.ErrorMessage = err.Error()
store.InsertAIAnalysis(ctx, record)   // 仍然写入，http 调用失败也留痕
```

### 12.4 API 查询接口

PulseOps 提供两个 AI 分析查询接口：

**接口一：按 runID 查询单次分析**

```
GET /tasks/{id}/runs/{runID}/ai
```

响应示例：
```json
{
  "id": 142,
  "run_id": "diagnose-steam-price-1715089200",
  "task_id": "diagnose-steam-price",
  "analysis_type": "diagnose",
  "model": "deepseek-chat",
  "prompt": "你是 PulseOps 运维分析助手...",
  "response": "{\"status\": \"abnormal\", \"root_cause\": \"...\"}",
  "tokens_in": 2340,
  "tokens_out": 156,
  "duration_ms": 1234,
  "status": "success",
  "error_message": "",
  "created_at": "2026-05-07T10:20:00Z"
}
```

**接口二：按 taskID 查询历史分析列表**

```
GET /tasks/{id}/ai?limit=20
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `limit` | `int` | `20` | 返回的最大条数，按 `created_at DESC` 排序 |

响应为 `[]AIAnalysisRecord` JSON 数组。

### 12.5 Repository 接口

```go
type Repository interface {
    // ...
    InsertAIAnalysis(ctx context.Context, record AIAnalysisRecord) error
    GetAIAnalysis(ctx context.Context, runID string) (*AIAnalysisRecord, error)
    ListAIAnalyses(ctx context.Context, taskID string, limit int) ([]AIAnalysisRecord, error)
}
```

### 12.6 留痕策略建议

| 场景 | 推荐 trace.level | 原因 |
|------|-----------------|------|
| 生产诊断任务 | `debug` | 需要完整 prompt 和 response 用于事后复盘 |
| 生产趋势任务 | `detail` | prompt 可能很大但 response 是关键 |
| 开发调试 | `debug` | 需要看到渲染后的模板和错误信息 |

---

## 附录 A：完整诊断任务配置

以下为 `diagnose-example.toml` 的完整内容及逐节注释：

```toml
# ===========================================================================
# AI 诊断任务示例
# 当 steam-price-check 任务判定为 fail 时自动触发 AI 诊断
# ===========================================================================
# 使用方式:
#   1. 确保 [ai] enabled = true
#   2. 修改 watch_task 指向你的真实监控任务
#   3. 调整 watch_condition 匹配逻辑
#   4. 自定义 prompt 文本以适应你的领域
# ===========================================================================

# ── 任务标识 ──
id = "diagnose-steam-price"         # 任务唯一 ID，不可重复
name = "Steam 价格检查 AI 诊断"      # 任务显示名称
kind = "ai_analyze"                  # 驱动类型，固定为 ai_analyze

# ── 状态与触发 ──
enabled = false                      # 设为 true 启用
trigger = "on_run"                   # 依赖触发：源任务执行后触发
watch_task = "steam-price-check"     # 监听的源任务 ID
watch_condition = "check_status == 'fail'"  # 仅源任务失败时触发，控制成本

# ── 执行参数 ──
timeout = "60s"                      # 单次 AI 分析超时
labels = { env = "prod", service = "steam-trade" }  # 标签，用于分类和过滤

# ═══════════════════════════════════════════════════════════════════════════
# [params] AI 分析专用参数
# ═══════════════════════════════════════════════════════════════════════════
[params]
analysis_type = "diagnose"           # 分析类型标识

# ── 数据源 1: 本次执行详情 ──
# run_context 只能在 trigger=on_run 时使用
# 提供被触发任务的完整 RunRecord (CheckStatus, DurationMS, Summary, Findings, etc.)
[[params.data_sources]]
type = "run_context"

# ── 数据源 2: 历史执行记录 ──
# 提供指定任务最近 N 次执行，帮助 AI 判断是一次性故障还是持续恶化
[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "steam-price-check"       # 查询哪个任务的历史
limit = 5                           # 取最近 5 条

# ── 数据源 3: 上次 AI 诊断结果 ──
# 提供上一次诊断结论，帮助 AI 做趋势对比
[[params.data_sources]]
type = "previous_analysis"
[params.data_sources.config]
task_id = "diagnose-steam-price"    # 查询自身的历史分析
limit = 1                           # 只取最近一次

# ── Prompt 模板 ──
# 使用 Go text/template 语法，通过 {{ .DataSources.xxx }} 注入数据
[params.prompt]
text = """
你是 PulseOps 运维分析助手。以下是一次 Steam 价格检查的执行记录，请进行诊断分析。

【本次执行】
状态: {{ .DataSources.run_context.CheckStatus }}
耗时: {{ .DataSources.run_context.DurationMS }}ms
触发类型: {{ .DataSources.run_context.TriggerType }}
{{ if .DataSources.run_context.ErrorMessage }}错误: {{ .DataSources.run_context.ErrorMessage }}{{ end }}
{{ if .DataSources.run_context.Summary }}
摘要数据:
{{ json .DataSources.run_context.Summary }}
{{ end }}
{{ if .DataSources.run_context.Findings }}
异常发现:
{{ json .DataSources.run_context.Findings }}
{{ end }}

{{ if .DataSources.run_history }}
【最近 5 次执行记录】
{{ table .DataSources.run_history "StartedAt" "CheckStatus" "RunStatus" "DurationMS" }}
{{ $failures := filter .DataSources.run_history "CheckStatus" "fail" }}
近 5 次中失败 {{ len $failures }} 次
平均耗时: {{ avg .DataSources.run_history "DurationMS" }}ms
{{ end }}

{{ if .DataSources.previous_analysis }}
【上次诊断结果】
{{ range .DataSources.previous_analysis }}{{ .Response }}{{ end }}
{{ end }}

请用中文回答：
1. 问题判定（正常/警告/异常）
2. 根因分析（一句话）
3. 建议操作（1-2条具体建议）
"""

# ── 输出写入器: summary ──
# 将 AI 返回的 JSON 中 ai_diagnosis 字段提取到本次运行摘要
[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_diagnosis"

# ═══════════════════════════════════════════════════════════════════════════
# [trace] 留痕配置
# ═══════════════════════════════════════════════════════════════════════════
[trace]
level = "debug"                      # 诊断任务建议 debug 级别
sinks = ["postgres_main"]            # 写入 PostgreSQL
store_stdout = true                  # 保留标准输出
```

---

## 附录 B：完整趋势分析任务配置

以下为 `trend-example.toml` 的完整内容及逐节注释：

```toml
# ===========================================================================
# AI 趋势分析任务示例
# 每 6 小时汇总 Steam 价格检查任务的执行趋势
# ===========================================================================
# 使用方式:
#   1. 确保 [ai] enabled = true
#   2. 修改 [data_sources.config].task_id 指向你的真实任务
#   3. 调整 cron 表达式以匹配你的分析频率
#   4. 根据需要调整 limit（历史记录条数）
# ===========================================================================

# ── 任务标识 ──
id = "trend-steam-price"             # 任务唯一 ID
name = "Steam 价格趋势分析"           # 任务显示名称
kind = "ai_analyze"                  # 驱动类型，固定为 ai_analyze

# ── 状态与触发 ──
enabled = false                      # 设为 true 启用
trigger = "scheduled"                # 定时触发
cron = "0 */6 * * *"                # cron 表达式：每 6 小时执行一次

# ── 执行参数 ──
timeout = "60s"                      # 单次分析超时
labels = { env = "prod", service = "steam-trade" }

# ═══════════════════════════════════════════════════════════════════════════
# [params] AI 分析参数
# ═══════════════════════════════════════════════════════════════════════════
[params]
analysis_type = "trend"              # 趋势分析类型

# ── 数据源 1: 历史执行记录（大量）──
# limit 设为 100 可提供充足的统计样本
[[params.data_sources]]
type = "run_history"
[params.data_sources.config]
task_id = "steam-price-check"       # 分析哪个任务
limit = 100                         # 取最近 100 条记录

# ── 数据源 2: 上次趋势分析结果 ──
# 让 AI 对比上次的趋势结论，判断改善还是恶化
[[params.data_sources]]
type = "previous_analysis"
[params.data_sources.config]
task_id = "trend-steam-price"       # 查询自身的分析记录
limit = 1

# ── Prompt 模板 ──
# 丰富的统计函数：len, avg, filter, failures, table
[params.prompt]
text = """
你是 PulseOps 数据趋势分析师。以下是 Steam 价格检查任务最近100次执行的汇总数据，请分析趋势。

【执行概况】
总执行次数: {{ len .DataSources.run_history }}
成功次数: {{ len (filter .DataSources.run_history "RunStatus" "success") }}
失败次数: {{ len (filter .DataSources.run_history "RunStatus" "failed") }}
平均耗时: {{ avg .DataSources.run_history "DurationMS" }}ms

【按检查状态分组】
通过次数: {{ len (filter .DataSources.run_history "CheckStatus" "pass") }}
失败次数: {{ len (filter .DataSources.run_history "CheckStatus" "fail") }}

【失败记录明细】
{{ $failures := failures .DataSources.run_history }}
{{ if $failures }}
{{ table $failures "StartedAt" "CheckStatus" "DurationMS" }}
{{ end }}

{{ if .DataSources.previous_analysis }}
【上次趋势分析】
{{ range .DataSources.previous_analysis }}{{ .Response }}{{ end }}
{{ end }}

请用中文回答：
1. 趋势判断（改善/稳定/恶化）
2. 关键指标变化（失败率、平均耗时）
3. 异常模式识别
4. 关注建议
"""

# ── 输出写入器 1: summary ──
[[params.outputs]]
type = "summary"
[params.outputs.config]
field = "ai_trend"                  # 趋势结论 key

# ── 输出写入器 2: findings ──
# AI 返回的 JSON 数组会被解析为 store.Finding 写入数据库
[[params.outputs]]
type = "findings"

# ═══════════════════════════════════════════════════════════════════════════
# [trace] 留痕配置
# ═══════════════════════════════════════════════════════════════════════════
[trace]
level = "debug"
sinks = ["postgres_main"]
store_stdout = true
```

---

## 附录 C：`[ai]` 配置参数速查表

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | `bool` | `false` | AI 总开关；启用后 `ai_analyze` 驱动和 `ai` evaluator 才可用 |
| `endpoint` | `string` | `""` | API 端点，结尾需 `/v1`（如 `http://127.0.0.1:8000/v1`） |
| `api_key` | `string` | `""` | API 密钥；强烈建议环境变量注入，勿硬编码 |
| `model` | `string` | `"deepseek-chat"` | 模型名称；DeepSeek: `deepseek-chat` / `deepseek-reasoner`；OpenAI: `gpt-4o` / `gpt-4o-mini` |
| `default_timeout` | `Duration` | `"30s"` | 单次调用超时；Go duration 格式，如 `"60s"` / `"2m"` |
| `max_tokens` | `int` | `4096` | 最大生成 token 数；诊断推荐 2048，趋势推荐 4096 |
| `temperature` | `float64` | `0`（未设时） | 采样温度 0-2；诊断推荐 0.1，校验推荐 0 |

### 数据源速查

| 数据源 type | 用途 | 关键 config | 仅在 trigger=on_run |
|-------------|------|-------------|---------------------|
| `run_context` | 本次触发 RunRecord | 无 | ✅ 是 |
| `run_history` | 历史执行记录列表 | `task_id`, `limit`（默认 20） | 否 |
| `previous_analysis` | 上次 AI 分析记录 | `task_id`, `limit`（默认 5） | 否 |

### 输出写入器速查

| 输出 type | 用途 | 关键 config | 写入目标 |
|-----------|------|-------------|----------|
| `summary` | 提取 JSON 字段到运行摘要 | `field`（默认 `"ai_analysis"`） | `runs.summary` JSONB |
| `findings` | 解析为结构化结论 | 无 | `findings` 表 |
| `artifact` | 占位（S3 外存） | 无 | 暂无 |

### 分析类型速查

| analysis_type | 典型触发 | 数据源组合 | 输出组合 |
|---------------|----------|-----------|----------|
| `diagnose` | `on_run` + `watch_condition` | run_context + run_history + previous_analysis | summary |
| `trend` | `scheduled` + `cron` | run_history（大 limit） + previous_analysis | summary + findings |
| `evaluate` | 嵌入 `scenario_check` | —（使用 FanoutItems） | evaluator.Result |

### 模板函数速查

| 函数 | 语法 | 说明 |
|------|------|------|
| `json` | `{{ json .DataSources.xxx }}` | 格式化为缩进 JSON |
| `table` | `{{ table .DataSources.xxx "F1" "F2" }}` | 渲染 Markdown 表格 |
| `len` | `{{ len .DataSources.xxx }}` | 返回切片/字符串长度 |
| `avg` | `{{ avg .DataSources.xxx "Field" }}` | 计算数值字段平均值 |
| `count` | `{{ count .DataSources.xxx }}` | `len` 的别名 |
| `filter` | `{{ filter .DataSources.xxx "F" "v" }}` | 按字段值过滤 |
| `failures` | `{{ failures .DataSources.xxx }}` | 过滤失败/异常记录 |

