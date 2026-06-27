# PulseOps 创建任务向导设计文档

## 1. 背景

当前创建任务页已经支持任务模板、基础信息、调度、任务参数、标签、追踪、告警、JSON 预览、后端校验和试运行，但页面仍是长表单形态。不同任务类型需要配置的字段差异很大，所有分区同时出现会造成两个问题：

- 用户需要在一个页面里理解所有底层字段，认知负担高。
- 关键字段容易漏填，尤其是依赖触发、数据处理、AI 分析这类需要上游数据源的任务。

本设计聚焦创建任务体验，不改变工作台、任务监控、任务详情和依赖拓扑的主流程。编辑任务可以复用同一套向导结构，但允许直接进入已有任务对应步骤。

## 2. 目标

1. 创建任务必须是分步向导，不再是完整长表单。
2. 不同任务类型只展示该类型需要的配置项。
3. 多上游、数据源、字段选择等能力只对支持的任务开放。
4. 创建过程在保存前提供只读配置预览、后端校验、dry-run 和 test-run。
5. 创建成功后按来源跳转到最合适页面。

## 3. 非目标

- 不把 PulseOps 做成通用低代码编排器。
- 不让前端绕过后端校验、执行或依赖保存。
- 不在高级预览里提供直接 JSON 编辑。
- 不为不支持上游数据的任务暴露多上游或数据源配置。
- 不在本设计中引入权限、审批或操作审计。

## 4. 设计改进结论

对现有设计和代码契约复核后，创建任务向导还需要重点补强以下点：

1. **能力矩阵需要前置**：不能只按 `kind` 渲染字段，还要按任务能力决定是否展示依赖触发、多上游、样本预览、字段选择和 AI Prompt。
2. **参数字段必须有规范化层**：前端表单字段必须严格映射后端 JSON 契约，例如 `script_exec` 使用 `params.workdir`，`ai_analyze` 使用 `params.prompt.text` 和结构化 `params.outputs`。
3. **依赖保存需要明确语义**：`TaskDefinition.dependencies` 可以随任务定义保存并由 store 替换下游依赖；独立 `/api/task-dependencies` 也可以编辑依赖边。向导必须选择一种主路径，避免任务创建成功但依赖边未保存或运行态未刷新。
4. **步骤不能只是视觉拆分**：每一步需要有进入条件、离开校验和错误回填，否则只是把长表单分页，仍然容易漏配置。
5. **预览要展示“有效定义”而不是原始表单值**：只读预览应展示经过字段规范化、默认值补齐和依赖合并后的 `TaskDefinition`。
6. **编辑态需要独立规则**：编辑任务不一定从模板开始，应根据已有 `kind`、缺失字段和 validate 结果定位到需要处理的步骤。

## 5. 入口与跳转

### 5.1 入口来源

创建任务入口需要携带来源上下文，前端通过 URL 参数或路由 state 传递：

| 来源 | 示例 | 创建时默认值 | 成功后跳转 |
| --- | --- | --- | --- |
| 任务监控列表 | `/tasks` | 无固定任务组 | 新任务详情页 `/tasks/{task_id}` |
| 工作台 | `/` | 无固定任务组 | 新任务详情页 `/tasks/{task_id}` |
| 任务详情 | `/tasks/{id}` | 可带入标签或任务组 | 新任务详情页 `/tasks/{task_id}` |
| 依赖拓扑 | `/pipelines/{id}` | 带入 `pipeline_id`；从节点创建时带入上游 | 返回依赖拓扑 `/pipelines/{id}` |
| 拓扑节点创建下游 | node action | 带入上游任务、依赖边、推荐下游类型 | 返回依赖拓扑并选中新节点 |

### 5.2 跳转规则

创建成功后不要固定回来源页，应按使用场景判断：

