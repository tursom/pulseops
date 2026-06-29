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

1. 插件包统一安装到固定目录，由 `pulseops.plugin.yaml` 描述身份、版本、能力、权限、配置 schema 和声明式 UI。
2. 内置能力和外部插件能力统一进入 Plugin Catalog，前端可查看、启停、重载和诊断。
3. 插件可以扩展任务模板、任务驱动、AI 数据源、输出写入器、业务 evaluator、事件 Hook、通知 Sink、前端入口。
4. 外部插件优先使用隔离运行时，不默认把不可信代码加载进 PulseOps 主进程。
5. 插件能力可以被任务创建向导消费，例如新增模板卡片、AI 数据源选项、参数 schema。
6. 插件可以声明插件级配置和能力级配置；PulseOps 提供配置实例、版本、资产、密钥、校验和任务覆盖能力。
7. 插件运行要有权限声明、超时、资源限制、错误隔离和审计留痕。
8. 插件升级、禁用、加载失败不能影响已有核心任务调度和管理页面启动。

## 3. 非目标

第一阶段不做这些事：

- 不把通用 Go `.so` 作为任务驱动热加载到主进程，也不为旧 AI C ABI 插件提供 V1 兼容入口。存量插件若要继续使用，需要迁移成新的 YAML manifest 插件能力。
- 不允许插件直接拿到 PostgreSQL、MinIO、AI API Key 等全量内部凭证。
- 不在前端直接执行插件提供的任意 JS。前端扩展优先使用 schema 驱动表单、菜单入口和受控 iframe。
- 不做公开插件市场。先支持本地目录安装和内部插件包治理。
- 不兼容 `pulseops.plugin.toml`。V1 只支持 `pulseops.plugin.yaml`，避免复杂配置 schema 在 TOML 中失控。

## 4. 总体架构

```text
plugins/
  steam-price-diagnostics/
    releases/
      1.0.0/
        pulseops.plugin.yaml
        bin/diagnose
        README.md
      1.1.0/
        pulseops.plugin.yaml
        bin/diagnose
        README.md
    active -> releases/1.0.0
  webhook-notifier/
    releases/2026.06.27-1/
      pulseops.plugin.yaml
      bin/notifier

PulseOps
  config.LoadGlobal
      |
      v
  plugin.Manager
      |-- 注册内置插件包
      |-- 扫描 plugins/*/releases/*/pulseops.plugin.yaml
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
    pulseops.plugin.yaml
    bin/
    schemas/
    static/
    README.md
  active -> releases/<active-version>
```

约束：

- 插件版本目录一旦进入 `validated` 或 `active` 状态后不可原地修改。
- 每个 release 必须且只能包含一个 `pulseops.plugin.yaml`。若同时出现 `pulseops.plugin.yaml` 和 `pulseops.plugin.toml`，视为插件包格式错误。
- 新版本必须写入新的 release 目录，例如 `releases/1.1.0/` 或 `releases/2026.06.27-1/`。
- `active` 软链接只用于人工排查和离线工具，运行时切换以 DB 中的 `active_version` 为准。
- 删除 release 前必须确认没有 registry generation 和 in-flight run 仍引用它。
- 容器部署时，插件目录必须挂载为可写持久卷；镜像内置插件只能作为 bundled official release，不作为在线更新目录。

`pulseops.plugin.yaml` 是 V1 唯一支持的插件声明文件。YAML 只作为声明式数据格式使用，不允许自定义 tag，不依赖 anchor/alias/merge 作为核心能力；PulseOps 读取后会 normalize 成内部 manifest model 并做 schema 校验。

`pulseops.plugin.yaml` 示例：

```yaml
schema_version: pulseops.plugin/v1
id: steam-price-diagnostics
name: Steam 价格诊断
version: 1.0.0
description: 提供 Steam 价格异常诊断任务模板和 AI 数据源
author: YYM
homepage: https://internal.example.com/pulseops/plugins/steam-price-diagnostics
enabled: true
permissions:
  - network:outbound
  - runs:read
  - ai:read

config_classes:
  RetryPolicy:
    title: 重试策略
    fields:
      enabled:
        type: bool
        default: false
        ui:
          label: 启用重试
          widget: switch
          order: 10
      max_attempts:
        type: number
        default: 2
        validation:
          min: 1
          max: 5
        ui:
          label: 最大重试次数
          widget: number
          order: 20

config:
  title: 插件公共配置
  validate_action: validate_config
  fields:
    api_base_url:
      type: string
      required: true
      overridable: false
      validation:
        pattern: "^https?://"
      ui:
        group: connection
        label: API Base URL
        widget: input
        order: 10
    api_token:
      type: secret
      required: true
      overridable: false
      ui:
        group: auth
        label: API Token
        widget: secret
        order: 20
    retry:
      type: object
      class: RetryPolicy
      default:
        enabled: false
      ui:
        group: runtime
        label: 重试
        widget: object
        order: 30

task_templates:
  - id: steam-price-diagnose-template
    kind: ai_analyze
    title: Steam 价格异常诊断
    description: 基于上游价格检查结果、历史运行和历史 AI 分析生成诊断结论
    defaults:
      trigger: on_run
      timeout: 45s
    params:
      analysis_type: diagnose

data_sources:
  - name: grpc_game_inventory
    title: gRPC 游戏库存查询
    protocol: grpc
    runtime: builtin
    permissions:
      - network:outbound
      - grpc:call
    config:
      title: 库存查询配置
      allow_plugin_config_ref: true
      validate_action: validate_config
      fields:
        service:
          type: string
          required: true
          overridable: true
          ui:
            group: call
            label: Service
            widget: input
            order: 10
        method:
          type: string
          required: true
          overridable: true
          ui:
            group: call
            label: Method
            widget: input
            order: 20
        request:
          type: object
          class: JSONObject
          required: true
          overridable: true
          ui:
            group: call
            label: 请求 JSON
            widget: json
            order: 30

ai_data_sources:
  - name: steam_price_context
    title: Steam 价格上下文
    runtime: process
    entrypoint: bin/steam-price-context
    permissions:
      - runs:read
      - network:outbound

ui_extensions:
  - id: steam-price-help
    title: Steam 价格诊断说明
    path: /plugins/steam-price-diagnostics/help
```

