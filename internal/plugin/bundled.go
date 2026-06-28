package plugin

import (
	"pulseops/internal/evaluator"
	"pulseops/internal/task"
)

const BundledVersion = "0.1.0"

func CoreTasksPlugin(drivers ...task.Driver) BundledPlugin {
	return BundledPlugin{
		Manifest: Manifest{
			SchemaVersion: SchemaVersionV1,
			ID:            "@pulseops/core-tasks",
			Name:          "PulseOps 官方基础任务插件",
			Version:       BundledVersion,
			Description:   "提供 HTTP、TCP、脚本执行和进程检查任务驱动",
			Author:        "PulseOps",
			Enabled:       true,
			TaskDrivers: []NamedCapability{
				{Name: "http_check", Title: "HTTP 检查"},
				{Name: "tcp_check", Title: "TCP 检查"},
				{Name: "script_exec", Title: "脚本执行", Permissions: []string{"process:exec"}},
				{Name: "process_check", Title: "进程检查"},
			},
			TaskTemplates: []TaskTemplateManifest{
				{
					ID:          "http-check-basic",
					Kind:        "http_check",
					Title:       "HTTP 检查",
					Description: "检查 URL 可用性、状态码和响应内容",
					Defaults: map[string]any{
						"trigger": "scheduled",
						"timeout": "10s",
					},
					Params: map[string]any{
						"method":        "GET",
						"expect_status": []any{float64(200)},
					},
				},
				{
					ID:          "tcp-check-basic",
					Kind:        "tcp_check",
					Title:       "TCP 检查",
					Description: "检查 host:port TCP 连通性",
					Defaults: map[string]any{
						"trigger": "scheduled",
						"timeout": "5s",
					},
				},
				{
					ID:          "script-exec-basic",
					Kind:        "script_exec",
					Title:       "脚本执行",
					Description: "执行本地命令并采集 stdout/stderr",
					Defaults: map[string]any{
						"trigger": "manual",
						"timeout": "30s",
					},
				},
				{
					ID:          "process-check-basic",
					Kind:        "process_check",
					Title:       "进程检查",
					Description: "检查本机进程是否存在",
					Defaults: map[string]any{
						"trigger": "scheduled",
						"timeout": "5s",
					},
				},
			},
		},
		DefaultEnabled: true,
		Disableable:    true,
		Build: func() RuntimeRegistration {
			return RuntimeRegistration{Drivers: drivers}
		},
	}
}

func ScenarioPlugin(driver task.Driver, evaluators ...evaluator.ScenarioEvaluator) BundledPlugin {
	return BundledPlugin{
		Manifest: Manifest{
			SchemaVersion: SchemaVersionV1,
			ID:            "@pulseops/scenario",
			Name:          "PulseOps 官方场景巡检插件",
			Version:       BundledVersion,
			Description:   "提供场景巡检任务驱动和官方业务 evaluator",
			Author:        "PulseOps",
			Enabled:       true,
			TaskDrivers: []NamedCapability{
				{Name: "scenario_check", Title: "场景巡检"},
			},
			TaskTemplates: []TaskTemplateManifest{
				{
					ID:          "scenario-check-sampling",
					Kind:        "scenario_check",
					Title:       "场景巡检",
					Description: "采样、fanout 并使用 evaluator 判断业务一致性",
					Defaults: map[string]any{
						"trigger": "scheduled",
						"timeout": "30s",
					},
				},
			},
			Evaluators: []NamedCapability{
				{Name: "steam_game_price_consistency", Title: "Steam 游戏价格一致性"},
			},
		},
		DefaultEnabled: true,
		Disableable:    true,
		Build: func() RuntimeRegistration {
			return RuntimeRegistration{Drivers: []task.Driver{driver}, Evaluators: evaluators}
		},
	}
}

func DataProcessPlugin(driver task.Driver) BundledPlugin {
	return BundledPlugin{
		Manifest: Manifest{
			SchemaVersion: SchemaVersionV1,
			ID:            "@pulseops/data-process",
			Name:          "PulseOps 官方数据处理插件",
			Version:       BundledVersion,
			Description:   "提供上游运行数据提取、JQ 处理和聚合能力",
			Author:        "PulseOps",
			Enabled:       true,
			TaskDrivers: []NamedCapability{
				{Name: "data_process", Title: "数据处理"},
			},
			TaskTemplates: []TaskTemplateManifest{
				{
					ID:          "data-process-upstream",
					Kind:        "data_process",
					Title:       "上游数据处理",
					Description: "从上游任务运行结果提取字段并生成结构化摘要",
					Defaults: map[string]any{
						"trigger": "on_run",
						"timeout": "30s",
					},
				},
			},
		},
		DefaultEnabled: true,
		Disableable:    true,
		Build: func() RuntimeRegistration {
			return RuntimeRegistration{Drivers: []task.Driver{driver}}
		},
	}
}