1. 从任务列表、工作台、普通创建入口进入：跳转新任务详情页。
2. 从依赖拓扑或拓扑节点创建下游进入：返回依赖拓扑页。
3. 如果 URL 明确传入 `from`，且 `from` 属于依赖拓扑路径，优先回 `from`。
4. 如果创建接口返回任务定义，应使用返回值中的 `task_id` 跳转；否则使用表单中的 `task_id`。

### 5.3 拓扑创建默认值

从依赖拓扑创建任务不是普通新建入口。拓扑已经提供了任务组、上游节点和下游意图，向导必须把这些上下文转成默认值，不能要求用户重复填写。

从拓扑节点创建下游时，P0 默认值如下：

| 字段 | 默认值 | 是否允许手动改 |
| --- | --- | --- |
| `pipeline_id` | 当前拓扑任务组 | 不允许，在页面只读展示 |
| `kind` | 抽屉快捷入口默认 `data_process`；右键菜单按用户选择传入 | 允许切换，但只推荐 `data_process` / `ai_analyze` |
| `task_id` | 前端生成唯一 ID | 允许在基础信息中修改一次 |
| `name` | `{上游名称} 数据处理` 或 `{上游名称} AI 分析` | 允许修改 |
| `trigger` | `on_run` | 不允许改为手动/定时，除非用户切换到不支持依赖的任务类型 |
| `interval` / `cron` | 空 | 不展示，依赖触发不需要调度字段 |
| `dependencies[0].upstream_task_id` | 当前节点任务 ID | 不要求用户选择，触发步骤只读展示 |
| `dependencies[0].downstream_task_id` | 新任务 ID | 自动生成 |
| `dependencies[0].condition` | `run_status == success` | P0 默认只读；P1 可展开高级修改 |
| `dependencies[0].source_key` | `upstream` | P0 默认只读；P1 可展开高级修改 |
| `labels.source` | `topology` | 可在基础信息中删除或修改 |

不同任务类型的参数默认值：

#### data_process

```json
{
  "params": {
    "source_task_id": "上游任务 ID",
    "extract_exprs": [
      {
        "field": "upstream_summary",
        "source_key": "upstream",
        "source": "summary",
        "jq_expr": ".",
        "agg_mode": ""
      }
    ]
  }
}
```

说明：

- `source_task_id` 用于单上游快捷解析，但界面上不再要求用户重复选择源任务。
- 默认提取上游 `summary` 全量，保证用户可以直接保存一个可运行的数据处理任务。
- 用户可以在数据源步骤改字段名、数据来源和 JQ 表达式。

#### ai_analyze

```json
{
  "params": {
    "analysis_type": "diagnose",
    "data_sources": [
      {
        "type": "upstream_output",
        "alias": "upstream",
        "config": { "task_id": "上游任务 ID" },
        "on_error": "fail"
      }
    ],
    "prompt": {
      "text": "分析上游任务输出..."
    },
    "outputs": [
      {
        "type": "summary",
        "config": { "field": "ai_analysis" }
      }
    ]
  }
}
```

说明：

- `upstream_output.config.task_id` 明确绑定拓扑上游，避免 test-run 或非触发场景没有 `TriggerRun` 时无法取数。
- Prompt 默认引用 `{{json .DataSources.upstream}}`。
- 输出默认写入 Summary 的 `ai_analysis` 字段，保证预览和下游消费有稳定字段。

### 5.4 拓扑任务组内普通创建

从任务组拓扑页顶部“创建任务”进入时，没有上游节点上下文，但仍然有任务组上下文。

规则：

- `pipeline_id` 固定为当前任务组，并只读展示。
- 不默认创建依赖边。
- 未选择模板前，`trigger` 默认用 `manual`，避免普通任务被迫填写 `interval` 或 `cron`。
- 用户选择 `data_process` 或 `ai_analyze` 后，触发方式默认切到 `on_run`，并要求在触发步骤选择至少一个上游。
- 用户选择不支持依赖的模板时，只填写该模板自己的业务参数，不展示依赖触发和多上游配置。