## 6. 能力模型

统一能力类型：

| type | 用途 | 第一阶段 |
| --- | --- | --- |
| `task_template` | 给任务创建向导提供模板、默认参数、表单 schema | 支持 |
| `task_driver` | 新增任务执行类型 | 第二阶段支持 process/http |
| `data_source` | 平台通用数据源，供 `data_process`、`ai_analyze`、后续 `scenario_check` 复用 | 第二阶段 |
| `ai_data_source` | AI 专用数据源扩展；供现有 `ai_analyze` 任务类型消费 | 支持 manifest 展示，第二阶段运行时接入 |
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
  core-tasks/pulseops.plugin.yaml
  scenario/pulseops.plugin.yaml
  data-process/pulseops.plugin.yaml
  ai/pulseops.plugin.yaml
  trace-sinks/pulseops.plugin.yaml
  grpc-source/pulseops.plugin.yaml
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

## 7. 插件配置系统

插件配置系统是 V1 的核心能力，不只服务 gRPC 数据源。它负责把插件声明的配置模型转成可编辑、可校验、可加密、可版本化、可被任务引用和覆盖的运行时配置。

核心边界：

- 插件只在 `pulseops.plugin.yaml` 中声明配置 schema、class、约束和 UI，不直接保存配置值。
- PulseOps 负责配置实例、配置版本、资产版本、secret 加密、审计和运行时合并。
- 前端只渲染声明式 UI schema，不执行插件提供的任意 JavaScript。
- run 启动时绑定明确的 `plugin_generation_id`、配置版本和实际资产版本，保证历史可追溯。

### 7.1 配置归属

配置分两层：

| 层级 | 用途 | 示例 |
| --- | --- | --- |
| 插件级配置 | 同一个插件内多个能力共享的连接、安全和默认运行参数 | API base URL、endpoint、TLS、通用 metadata、认证方式、默认超时 |
| capability 级配置 | 某个具体能力自己的配置 | gRPC data source 的 proto/descriptor、service、method、request 模板 |

`capability` 是插件暴露给 PulseOps 的具体能力，例如 `@pulseops/grpc-source / data_source / grpc`。一个插件可以有多个 capability，每个 capability 都可以有自己的配置 schema。

capability 配置可以引用一个插件级配置实例。执行时合并顺序固定为：

```text
schema defaults
+ plugin config active version
+ capability config active version
+ task overrides
```

任务覆盖默认禁止，只有字段显式声明 `overridable: true` 时才能在任务中覆盖。连接地址、TLS、认证这类字段可以声明为可覆盖，但默认必须保守关闭。

### 7.2 Manifest 结构

插件内可复用类型写在 `config_classes` 中，只允许当前插件内部引用：

```yaml
config_classes:
  TLSConfig:
    title: TLS 配置
    fields:
      enabled:
        type: bool
        default: false
        ui:
          label: 启用 TLS
          widget: switch
          order: 10
      server_name:
        type: string
        ui:
          label: Server Name
          widget: input
          order: 20
      ca_cert:
        type: file
        asset_kind: certificate
        asset_scope: config_instance
        accept:
          - .crt
          - .pem
        ui:
          label: CA 证书
          widget: file
          order: 30
```

插件级配置写在顶层 `config`：

```yaml
config:
  title: gRPC 公共连接配置
  description: 整个 gRPC 插件共享的连接、鉴权和默认调用配置
  validate_action: validate_config
  fields:
    endpoint:
      type: string
      required: true
      overridable: true
      validation:
        min_len: 3
        max_len: 255
        pattern: "^[^:]+:[0-9]+$"
      ui:
        group: connection
        label: 服务地址
        widget: input
        placeholder: inventory.service:9090
        order: 10
    tls:
      type: object
      class: TLSConfig
      default:
        enabled: false
      overridable: true
      ui:
        group: connection
        label: TLS
        widget: object
        order: 20
    authorization:
      type: secret
      required: false
      overridable: true
      ui:
        group: auth
        label: Authorization
        widget: secret
        order: 30
```

capability 级配置写在对应 capability 下的 `config`：

