import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  ReactFlow,
  Controls,
  Background,
  BackgroundVariant,
  useNodesState,
  useEdgesState,
  MarkerType,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

import { fetchTaskDefinitions, fetchTasks } from '../../api/client'
import type { TaskDefinition, TaskState } from '../../api/types'
import TaskNode from './TaskNode'
import DependencyEdge from './DependencyEdge'
import type { TaskNodeType, DependencyEdgeType } from './types'

const NODE_TYPES = { taskNode: TaskNode }
const EDGE_TYPES = { dependencyEdge: DependencyEdge }

const COLUMN_GAP = 300
const ROW_GAP = 80
const START_X = 100
const START_Y = 50

interface GraphData {
  nodes: TaskNodeType[]
  edges: DependencyEdgeType[]
}

function buildGraph(
  defs: TaskDefinition[],
  states: TaskState[],
): GraphData {
  const statusMap = new Map(states.map((s) => [s.task_id, s]))
  const nodes: TaskNodeType[] = []
  const edges: DependencyEdgeType[] = []

  for (const def of defs) {
    const state = statusMap.get(def.task_id)
    nodes.push({
      id: def.task_id,
      type: 'taskNode',
      position: { x: 0, y: 0 },
      data: {
        taskId: def.task_id,
        name: def.name,
        kind: def.kind,
        enabled: def.enabled,
        status: state?.status,
        lastRunStatus: state?.last_run_status,
      },
    })
  }

  for (const def of defs) {
    if (def.trigger === 'on_run' && def.watch_task_id !== '') {
      edges.push({
        id: `dep-${def.watch_task_id}->${def.task_id}`,
      type: 'dependencyEdge' as const,
        source: def.watch_task_id,
        target: def.task_id,
        data: { condition: def.watch_condition || undefined },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 14,
          height: 14,
          color: '#faad14',
        },
      })
    }
  }

  const adjacency = new Map<string, string[]>()
  for (const n of nodes) {
    adjacency.set(n.id, [])
  }
  for (const e of edges) {
    adjacency.get(e.source)?.push(e.target)
  }

  const depth = new Map<string, number>()
  const inDegree = new Map<string, number>()
  for (const n of nodes) {
    inDegree.set(n.id, 0)
  }
  for (const e of edges) {
    inDegree.set(e.target, (inDegree.get(e.target) || 0) + 1)
  }

  const queue: string[] = []
  for (const n of nodes) {
    if ((inDegree.get(n.id) || 0) === 0) {
      depth.set(n.id, 0)
      queue.push(n.id)
    }
  }

  while (queue.length > 0) {
    const current = queue.shift()!
    const currentDepth = depth.get(current)!
    for (const neighbor of adjacency.get(current) || []) {
      const newDepth = currentDepth + 1
      if (!depth.has(neighbor) || depth.get(neighbor)! < newDepth) {
        depth.set(neighbor, newDepth)
      }
      const deg = (inDegree.get(neighbor) || 1) - 1
      inDegree.set(neighbor, deg)
      if (deg === 0) {
        queue.push(neighbor)
      }
    }
  }

  for (const n of nodes) {
    if (!depth.has(n.id)) {
      depth.set(n.id, 0)
    }
  }

  const columns = new Map<number, TaskNodeType[]>()
  for (const n of nodes) {
    const d = depth.get(n.id)!
    if (!columns.has(d)) {
      columns.set(d, [])
    }
    columns.get(d)!.push(n)
  }

  for (const [col, colNodes] of columns.entries()) {
    const x = START_X + col * COLUMN_GAP
    let y = START_Y
    for (const node of colNodes) {
      node.position = { x, y }
      y += ROW_GAP
    }
  }

  return { nodes, edges }
}

export default function PipelineCanvas() {
  const [nodes, setNodes, onNodesChange] = useNodesState<TaskNodeType>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<DependencyEdgeType>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const loadData = useCallback(async () => {
    try {
      const [defs, states] = await Promise.all([
        fetchTaskDefinitions(),
        fetchTasks(),
      ])
      const graph = buildGraph(defs, states)
      setNodes(graph.nodes)
      setEdges(graph.edges)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load pipeline data')
    } finally {
      setLoading(false)
    }
  }, [setNodes, setEdges])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, 10_000)
    return () => clearInterval(interval)
  }, [loadData])

  const defaultEdgeOptions = useMemo(
    () => ({
      type: 'dependencyEdge',
      markerEnd: {
        type: MarkerType.ArrowClosed,
        width: 14,
        height: 14,
        color: '#faad14',
      },
    }),
    [],
  )

  if (loading) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          height: '60vh',
          color: '#8c8c8c',
          fontSize: 14,
        }}
      >
        Loading pipeline...
      </div>
    )
  }

  if (error) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          height: '60vh',
          color: '#ff4d4f',
          fontSize: 14,
        }}
      >
        {error}
      </div>
    )
  }

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      nodeTypes={NODE_TYPES}
      edgeTypes={EDGE_TYPES}
      defaultEdgeOptions={defaultEdgeOptions}
      fitView
      fitViewOptions={{ padding: 0.2 }}
      attributionPosition="bottom-left"
      style={{ background: '#f5f5f5' }}
    >
      <Controls />
      <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#d9d9d9" />
    </ReactFlow>
  )
}