### 5.5 拓扑下游创建时切换模板

从节点创建下游时，模板切换必须保留用户的拓扑意图：

- 切换到 `data_process` 或 `ai_analyze` 时，继续保留拓扑上游依赖，并按新模板重建默认参数。
- 切换到不支持依赖的任务类型时，清空依赖边并把触发方式改为 `manual`。
- 再切回 `data_process` 或 `ai_analyze` 时，应从拓扑上下文恢复上游依赖和默认参数，不要求用户重新选择上游。

## 6. 任务能力矩阵

向导首先按任务能力决定步骤和字段，而不是只按 `kind` 切换参数表单。P0 版本建议收紧能力暴露范围：只有明确消费上游数据的任务才展示依赖触发、多上游、样本预览和字段选择。

| kind | 手动 | 定时 | 依赖触发 | 多上游 | 数据源步骤 | 样本预览 | 字段选择 | 说明 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `http_check` | 是 | 是 | 否 | 否 | 否 | 否 | 否 | 只配置请求和校验条件 |
| `tcp_check` | 是 | 是 | 否 | 否 | 否 | 否 | 否 | 只配置连通性目标 |
| `script_exec` | 是 | 是 | 否 | 否 | 否 | 否 | 否 | 只配置命令、参数、工作目录和环境变量 |
| `process_check` | 是 | 是 | 否 | 否 | 否 | 否 | 否 | 只配置进程关键字 |
| `scenario_check` | 是 | 是 | 否 | 否 | 否 | 否 | 否 | 自身已经包含 source/sample/fanout/evaluator 链路，不复用上游任务数据源 |
| `data_process` | 是 | 是 | 是 | 是 | 是 | 是 | 是 | 从上游运行数据抽取字段和聚合 |
| `ai_analyze` | 是 | 是 | 是 | 是 | 是 | 是 | 部分 | 使用上游输出、运行历史、历史分析或 HTTP 调用构造 Prompt |

规则：

- UI 层不为 `http_check`、`tcp_check`、`script_exec`、`process_check`、`scenario_check` 展示依赖触发和多上游配置。
- 如果后端仍接受普通任务的 `trigger = on_run`，它只作为兼容能力存在，不作为 P0 创建向导能力暴露。
- 从依赖拓扑创建下游时，默认推荐 `data_process` 或 `ai_analyze`，因为这两个任务能实际消费上游输出。
- 选择 `data_process` 或 `ai_analyze` 时默认使用依赖触发；没有拓扑上游时，由用户在触发步骤手动选择上游。
- 能力矩阵应落成前端常量，驱动步骤展示、字段校验和模板默认值。

## 7. 向导结构

创建任务使用横向或侧向步骤条。每一步只处理一个决策维度，不能把所有字段摊开在同一页。

| 步骤 | 名称 | 目的 | 是否可跳过 |
| --- | --- | --- | --- |
| 1 | 选择模板 | 确定任务类型和推荐配置路径 | 不可跳过 |
| 2 | 基础信息 | 填写名称、任务 ID、任务组、标签、启用状态 | 不可跳过 |
| 3 | 触发方式 | 配置手动、定时或依赖触发 | 不可跳过 |
| 4 | 任务参数 | 按任务类型填写业务参数 | 不可跳过 |
| 5 | 数据源 | 仅支持数据源的任务展示 | 条件展示 |
| 6 | 观测与告警 | 配置 trace、脱敏、保留、告警 | 可使用默认值 |
| 7 | 预览与验证 | 只读预览、validate、dry-run、test-run、提交 | 不可跳过 |

移动端或窄屏可以使用垂直步骤条，但不能降级成长表单。

### 7.1 向导状态机

向导步骤需要有明确状态，而不是只靠 Ant Design `Steps` 做视觉提示。

