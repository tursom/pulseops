import type { TaskDependency } from '../api/types'

export const TOPOLOGY_SOURCE_KEY = 'upstream'
export const TOPOLOGY_DEFAULT_CONDITION = 'run_status == success'

export const TASK_CAPABILITIES: Record<string, { dependency: boolean; dataSources: boolean }> = {
  http_check: { dependency: false, dataSources: false },
  tcp_check: { dependency: false, dataSources: false },
  script_exec: { dependency: false, dataSources: false },
  process_check: { dependency: false, dataSources: false },
  scenario_check: { dependency: false, dataSources: false },
  data_process: { dependency: true, dataSources: true },
  ai_analyze: { dependency: true, dataSources: true },
}

export const DEPENDENCY_CAPABLE_KINDS = Object.entries(TASK_CAPABILITIES)
  .filter(([, capability]) => capability.dependency)
  .map(([kind]) => kind)

export interface TopologyTaskSeed {
  upstreamTaskId?: string
  upstreamName?: string
}

export interface TaskCreationContext {
  source: 'topology'
  lockedPipelineId?: string
  lockedUpstreamTaskId?: string
  lockedUpstreamName?: string
  recommendedKinds?: string[]
}

export function isDependencyCapableKind(kind?: string | null): boolean {
  return Boolean(kind && TASK_CAPABILITIES[kind]?.dependency)
}

export function buildTopologyTaskName(kind: string, seed: TopologyTaskSeed): string {
  const upstream = seed.upstreamName || seed.upstreamTaskId || '上游任务'
  if (kind === 'ai_analyze') return `${upstream} AI 分析`
  if (kind === 'data_process') return `${upstream} 数据处理`
  return `${upstream} 下游任务`
}

export function buildTopologyDependency(taskID: string, upstreamTaskId: string): TaskDependency {
  return {
    id: '',
    upstream_task_id: upstreamTaskId,
    downstream_task_id: taskID,
    condition: TOPOLOGY_DEFAULT_CONDITION,
    source_key: TOPOLOGY_SOURCE_KEY,
  }
}

export function buildTopologyParams(kind: string, seed: TopologyTaskSeed): Record<string, unknown> {
  if (!seed.upstreamTaskId) return {}

  if (kind === 'data_process') {
    return {
      source_task_id: seed.upstreamTaskId,
      extract_exprs: [
        {
          field: 'upstream_summary',
          source_key: TOPOLOGY_SOURCE_KEY,
          source: 'summary',
          jq_expr: '.',
          agg_mode: '',
        },
      ],
    }
  }

  if (kind === 'ai_analyze') {
    const upstream = seed.upstreamName || seed.upstreamTaskId || '上游任务'
    return {
      analysis_type: 'diagnose',
      data_sources: [
        {
          type: 'upstream_output',
          alias: TOPOLOGY_SOURCE_KEY,
          config: { task_id: seed.upstreamTaskId },
          on_error: 'fail',
        },
      ],
      prompt: {
        text: `分析 ${upstream} 的上游任务输出：\n{{json .DataSources.${TOPOLOGY_SOURCE_KEY}}}\n\n请判断是否存在异常，并给出简洁结论。`,
      },
      outputs: [
        {
          type: 'summary',
          config: { field: 'ai_analysis' },
        },
      ],
    }
  }

  return {}
}