func AIPlugin(enabled bool, driver task.Driver, aiEvaluator evaluator.ScenarioEvaluator) BundledPlugin {
	drivers := []task.Driver(nil)
	evaluators := []evaluator.ScenarioEvaluator(nil)
	if enabled && driver != nil {
		drivers = append(drivers, driver)
	}
	if enabled && aiEvaluator != nil {
		evaluators = append(evaluators, aiEvaluator)
	}
	return BundledPlugin{
		Manifest: Manifest{
			SchemaVersion: SchemaVersionV1,
			ID:            "@pulseops/ai",
			Name:          "PulseOps 官方 AI 分析插件",
			Version:       BundledVersion,
			Description:   "提供 AI 分析任务、内置 AI 数据源和输出写入方式",
			Author:        "PulseOps",
			Enabled:       enabled,
			Permissions:   []string{"runs:read", "ai:write"},
			TaskDrivers: []NamedCapability{
				{Name: "ai_analyze", Title: "AI 分析"},
			},
			TaskTemplates: []TaskTemplateManifest{
				{
					ID:          "ai-analyze-diagnose",
					Kind:        "ai_analyze",
					Title:       "AI 诊断分析",
					Description: "读取上游数据源并生成诊断结论",
					Defaults: map[string]any{
						"trigger": "on_run",
						"timeout": "45s",
					},
					Params: map[string]any{
						"analysis_type": "diagnose",
						"outputs": []any{
							map[string]any{"type": "summary", "config": map[string]any{"field": "ai_analysis"}},
						},
					},
				},
			},
			AIDataSources: []RuntimeCapability{
				{Name: "run_context", Title: "触发上下文", Runtime: "builtin"},
				{Name: "run_history", Title: "运行历史", Runtime: "builtin", Permissions: []string{"runs:read"}},
				{Name: "previous_analysis", Title: "历史分析", Runtime: "builtin", Permissions: []string{"ai:read"}},
				{Name: "http_call", Title: "HTTP 调用", Runtime: "builtin", Permissions: []string{"network:outbound"}},
			},
			OutputWriters: []NamedCapability{
				{Name: "summary", Title: "Summary"},
				{Name: "findings", Title: "Findings"},
				{Name: "artifact", Title: "Artifact", Permissions: []string{"artifacts:write"}},
			},
			Evaluators: []NamedCapability{
				{Name: "ai_evaluator", Title: "AI Evaluator"},
			},
		},
		DefaultEnabled:     enabled,
		Disableable:        true,
		ForceDefaultStatus: true,
		Build: func() RuntimeRegistration {
			return RuntimeRegistration{Drivers: drivers, Evaluators: evaluators}
		},
	}
}

func TraceSinksPlugin() BundledPlugin {
	return BundledPlugin{
		Manifest: Manifest{
			SchemaVersion: SchemaVersionV1,
			ID:            "@pulseops/trace-sinks",
			Name:          "PulseOps 官方留痕 Sink 插件",
			Version:       BundledVersion,
			Description:   "提供 Postgres 和 Webhook 留痕写入目标",
			Author:        "PulseOps",
			Enabled:       true,
			TraceSinks: []NamedCapability{
				{Name: "postgres", Title: "Postgres Sink"},
				{Name: "webhook", Title: "Webhook Sink", Runtime: "builtin", Permissions: []string{"network:outbound"}},
			},
		},
		DefaultEnabled: true,
		Disableable:    true,
	}
}

func GRPCSourcePlugin(enabled bool) BundledPlugin {
	return BundledPlugin{
		Manifest: Manifest{
			SchemaVersion: SchemaVersionV1,
			ID:            "@pulseops/grpc-source",
			Name:          "PulseOps 官方 gRPC 数据源插件",
			Version:       BundledVersion,
			Description:   "提供 gRPC Unary 通用数据源能力",
			Author:        "PulseOps",
			Enabled:       enabled,
			Permissions:   []string{"network:outbound", "grpc:call"},
			DataSources: []DataSourceManifest{
				{
					Name:     "grpc",
					Title:    "gRPC Unary",
					Protocol: "grpc",
					Runtime:  "builtin",
					Schema: Schema{
						"endpoint": {Type: "string", Required: true},
						"service":  {Type: "string", Required: true},
						"method":   {Type: "string", Required: true},
						"request":  {Type: "object", Required: true},
						"metadata": {Type: "object"},
						"tls":      {Type: "object"},
					},
				},
			},
		},
		DefaultEnabled:     enabled,
		Disableable:        true,
		ForceDefaultStatus: true,
	}
}