| 状态 | 含义 |
| --- | --- |
| `locked` | 依赖前置步骤，暂不可进入 |
| `editable` | 可进入编辑 |
| `invalid` | 已进入但校验失败 |
| `valid` | 当前步骤本地校验通过 |
| `verified` | 最终预览已通过后端 validate |

规则：

- 下一步前只校验当前步骤和依赖字段。
- 提交前必须重新生成完整 `TaskDefinition` 并调用后端 validate。
- 任一步骤修改会使预览步骤从 `verified` 回退到 `editable`。
- 后端 validate 返回错误时，应定位到最可能的步骤；无法定位时展示在预览步骤顶部。

### 7.2 表单值规范化

向导内部可以使用更适合 UI 的临时结构，但提交前必须通过规范化函数生成后端契约。

```text
WizardFormState -> normalizeTaskDefinition() -> TaskDefinition -> validate/create/update
```

规范化要求：

| UI 字段 | 后端字段 |
| --- | --- |
| 标签键值列表 | `labels: Record<string,string>` |
| Header/Env 键值列表 | `params.headers`、`params.env` 等 map 字段 |
| 脚本工作目录 | `params.workdir` |
| AI Prompt 文本框 | `params.prompt.text` |
| AI 输出字段快捷配置 | `params.outputs: OutputSpec[]` |
| 依赖触发上游列表 | `dependencies: TaskDependency[]` |

只读预览展示的是规范化后的 `TaskDefinition`，不是原始表单状态。

## 8. 步骤设计

### 8.1 选择模板

模板选择必须先于字段表单出现。模板卡片展示任务类型、适用场景和配置复杂度。

| 模板 | kind | 适用场景 | 是否支持依赖触发 | 是否支持数据源配置 |
| --- | --- | --- | --- | --- |
| HTTP 检查 | `http_check` | URL 健康检查、接口返回校验 | 否 | 否 |
| TCP 检查 | `tcp_check` | 端口连通性检查 | 否 | 否 |
| 脚本执行 | `script_exec` | 本机命令或脚本执行 | 否 | 否 |
| 进程检查 | `process_check` | 本机进程存在性检查 | 否 | 否 |
| 场景巡检 | `scenario_check` | 拉列表、采样、fan-out、规则评估 | 否 | 否 |
| 数据处理 | `data_process` | 从上游运行数据提取字段、聚合、生成摘要 | 是 | 是 |
| AI 分析 | `ai_analyze` | 基于上游输出、运行历史或外部数据进行诊断 | 是 | 是 |

规则：

- 用户选择模板后才进入下一步。
- 修改模板时必须提示会清空不兼容的参数字段。
- 从拓扑节点创建下游时，可以预选 `data_process` 或 `ai_analyze`，但用户仍可切换到其它模板。

### 8.2 基础信息

基础信息只保留所有任务共用字段：

| 字段 | 说明 | 规则 |
| --- | --- | --- |
| `task_id` | 任务唯一 ID | 创建时自动生成；允许高级用户修改一次；保存后不可改 |
| `name` | 任务显示名称 | 必填 |
| `pipeline_id` | 所属任务组 | 可选；从拓扑进入时默认当前任务组 |
| `labels` | 标签 | 可选；推荐 `env`、`service`、`domain` |
| `enabled` | 是否启用 | 默认启用 |

基础信息页不展示 `params`、trace、alert 等高级结构。

### 8.3 触发方式

触发方式根据任务本身能力和入口上下文展示。

| 触发方式 | 字段 | 规则 |
| --- | --- | --- |
| 手动 | `trigger = manual` | 不要求 `interval` 或 `cron` |
| 定时 | `trigger = scheduled`、`interval` 或 `cron` | `interval` 和 `cron` 互斥，至少一个必填 |
| 依赖触发 | `trigger = on_run`、依赖边 | 需要至少一个上游；仅对支持依赖的场景展示 |

依赖触发配置分两层：