```yaml
data_sources:
  - name: grpc
    title: gRPC Unary
    protocol: grpc
    runtime: builtin
    permissions:
      - network:outbound
      - grpc:call
    config:
      title: gRPC 数据源配置
      description: 定义 proto、默认 service/method 和请求模板
      allow_plugin_config_ref: true
      validate_action: validate_config
      fields:
        schema_mode:
          type: select
          required: true
          default: reflection
          overridable: true
          options:
            - value: reflection
              label: 服务端 Reflection
            - value: descriptor_set
              label: Descriptor Set
            - value: proto_files
              label: Proto 文件
          ui:
            group: schema
            label: Schema 来源
            widget: select
            order: 10
        descriptor:
          type: file
          asset_kind: proto_descriptor_set
          asset_scope: capability_shared
          accept:
            - .pb
            - .protoset
            - .desc
          overridable: true
          ui:
            group: schema
            label: Descriptor 文件
            widget: file
            visible_when:
              field: schema_mode
              op: eq
              value: descriptor_set
            order: 20
        proto_files:
          type: array
          items:
            type: file
            asset_kind: proto_file
            asset_scope: capability_shared
            accept:
              - .proto
          overridable: true
          ui:
            group: schema
            label: Proto 文件
            widget: file_list
            visible_when:
              field: schema_mode
              op: eq
              value: proto_files
            order: 25
        service:
          type: string
          required: true
          overridable: true
          ui:
            group: call
            label: Service
            widget: input
            order: 30
        method:
          type: string
          required: true
          overridable: true
          ui:
            group: call
            label: Method
            widget: input
            order: 40
        request:
          type: object
          class: JSONObject
          required: true
          overridable: true
          ui:
            group: call
            label: 请求 JSON
            widget: json
            order: 50
```

### 7.3 类型系统

字段类型必须严格声明：

| 类型 | 说明 | 关键约束 |
| --- | --- | --- |
| `string` | 字符串 | 支持 `min_len`、`max_len`、`pattern` |
| `number` | 数值 | 支持 `min`、`max`、`step` |
| `bool` | 布尔值 | UI 通常为 switch/checkbox |
| `select` | 单选 | 必须声明 `options` |
| `multi_select` | 多选 | 必须声明 `options` |
| `object` | 结构对象 | 必须声明 `class` |
| `array` | 数组 | 必须声明 `items` |
| `file` | 文件资产引用 | 必须声明 `asset_kind` 和 `asset_scope`，可声明 `accept` |
| `secret` | 加密密钥引用 | DB 仅保存密文，API 只返回 masked 值 |

`object` 不能默认为任意 JSON，必须引用当前插件内的 class。少量需要动态结构的官方 adapter 可以显式引用系统保留 class `JSONObject`，但必须配合 `validate_action` 做领域校验，例如 gRPC request 需要根据 proto schema 校验。`array` 不能是任意数组，必须指定元素类型：

```yaml
config_classes:
  HeaderItem:
    fields:
      key:
        type: string
        required: true
      value:
        type: secret
        required: true

config:
  fields:
    headers:
      type: array
      items:
        type: object
        class: HeaderItem
      ui:
        label: 请求头
        widget: table
```

class 可以引用同一插件内的其他 class，但不能跨插件引用。加载 manifest 时必须检查循环引用、未知 class、未知字段类型、缺失 `items`、缺失 `options`、缺失或非法 `asset_scope` 等错误。

### 7.4 声明式 UI

字段可以通过 `ui` 描述前端表现：

| 字段 | 说明 |
| --- | --- |
| `label` | 显示名称 |
| `widget` | 控件类型，如 `input`、`textarea`、`number`、`switch`、`select`、`multi_select`、`json`、`file`、`file_list`、`secret`、`object`、`table` |
| `group` | 所属分组 |
| `order` | 展示顺序 |
| `placeholder` | 占位提示 |
| `help` | 辅助说明 |
| `advanced` | 是否放入高级配置区域 |
| `collapsed` | 分组默认是否折叠 |
| `visible_when` | 条件展示 |

条件展示只支持受控表达式：

```yaml
visible_when:
  field: tls.enabled
  op: eq
  value: true
```

V1 支持的 `op`：`eq`、`ne`、`in`、`not_in`、`exists`、`empty`。不支持任意表达式和插件自定义前端代码。

### 7.5 配置实例和版本

用户基于 schema 创建配置实例。实例只是稳定身份，配置值保存在版本中：

```text
plugin_config_instance
  id = grpc-prod-common
  plugin_id = @pulseops/grpc-source

plugin_config_version
  instance_id = grpc-prod-common
  version = 3
  status = active
  values = {...}
```

状态流转：

```text
draft -> validated -> active
active -> retired
draft -> failed
validated -> failed
```

规则：

- 修改 active 配置时不原地覆盖，必须创建新的 draft version。
- draft 可反复编辑；进入 validated 后不可修改。
- activate 成功后，新 run 使用新配置版本，已开始的 run 继续使用启动时绑定的版本。
- disabled 的配置实例不能被新任务引用，但历史 run 仍可追溯。
- run record 需要记录插件 generation、插件级配置版本、capability 配置版本、任务覆盖摘要和实际资产版本。

### 7.6 文件资产

文件资产用于保存 proto、descriptor、证书等不能直接塞进配置 JSON 的内容。V1 支持两类资产，但不做跨插件的平台级共享：

