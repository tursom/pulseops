import type { Node, Edge } from '@xyflow/react'

export interface TaskNodeData extends Record<string, unknown> {
  taskId: string
  name: string
  kind: string
  enabled: boolean
  status?: string
  lastRunStatus?: string
  onRefresh?: () => void
}

export interface DependencyEdgeData extends Record<string, unknown> {
  condition?: string
}

export type TaskNodeType = Node<TaskNodeData, 'taskNode'>
export type DependencyEdgeType = Edge<DependencyEdgeData, 'dependencyEdge'>