1. `data_process` 和 `ai_analyze` 可以配置一个或多个上游，并为每个上游设置 `source_key`。
2. 其它任务类型 P0 不展示依赖触发；即使后端兼容 `on_run`，也不作为创建向导的推荐路径。

多上游依赖使用 `TaskDefinition.dependencies` 和 `TaskDependency`：

```json
{
  "dependencies": [
    {
      "upstream_task_id": "source-a",
      "condition": "check_status == pass",
      "source_key": "source_a",
      "params": {}
    }
  ]
}
```

兼容规则：

- 单上游旧字段 `watch_task_id` 可以作为快捷入口或兼容字段。
- 新建多上游任务时应优先写入 `dependencies`。
- 多上游任务中的 `source_key` 必须唯一。

依赖和数据源的职责边界：

| 结构 | 作用 | 是否影响触发 |
| --- | --- | --- |
| `dependencies[]` | 定义上游触发边、触发条件和触发型 `source_key` | 是 |
| `params.data_sources[]` | 定义任务运行时额外读取的数据源 | 否 |
| `params.common_params` | 为多个数据源复用授权、Header 模板、超时等参数 | 否 |

规则：

- `dependencies[].source_key` 和 `params.data_sources[].key` 属于同一个命名空间，不允许重复。
- 如果某个上游既要触发任务又要被字段提取引用，应优先放在 `dependencies[]` 并设置 `source_key`。
- 如果某个上游只作为补充读取数据，不参与触发，可放在 `params.data_sources[]`。
- 保存 `TaskDefinition.dependencies` 时，store 会替换该下游任务的依赖边；向导应优先使用这个路径完成“创建任务 + 依赖边”。
- 独立 `/api/task-dependencies` 更适合拓扑边的后续编辑，不作为创建向导的主保存路径。

### 8.4 任务参数

任务参数步骤只展示当前模板相关字段。

#### HTTP 检查

| 字段 | 说明 |
| --- | --- |
| `params.url` | 请求地址，必填 |
| `params.method` | 请求方法，默认 GET |
| `params.headers` | 请求头键值对 |
| `params.body` | 请求体；只在非 GET/HEAD 时突出展示 |
| `params.expect_status` | 期望状态码 |
| `params.expect_body_contains` | 响应体包含校验 |

#### TCP 检查

| 字段 | 说明 |
| --- | --- |
| `params.address` | `host:port`，必填 |

#### 脚本执行

| 字段 | 说明 |
| --- | --- |
| `params.command` | 命令路径或命令名，必填 |
| `params.args` | 参数列表 |
| `params.workdir` | 工作目录 |
| `params.env` | 环境变量键值对 |

#### 进程检查

| 字段 | 说明 |
| --- | --- |
| `params.name` | 进程名，必填 |

#### 场景巡检

| 字段 | 说明 |
| --- | --- |
| `params.source.url` | 源数据 URL，必填 |
| `params.source.method` | 源请求方法 |
| `params.source.headers` | 源请求头 |
| `params.sample` | 采样配置 |
| `params.fanout.url` | fan-out 请求 URL |
| `params.fanout.method` | fan-out 请求方法 |
| `params.evaluator.name` | 评估器 |
| `params.thresholds` | 阈值配置 |

场景巡检参数复杂，允许把采样、fan-out、阈值放在折叠区，但仍属于当前任务参数步骤。

### 8.5 数据源

数据源步骤只对支持的任务展示：

- `data_process`
- `ai_analyze`

其它任务类型不展示这一页，也不展示多上游数据源配置。

#### 数据处理

数据处理任务使用当前后端支持的参数结构：

```json
{
  "params": {
    "source_task_id": "source-a",
    "data_sources": [
      {
        "key": "source_a",
        "task_id": "source-a",
        "params": {}
      }
    ],
    "common_params": {},
    "extract_exprs": [
      {
        "field": "cost_ms",
        "source_key": "source_a",
        "source": "summary",
        "jq_expr": ".duration_ms",
        "agg_mode": ""
      }
    ]
  }
}
```