| 作用域 | manifest 值 | 用途 | 示例 |
| --- | --- | --- | --- |
| 插件/能力共享资产 | `plugin_shared`、`capability_shared` | 同一个插件或同一个 capability 内多个配置实例复用 | gRPC proto files、descriptor set、公共 CA 证书 |
| 配置实例私有资产 | `config_instance` | 只服务某个配置实例，避免误共享敏感或临时文件 | 某个环境的客户端证书、私有 proto 包 |

配置值只保存资产引用，不保存文件内容：

```text
plugin_asset
  id = inventory-proto
  plugin_id = @pulseops/grpc-source
  capability_id = grpc
  config_instance_id = null
  scope = capability_shared
  kind = proto_files

plugin_asset_version
  asset_id = inventory-proto
  version = 4
  status = active
  storage = db
  checksum = sha256:...
```

规则：

- 共享资产可以被同一插件或 capability 下的多个配置实例和多个配置版本引用。
- 配置实例私有资产只能被所属配置实例引用，不能被其他实例选择。
- 更新资产不要求重新创建配置版本；配置版本引用稳定的 `asset_id`，运行时解析到当前 active asset version。
- 资产版本也必须走 validate/activate；新 run 解析到当前 active asset version。
- run 启动时记录实际使用的 asset version，避免资产更新后历史运行不可解释。
- 资产删除前必须确认没有配置版本或历史 run 引用。
- 小文件资产可以直接存入数据库；如果后续迁移到对象存储，DB 仍保存资产版本、checksum、大小、存储位置和状态，配置引用语义不变。
- 跨插件共享资产不进入 V1，避免权限和生命周期边界变复杂；确需共享时由各插件各自创建资产引用。

gRPC 插件的 `.proto`、descriptor set、CA 证书、客户端证书都走资产模型。

### 7.7 Secret

`secret` 是一等字段类型。前端保存 secret 后，DB 存密文，API 读回只返回 masked 视图：

```json
{
  "secret_id": "sec_01J...",
  "masked": "********",
  "updated_at": "2026-06-28T10:00:00Z"
}
```

规则：

- 普通配置 JSON 中只保存 `secret_id`，不保存明文。
- 后端在 validate/run 调用前解析 secret，并只在内存中生成最终配置。
- 插件中心、任务详情、运行详情默认展示 masked 值。
- 任务覆盖 secret 时也必须引用 secret，不能直接提交明文到任务定义。
- V1 的 secret 加密材料和密文都由 PulseOps 持久化到数据库；这满足产品内加密和误读防护，但不是 KMS 级安全隔离。

### 7.8 配置校验

配置发布前必须两级校验：

1. PulseOps schema 校验：类型、必填、范围、枚举、正则、class 引用、资产引用、secret 引用、任务覆盖权限。
2. 插件自定义校验：若 schema 声明 `validate_action`，PulseOps 调用插件的 `validate_config`。

`validate_config` 请求使用同一套 runtime envelope：

```json
{
  "protocol": "pulseops.plugin/v1",
  "plugin_id": "@pulseops/grpc-source",
  "capability": "grpc",
  "action": "validate_config",
  "config": {
    "endpoint": "inventory.service:9090",
    "schema_mode": "proto_files",
    "service": "yym.inventory.v1.InventoryService"
  },
  "input": {
    "scope": "capability_config",
    "assets": [
      {
        "field": "proto_files",
        "asset_id": "inventory-proto",
        "version": 4
      }
    ]
  }
}
```

gRPC 官方插件必须在 `validate_config` 中支持：

- endpoint dial 检查。
- reflection 可用性检查。
- descriptor set 解析。
- proto files 编译。
- service/method 查找。
- 可选的 request dry-run 或真实试调用。

### 7.9 任务引用和覆盖

任务中引用配置实例，而不是重复填写整套连接配置：

```json
{
  "type": "grpc",
  "alias": "inventory",
  "plugin_config_ref": "grpc-prod-common",
  "capability_config_ref": "inventory-query",
  "overrides": {
    "method": "GetInventory",
    "request": {
      "user_id": "{{ .Run.Labels.user_id }}"
    }
  },
  "on_error": "fail"
}
```

后端保存任务时必须校验：

- 引用的配置实例存在且有 active version。
- override 字段在 schema 中存在。
- override 字段显式 `overridable: true`。
- override 值通过相同类型系统校验。
- secret/file override 使用引用，不内嵌明文或文件内容。

## 8. 外部运行时协议

外部插件推荐两种运行时：

- `process`：PulseOps 启动子进程，通过 stdin/stdout 传 JSON envelope。适合内部插件、私有部署、低延迟任务。
- `http`：PulseOps 调用插件服务 HTTP endpoint。适合独立部署、跨语言、资源隔离更强的插件。

旧 AI `.so` 不作为 V1 插件运行时。V1 不直接加载旧 `plugin_name` / `plugin_fetch` / `plugin_free` C ABI 插件；存量能力需要重新包装为 `process`、`http` 或官方 bundled adapter，并通过 `pulseops.plugin.yaml` 声明 capability、权限和配置 schema。

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

### 8.1 数据源协议适配器

`data_source` 是平台通用能力，负责把外部系统数据标准化为 JSON-like 数据对象，供 `data_process`、`ai_analyze` 和后续 `scenario_check` 复用。

第一批建议支持的数据源协议：