页面分区：

| 分区 | 说明 |
| --- | --- |
| 上游数据源 | 展示依赖边和额外 `data_sources`；每个数据源有 `key`、任务、可选参数 |
| 公共参数 | `common_params`，用于复用授权信息、Header 模板、超时等 |
| 样本预览 | 按上游展示 `summary`、`record`、`payload`、artifact 样本 |
| 字段提取 | 配置 `extract_exprs`，支持字段选择器和高级 JQ 输入 |
| 聚合 | 对数组结果支持 `sum`、`avg`、`count`、`min`、`max` |

规则：

- 多个上游时，提取表达式必须指定 `source_key`。
- `source_key` 来源可以是依赖边，也可以是 `params.data_sources`。
- `source_key` 不允许重复。
- 字段选择器只辅助生成 `jq_expr`，最终保存仍是结构化参数。

#### AI 分析

AI 分析任务的数据源应展示为“分析输入”，而不是底层 JSON 数组。

支持的数据源类型：

| 类型 | 说明 |
| --- | --- |
| `upstream_output` | 上游任务输出 |
| `run_context` | 本次触发上下文 |
| `run_history` | 指定任务运行历史 |
| `previous_analysis` | 历史 AI 分析 |
| `http_call` | 外部 HTTP 数据 |

字段：

| 字段 | 说明 |
| --- | --- |
| `params.analysis_type` | `diagnose`、`trend`、`evaluate` |
| `params.data_sources[].type` | 数据源类型 |
| `params.data_sources[].alias` | Prompt 中引用的别名 |
| `params.data_sources[].config` | 该数据源的配置 |
| `params.data_sources[].on_error` | 数据源失败策略，例如 `fail` 或 `skip` |
| `params.prompt.text` | 提示词模板文本 |
| `params.outputs[].type` | 输出写入器类型 |
| `params.outputs[].config` | 输出写入器配置 |

规则：

- 从拓扑节点创建 AI 下游时，自动加入 `upstream_output` 数据源，别名默认 `upstream`。
- Prompt 编辑区要展示可用变量，但不应该要求用户理解完整底层 JSON。
- 保存前必须校验数据源别名唯一。
- 别名不能与内置数据源类型冲突：`run_context`、`run_history`、`previous_analysis`、`http_call`。
- 前端表单里的 Prompt 文本必须保存到 `params.prompt.text`，不能保存成 `params.prompt` 字符串。
- 输出配置必须保存为 `OutputSpec[]`，不能保存成字符串数组。

### 8.6 观测与告警

该步骤使用默认值优先，避免阻塞新建任务。

| 分区 | 字段 | 默认策略 |
| --- | --- | --- |
| Trace | `trace.level`、`trace.retain_days`、`trace.mask_fields` | 默认继承平台配置 |
| Alert | `alert.consecutive_failures`、`alert.channels`、`alert.recover_notify` | 默认不强制告警 |

展示规则：

- 默认只展示常用项。
- 保留天数、脱敏字段、通知渠道可放入高级折叠区。
- 对 HTTP Header、环境变量、Prompt 中的敏感字段给出脱敏建议。

### 8.7 预览与验证

最后一步提供只读预览和执行前验证。

必须包含：

- 只读 `TaskDefinition` JSON 预览。
- 当前任务摘要：类型、触发方式、上游数量、关键参数、trace/alert 状态。
- 后端 `validate` 结果。
- `dry-run` 按钮。
- `test-run` 按钮。
- 创建按钮。
- 对规范化差异的提示，例如 UI 字段如何映射为最终 JSON。

规则：

- 高级 JSON 预览只读，不允许直接编辑。
- 用户需要修改字段时回到对应步骤。
- `validate` 失败时不能创建。
- `test-run` 失败时允许用户返回修改；是否允许强制保存由后端校验结果决定，前端不绕过 validate。
- 当前后端 validate 返回 `errors: string[]`，P0 可以展示通用错误并通过关键字粗略定位步骤。
- P1 建议扩展 validate 响应为字段级错误，例如 `field_errors: [{ path, message }]`，用于精确回填步骤和表单项。

## 9. 前端组件边界

建议将现有 `TaskForm` 拆成向导容器和任务类型子表单。

```text
TaskEditor
  TaskCreationWizard
    TemplateStep
    BasicInfoStep
    TriggerStep
    DriverParamsStep
      HTTPCheckParams
      TCPCheckParams
      ScriptExecParams
      ProcessCheckParams
      ScenarioCheckParams
      DataProcessParams
      AIAnalyzeParams
    DataSourcesStep
    ObservabilityStep
    PreviewValidateStep
```

组件职责：

| 组件 | 职责 |
| --- | --- |
| `TaskEditor` | 读取路由上下文、加载编辑态初始值、处理提交后跳转 |
| `TaskCreationWizard` | 管理步骤、表单状态、步骤校验和只读预览 |
| `TemplateStep` | 选择 `kind`，处理切换模板时的不兼容字段清理 |
| `TriggerStep` | 管理 `trigger`、调度字段和依赖边字段 |
| `DataSourcesStep` | 只在支持任务类型时挂载，管理多上游、样本和字段提取 |
| `PreviewValidateStep` | 调用 validate、dry-run、test-run、展示结果 |

现有任务参数表单可以继续复用，但要从长表单里的所有卡片改成步骤内容。

## 10. 后端与 API 契约

本设计优先复用现有接口：

| 能力 | 接口 |
| --- | --- |
| 获取任务定义 | `GET /api/task-defs/{id}` |
| 创建任务定义 | `POST /api/task-defs` |
| 更新任务定义 | `PUT /api/task-defs/{id}` |
| 校验任务定义 | `POST /api/task-defs/validate` |
| dry-run | `POST /api/task-defs/dry-run` |
| test-run | `POST /api/task-defs/test-run` |
| 上游样本 | `GET /api/tasks/{id}/sample?source=...` |
| 创建/更新依赖边 | `POST /api/task-dependencies` |

当前契约事实：

1. `POST /api/task-defs` 和 `PUT /api/task-defs/{id}` 返回 `TaskDefinition`。
2. `TaskDefinition.dependencies` 随任务定义保存时，Postgres store 会替换该下游任务的依赖边。
3. `POST /api/task-defs/validate` 当前返回 `{ valid, errors, normalized }`，错误是字符串数组，不是字段级结构。
4. `POST /api/task-defs/test-run` 成功时返回 `RunRecord`；失败时返回包含 `record` 和 `error` 的对象，前端类型需要覆盖失败响应。
5. `/api/task-dependencies` 可以独立创建或编辑依赖边，并会重新加载下游任务。

建议补齐：

1. validate 响应增加字段级错误：`field_errors: [{ path, message, step }]`。
2. test-run 失败响应前端类型显式建模，避免只能以异常消息展示。
3. create/update 的依赖替换语义写入 API 注释和前端调用约定。
4. 创建下游任务时优先一次提交 `TaskDefinition.dependencies`，减少“任务创建成功但依赖边保存失败”的补偿场景。
5. 如果未来继续通过 `/api/task-dependencies` 后补依赖边，需要返回可恢复错误并提供重试入口。

## 11. 校验策略

### 11.1 前端校验

前端负责即时校验和步骤完整性：