| protocol | 用途 | 运行方式 |
| --- | --- | --- |
| `http_json` | 调 HTTP/REST 接口并解析 JSON | builtin adapter |
| `grpc` | 调 gRPC Unary 方法并把响应转为 JSON | builtin adapter |
| `run_record` | 读取 PulseOps 任务运行记录、summary、payload、artifact | builtin adapter |
| `process` | 调插件子进程，由插件自行采集数据 | process runtime |
| `http_plugin` | 调插件服务 HTTP endpoint，由插件自行采集数据 | http runtime |

gRPC 数据源不要求插件作者写 Go 代码。插件可以只声明一个 `protocol: grpc` 的 `data_source`，PulseOps 使用内置 gRPC adapter 执行调用。

gRPC 数据源 manifest 最小示例：

```yaml
data_sources:
  - name: grpc_game_inventory
    title: gRPC 游戏库存查询
    protocol: grpc
    runtime: builtin
    permissions:
      - network:outbound
      - grpc:call
    config:
      title: gRPC 调用配置
      validate_action: validate_config
      fields:
        service:
          type: string
          required: true
          overridable: true
        method:
          type: string
          required: true
          overridable: true
        request:
          type: object
          class: JSONObject
          required: true
          overridable: true
```

任务中引用示例：

```json
{
  "type": "grpc_game_inventory",
  "alias": "inventory",
  "plugin_config_ref": "grpc-prod-common",
  "capability_config_ref": "inventory-query",
  "overrides": {
    "method": "GetInventory",
    "request": {
      "user_id": "{{ .Run.Labels.user_id }}"
    }
  },
  "on_error": "fail"
}
```

gRPC adapter 约束：

- 第一阶段只支持 Unary RPC；server streaming、client streaming、bidirectional streaming 后续单独设计。
- 优先使用 gRPC reflection；生产环境也允许通过配置资产引用 descriptor set 或 proto files，避免依赖线上 reflection。
- 请求体用 JSON 表达，adapter 根据 proto schema 转换为 protobuf message。
- 响应统一转为 JSON object，写入 `DataSources.<alias>`。
- 支持 TLS、mTLS、metadata、deadline、max receive bytes。
- metadata 中的 token、authorization 等敏感字段必须走 `secret` 字段和 secret 引用，不直接落 manifest 或任务定义。
- gRPC 调用错误需要标准化为 `{code,message,retryable,details}`，并遵守数据源的 `on_error` 策略。

## 9. 权限和安全

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
- 插件不直接读取主配置中的密钥，使用插件配置系统中的 `secret` 字段和 secret 引用。
- 每次调用都有 timeout 和最大输出字节数。
- process 插件工作目录固定在插件包目录。
- 插件 stdout/stderr 纳入运行留痕，但需要截断。
- 插件加载失败只标记 plugin/capability error，不阻断平台启动，除非 `[plugins].strict = true`。

## 10. 全局配置

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

迁移策略：

- 插件系统 V1 不兼容 `pulseops.plugin.toml`，只扫描和加载 `pulseops.plugin.yaml`。
- 旧 AI C ABI 插件不作为兼容目标；若业务必须保留，按新插件模型重新包装成 `ai_data_source` 或通用 `data_source` capability。
- 若 `[plugins].dir` 未配置，默认仍使用 `plugins`。

## 11. 后端 API

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

插件配置 API：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/plugins/{id}/config-schema` | 获取插件级配置 schema、class 和 UI schema |
| `GET` | `/api/plugin-capabilities/{capability_id}/config-schema` | 获取 capability 级配置 schema |
| `GET` | `/api/plugin-configs` | 按 plugin/capability 查询配置实例 |
| `POST` | `/api/plugin-configs` | 创建插件级或 capability 级配置实例 |
| `GET` | `/api/plugin-configs/{instance_id}` | 获取配置实例详情、active version 和版本列表 |
| `POST` | `/api/plugin-configs/{instance_id}/versions` | 基于当前 active 或空白 schema 创建 draft version |
| `PUT` | `/api/plugin-configs/{instance_id}/versions/{version}` | 更新 draft version 的配置值 |
| `POST` | `/api/plugin-configs/{instance_id}/versions/{version}/validate` | 执行 schema 校验和插件 `validate_config` |
| `POST` | `/api/plugin-configs/{instance_id}/versions/{version}/activate` | 激活配置版本 |
| `POST` | `/api/plugin-configs/{instance_id}/disable` | 禁用配置实例，阻止新任务引用 |
| `POST` | `/api/plugin-assets` | 创建文件资产，需指定 `scope`、`plugin_id`、可选 `capability_id` 或 `config_instance_id` |
| `POST` | `/api/plugin-assets/{asset_id}/versions` | 上传新的资产版本 |
| `POST` | `/api/plugin-assets/{asset_id}/versions/{version}/validate` | 校验资产版本，例如 proto 编译、证书解析 |
| `POST` | `/api/plugin-assets/{asset_id}/versions/{version}/activate` | 激活资产版本 |
| `POST` | `/api/plugin-secrets` | 创建或更新 secret，返回 masked 引用 |
| `GET` | `/api/plugin-secrets/{secret_id}` | 获取 secret masked 元数据，不返回明文 |

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
| `plugin_config_instances` | 插件级或 capability 级配置实例身份、归属、状态 |
| `plugin_config_versions` | 配置版本、状态、配置值、校验结果、active/retired 时间 |
| `plugin_assets` | 文件资产身份、kind、scope、插件/capability/配置实例归属、状态 |
| `plugin_asset_versions` | 资产版本、存储位置、checksum、状态、校验结果 |
| `plugin_secrets` | secret 身份、归属、masked 元数据 |
| `plugin_secret_values` | secret 密文、加密元数据、更新时间 |
| `plugin_config_events` | 配置、资产、secret 的 create/update/validate/activate/disable 审计事件 |

`kv_metadata` 只可用于早期本地开发或 catalog-only 原型；只要要求不停机更新，就必须有 release/generation 级别的持久状态。

## 12. 前端设计

新增一级页面：`插件中心`。

页面模块：

- 概览：总插件数、启用数、错误数、能力数。
- 插件列表：名称、active version、状态、draining 数、能力数量、权限、最近错误。
- 插件详情：manifest 信息、release 列表、generation 引用、能力列表、权限、入口文件、默认配置。
- 配置实例：插件级配置和 capability 级配置的创建、编辑、校验、激活、禁用、版本对比。
- 资产管理：上传共享资产和配置实例私有资产，展示资产版本、checksum、引用关系和校验结果。
- Secret 管理：录入、更新、masked 展示和引用 secret，不展示明文。
- 操作：安装 release、校验、激活、回滚、禁用、卸载、GC。
- 能力视图：按 `task_template`、`task_driver`、`data_source`、`ai_data_source`、`hook` 分组。
- 发布状态：展示当前 active generation、旧 generation refcount、release 是否可删除。

任务创建向导改造：

- 模板卡片从内置 `KIND_OPTIONS` 扩展为后端返回的 `task_template`。
- 通用数据源类型从硬编码数组扩展为 `/api/plugin-capabilities?type=data_source`。
- `ai_analyze` 继续读取 `/api/plugin-capabilities?type=ai_data_source`，但优先复用通用 `data_source`。
- 插件提供的配置和参数表单使用 `pulseops.plugin.yaml` 中的声明式 UI schema 渲染，不执行插件 JS。
- 插件模板可声明推荐触发方式、默认 labels、默认 params 和是否支持依赖触发。
- 数据源类任务优先选择配置实例，再填写 schema 允许覆盖的字段。
- 保存任务时后端校验配置实例、active version、覆盖字段和 secret/file 引用。

平台设置页改造：

- AI 配置中的“插件目录”迁移到插件中心。
- 设置页只保留全局开关和运行安全配置。
- 插件配置、资产、secret 归入插件中心或独立“插件配置”页，不放在平台全局设置页。

## 13. 零停机上下架与更新方案

### 13.1 零停机不变量

插件系统必须满足以下不变量：

1. PulseOps 主进程不因插件安装、启用、禁用、升级、回滚而重启。
2. 新 release 校验失败时，当前 active release 和当前 registry generation 不变。
3. 任意运行中的任务只引用它启动时绑定的 registry generation，不被中途切换。
4. 新任务和新 run 只使用当前 active generation。
5. 旧 release 进入 `draining` 后不再接收新 run，但必须允许已有 run 正常结束。
6. 只有当旧 release 的引用计数为 0，且超过最小保留时间后，才允许 GC 删除。
7. 插件下架不等于立即杀死运行中插件进程；默认行为是停止新流量并等待 draining。
8. 所有 activate、disable、rollback 都必须有审计事件，并可在插件中心看到结果。
9. 配置版本 activate 不重启 PulseOps；新 run 使用新的 active 配置版本，运行中的 run 使用启动时绑定的旧配置版本。
10. 资产版本 activate 不要求重新保存配置；新 run 解析到新的 active 资产版本，但必须记录实际使用的 asset version。

### 13.2 状态机

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

### 13.3 Registry Generation

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

### 13.4 上架流程

上架指引入一个新插件或一个新 release，但不影响线上流量。

流程：

1. 将插件写入临时目录：`plugins/.staging/<plugin-id>/<version>/`。
2. 计算 checksum，校验目录完整性。
3. 解析 `pulseops.plugin.yaml`，校验 `id`、`version`、schema version、能力唯一性。
4. 原子重命名到 `plugins/<plugin-id>/releases/<version>/`。
5. 写入 `plugin_releases`，状态为 `staged`。
6. 运行 validate：manifest、权限、入口文件、process 可执行位、http endpoint 格式、schema。
7. 对 process/http 插件执行 readiness check。
8. 校验通过后状态变为 `validated`。

上架完成后，新 release 只出现在插件中心，不会自动承接新 run。必须显式 activate 才会上线。

### 13.5 更新流程

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

### 13.6 下架流程

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

### 13.7 回滚流程

回滚是一次特殊 activate，不是恢复旧目录：

1. 从 `plugin_releases` 找到最近一个 `validated`、`draining` 或 `retired` 且文件仍存在的旧 release。
2. 对旧 release 重新执行快速 readiness check。
3. 构建候选 generation。
4. CAS 更新 active version 指向旧 release。
5. 原子替换 active generation。
6. 当前坏版本进入 `draining` 或 `failed`。

回滚要求旧 release 在 `retention` 内不被 GC。默认建议保留最近 3 个 release 或至少保留 7 天。

### 13.8 旧 C ABI 迁移约束

旧 AI `.so` 插件不进入 V1 标准运行时，也不承诺旧插件包原样可用。原因是 `.so` 动态加载和卸载无法提供稳定的零停机、隔离和资源回收语义。

迁移要求：

- 旧能力需要按 `pulseops.plugin.yaml` 重新声明身份、权限、能力和配置 schema。
- 推荐改造成 `process` 或 `http` runtime。
- 若确需保留 native 代码，只能作为 PulseOps 官方 bundled adapter 的内部实现细节，不暴露为第三方插件兼容协议。
- 需要强零停机和可回收能力的插件，必须使用 process/http runtime。

### 13.9 并发和一致性

发布操作必须串行化到插件级别：

- 同一个 `plugin_id` 同一时刻只允许一个 install/activate/disable/rollback。
- 不同插件可以并行 validate，但 activate 时必须对全局 generation 做一次能力冲突校验。
- active version 更新使用 DB 事务和 CAS 条件，避免两个发布互相覆盖。
- `/api/plugins/reload` 只能刷新 catalog 视图，不能绕过 activate 流程直接上线 release。

任务运行一致性：

- run record 中记录 `plugin_generation_id`。
- 插件调用日志中记录 `plugin_id`、`plugin_version`、`capability_id`。
- run record 中记录插件级配置版本、capability 配置版本和实际资产版本。
- 任务详情页能展示“当前任务引用的插件能力是否仍 active”。

### 13.10 场景矩阵

| 场景 | 入口操作 | 对新 run 的影响 | 对运行中 run 的影响 | 失败处理 |
| --- | --- | --- | --- | --- |
| 新插件上架 | install + validate | 无影响，直到 activate 才可见 | 无影响 | release 停在 `failed` 或 `staged`，active generation 不变 |
| 新版本更新 | validate + activate | activate 成功后新 run 使用新 generation | 旧 run 继续使用旧 generation | validate/readiness/CAS 失败均不改变 active version |
| 临时下架 | disable | 新 run 不再使用该插件能力 | 旧 run 继续执行直到 draining 完成 | disable generation 构建失败则保持 enabled |
| 永久卸载 | uninstall | 仅允许 retired release 卸载 | 不允许卸载仍有引用的 release | refcount 不为 0 时拒绝 |
| 回滚 | rollback | 新 run 切回旧 release 的新 generation | 已开始的坏版本 run 不被强杀，按策略自然结束或任务级取消 | 旧 release readiness 失败则拒绝回滚 |
| 插件目录 reload | reload | 不切流，只刷新可见 release/catalog | 无影响 | 只记录 catalog error |

## 14. 生命周期

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

## 15. 运行观测

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

## 16. 与现有模块的关系

| 现有模块 | 改造方式 |
| --- | --- |
| `internal/task` | 保持 `Driver` 接口；当前内置 driver 迁入 `@pulseops/core-tasks`、`@pulseops/scenario`、`@pulseops/data-process` 官方插件 |
| `internal/ai` | 迁入 `@pulseops/ai` 官方插件；移除旧 C ABI 插件直接加载入口；新增从 plugin catalog 注册数据源 |
| `internal/evaluator` | 将 evaluator 注册表纳入 capability registry；官方 evaluator 归入 `@pulseops/scenario` |
| `internal/trace` | `postgres`、`webhook` sink 归入 `@pulseops/trace-sinks` 官方插件 |
| `internal/app` | 不再直接拼 `driverList` 注册；只注册 bundled official 插件并从 active generation 取得 driver/evaluator/source registry |
| `internal/api` | 新增 plugin catalog、配置实例、配置版本、资产和 secret API，不改变现有任务 API 的基础路径 |
| `internal/store` | 新增插件 release/generation 表，以及配置实例、配置版本、资产版本、secret 密文和审计表 |
| `web/src/components/task-form` | 从 API 读取模板、`data_source` 能力和配置实例；只允许编辑 schema 声明可覆盖的字段 |
| `web/src/pages/Settings.tsx` | 插件目录迁移到插件中心或平台配置摘要；插件配置、资产、secret 不放在平台设置页 |

## 17. 实施计划

### 阶段 1：Catalog 和插件中心

- 新增 `internal/plugin` 包。
- 支持扫描 `plugins/*/releases/*/pulseops.plugin.yaml`。
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

### 阶段 2：插件配置服务

- `pulseops.plugin.yaml` parser 支持 `config_classes`、插件级 `config`、capability 级 `config`。
- 实现严格类型系统：`string`、`number`、`bool`、`select`、`multi_select`、`object`、`array`、`file`、`secret`。
- `object` 支持 class，`array` 支持 `items`，class 只允许插件内复用。
- 实现声明式 UI schema 渲染，不执行插件 JS。
- 新增配置实例、配置版本、资产、资产版本、secret 加密存储和审计表。
- 配置版本支持 `draft -> validated -> active`，修改 active 时创建新 draft。
- 支持 `validate_config` runtime action，先 schema 校验，再插件自定义校验。
- 任务保存时校验配置实例引用、active version、任务覆盖权限和 override 类型。

验收：

- 插件可以只通过 YAML 声明配置页面，前端能渲染字段、分组、条件展示和校验提示。
- 创建配置实例后，保存为 draft，validate 通过后才能 activate。
- secret 只在保存时提交明文，后续 API 只返回 masked 值。
- 共享文件资产上传后可被多个配置实例和多个配置版本引用；配置实例私有资产只能被所属实例引用。
- 资产更新不要求重新保存配置，新 run 使用 active asset version，并记录实际 asset version。
- run 记录实际使用的配置版本和资产版本。
- gRPC 插件能通过配置实例上传/引用 proto 或 descriptor，并在 `validate_config` 中完成编译或解析校验。

### 阶段 3：模板和数据源接入

- 任务创建向导消费 `task_template`。
- 数据源下拉消费 `data_source` 和配置实例，AI 分析继续消费 `ai_data_source` capability。
- 新增内置 `grpc` 数据源 adapter，支持 Unary RPC、reflection/descriptor、TLS、metadata、deadline。
- 旧 C ABI AI 数据源不作为兼容目标；如确需保留，另做官方迁移 adapter。
- 支持 process/http AI 数据源 adapter。
- process/http runtime 接入 readiness check 和 worker pool draining。

验收：

- 插件新增通用数据源后，任务表单可选择。
- 配置 `protocol: grpc` 的数据源后，`data_process` 和 `ai_analyze` 都可以通过 alias 读取 gRPC 响应 JSON。
- 任务可以引用 gRPC 插件配置实例，并覆盖 schema 允许覆盖的 `service`、`method`、`request` 等字段。
- 禁用插件后，新建/保存依赖该数据源的任务会校验失败。
- 升级 AI 数据源插件时，正在执行的 `ai_analyze` run 继续使用旧 generation，新 run 使用新 generation。
- 新版本 readiness 失败时，不改变当前 active version。

### 阶段 4：外部任务驱动

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

### 阶段 5：Hook、Evaluator、Output Writer

- 引入事件总线：`run.started`、`run.finished`、`task.updated`、`plugin.loaded`。
- 支持外部 evaluator。
- 支持 AI output writer 扩展。
- 支持 trace/notification sink 扩展。

验收：

- 插件可监听 run finished 并发送通知。
- 插件 evaluator 可被 `scenario_check` 引用。

### 阶段 6：治理增强

- 插件签名和 checksum。
- 插件安装/升级审计表。
- 插件调用限流。
- 插件包导入/导出。

## 18. 关键风险

| 风险 | 处理 |
| --- | --- |
| 插件破坏主进程稳定性 | 默认 process/http 隔离；`.so` 不作为 V1 第三方插件运行时 |
| 插件权限过大 | manifest 权限声明 + allowlist + 配置系统 secret 引用 |
| 插件表单不可维护 | 使用 `pulseops.plugin.yaml` 的声明式 UI schema，不执行任意前端 JS |
| YAML 表达过于自由 | 只支持受限 YAML 数据子集；加载后 normalize，再做 manifest/schema 校验 |
| 配置变更影响历史 run | 配置实例版本化，run 绑定配置版本；资产独立版本化并记录实际 asset version |
| secret 泄露 | DB 只保存密文，API 默认返回 masked 值；明文只在 validate/run 内存流程出现 |
| 插件 reload 影响运行中任务 | registry generation 不可变，run 启动时绑定 generation，旧 release draining 后再回收 |
| 插件更新时原地覆盖文件 | release 目录不可变，新版本必须写入新目录，active version 通过 DB CAS 切换 |
| bundled official 插件被误认为可单独热更新代码 | 文档和 UI 明确区分 `bundled_official` 与 `external_official`；需要独立发版的官方能力迁移到 external release 包 |
| C ABI `.so` 无法可靠卸载 | 不提供旧插件兼容入口；强零停机能力使用 process/http runtime |
| 插件能力冲突 | capability id 全局唯一，冲突进入 error 状态 |
| 目录不存在导致启动失败 | 默认 warning，不阻断；strict=true 才失败 |

## 19. 推荐决策

建议按以下决策推进：

1. 插件系统 V1 只支持 `pulseops.plugin.yaml`，不提供 `pulseops.plugin.toml` 兼容层。
2. 第一阶段必须同时落下 release/generation/draining 状态底座，否则后续零停机更新会返工。
3. 现有内置任务先迁为 `bundled_official` 官方插件，从 catalog 和 active generation 注册。
4. 需要独立零停机更新代码的官方能力，再迁为 `external_official`。
5. 外部运行时优先支持 `process` 和 `http`，不要扩展通用 Go `.so`。
6. 插件配置系统必须作为 V1 核心能力：插件级配置、capability 级配置、class 类型系统、声明式 UI、版本化配置、共享/私有资产版本和 DB 加密 secret。
7. 任务创建向导优先接入 `task_template`、通用 `data_source` 和配置实例，AI 分析复用同一套数据源能力。
8. 配置覆盖默认禁止，只允许 schema 显式 `overridable: true` 的字段被任务覆盖。
9. V1 支持插件/能力共享资产和配置实例私有资产，但不做跨插件平台级共享资产；资产独立版本化并由配置版本引用，配置版本 activate 和资产版本 activate 互不强绑定。
10. 插件加载失败默认 degraded，不阻断核心平台启动。