- `name` 必填。
- `kind` 必填。
- 定时任务必须设置 `interval` 或 `cron`，且二者互斥。
- `data_process` 和 `ai_analyze` 使用依赖触发时必须至少选择一个上游。
- 只对支持数据源的任务校验 `source_key`、数据源别名和提取表达式。
- HTTP URL、TCP address、脚本 command 等任务类型必填字段在对应步骤校验。
- AI Prompt 必须写入 `params.prompt.text`。
- AI 输出配置必须规范化为 `OutputSpec[]`。
- `script_exec` 工作目录必须写入 `params.workdir`。

### 11.2 后端校验

后端仍是最终判断：

- 校验 `TaskDefinition` 基础字段。
- 校验 driver-specific params。
- 校验依赖条件表达式。
- 校验多上游 `source_key` 是否重复、是否存在、是否被表达式引用。
- 校验 AI 数据源和 Prompt 所需最小字段。

前端不能因为本地校验通过就跳过后端 validate。

## 12. 编辑态、状态与草稿

P0 不要求做服务端草稿，但向导切换步骤时不能丢失已填写字段。

建议：

- 表单状态在 `TaskCreationWizard` 内集中维护。
- 切换模板时只清理不兼容字段，保留基础信息、任务组、标签。
- 浏览器刷新后是否恢复草稿作为 P1。
- 编辑态进入向导时，根据已有 `kind` 和字段自动定位到第一个需要处理的问题步骤。
- 编辑态不强制从模板步骤开始；默认进入第一个 invalid 步骤，没有问题时进入任务参数或预览步骤。
- 编辑态切换 `kind` 属于破坏性操作，需要二次确认，并清理旧 `params`、不兼容依赖和数据源。
- 编辑态保存时，如果 `dependencies` 为 `undefined`，不应隐式清空依赖；只有用户在向导里进入并修改依赖配置后才提交新的 `dependencies` 数组。

## 13. 验收标准

P0 验收：

- 创建任务页面是分步向导，不再展示完整长表单。
- 每个任务类型只展示自己的参数字段。
- `data_process` 和 `ai_analyze` 才展示数据源步骤。
- 多上游配置只在支持任务中出现。
- 从任务列表创建成功后跳转到新任务详情。
- 从依赖拓扑创建成功后回到依赖拓扑。
- 预览 JSON 只读。
- 保存前调用后端 validate。
- dry-run 和 test-run 结果在预览步骤展示。
- 原有任务类型都能创建成功。
- `script_exec` 创建时工作目录保存为 `params.workdir`。
- `ai_analyze` 创建时 Prompt 保存为 `params.prompt.text`。
- AI 输出配置保存为 `OutputSpec[]`，不是字符串数组。
- 创建带上游的 `data_process` 或 `ai_analyze` 时，依赖边随任务定义保存并在拓扑中可见。

P1 验收：

- validate 字段错误能定位到步骤。
- 数据源样本预览支持多上游切换。
- AI Prompt 区展示可用变量和样本插入能力。
- 支持浏览器本地草稿恢复。
- 从拓扑创建下游后自动选中新节点或高亮新边。
- test-run 失败响应能展示部分运行记录和错误信息。

## 14. 实施拆分

建议按以下顺序落地：

1. 抽出 `TaskCreationWizard`，把现有长表单拆成步骤，但不改变保存契约。
2. 建立任务能力矩阵，驱动步骤显示和字段校验。
3. 建立 `normalizeTaskDefinition()`，修正 `workdir`、`prompt.text`、`outputs`、labels、headers/env、dependencies 等字段映射。
4. 调整创建成功跳转规则。
5. 将任务类型参数按模板条件挂载，避免不相关字段出现。
6. 抽出 `DataSourcesStep`，只对 `data_process` 和 `ai_analyze` 展示。
7. 将预览 JSON 改为规范化后的只读 `TaskDefinition`，并在预览步骤集中执行 validate、dry-run、test-run。
8. 优先通过 `TaskDefinition.dependencies` 保存创建期依赖边。
9. 补充多上游字段级校验和错误定位。
10. 优化拓扑入口创建下游的默认值和成功回跳。
