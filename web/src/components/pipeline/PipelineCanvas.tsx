import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import {
  ReactFlow,
  Controls,
  Background,
  BackgroundVariant,
  useNodesState,
  useEdgesState,
  MarkerType,
} from '@xyflow/react'
import type { Connection, Edge } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Modal, Input, Card, Empty, message } from 'antd'

import { fetchPipelineTasks, fetchTasks, updateTaskDefinition } from '../../api/client'
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
        pipelineId: def.pipeline_id || undefined,
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

interface Props {
  pipelineId: string
}

export default function PipelineCanvas({ pipelineId }: Props) {
  const [nodes, setNodes, onNodesChange] = useNodesState<TaskNodeType>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<DependencyEdgeType>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const taskDefsRef = useRef<TaskDefinition[]>([])
  const loadDataRef = useRef<() => Promise<void>>(async () => {})

  const loadData = useCallback(async () => {
    try {
      const [defs, states] = await Promise.all([
        fetchPipelineTasks(pipelineId),
        fetchTasks(),
      ])
      taskDefsRef.current = defs
      const graph = buildGraph(defs, states)

      for (const node of graph.nodes) {
        node.data.onRefresh = () => { loadDataRef.current() }
      }

      setNodes(graph.nodes)
      setEdges(graph.edges)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load pipeline data')
    } finally {
      setLoading(false)
    }
  }, [pipelineId, setNodes, setEdges])

  useEffect(() => {
    loadDataRef.current = loadData
  }, [loadData])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, 10_000)
    return () => clearInterval(interval)
  }, [loadData])

  const onConnect = useCallback(async (connection: Connection) => {
    const sourceId = connection.source
    const targetId = connection.target
    if (!sourceId || !targetId) return
    if (sourceId === targetId) return

    const defs = taskDefsRef.current
    const sourceDef = defs.find(d => d.task_id === sourceId)
    const targetDef = defs.find(d => d.task_id === targetId)
    if (!sourceDef || !targetDef) return

    let condition = ''

    Modal.confirm({
      title: '创建依赖',
      content: (
        <div>
          <p>
            将 <strong>{sourceDef.name}</strong> 设为 <strong>{targetDef.name}</strong> 的监听任务？
          </p>
          <Input
            placeholder="触发条件（可选，如 check_status == 'fail'）"
            onChange={e => { condition = e.target.value }}
          />
        </div>
      ),
      onOk: async () => {
        try {
          await updateTaskDefinition(targetId, {
            ...targetDef,
            watch_task_id: sourceId,
            trigger: 'on_run',
            watch_condition: condition,
          })
          message.success('依赖已创建')
          loadData()
        } catch (err) {
          message.error(err instanceof Error ? err.message : 'Failed to create dependency')
        }
      },
    })
  }, [loadData])

  const onEdgeClick = useCallback(async (_event: React.MouseEvent, edge: Edge) => {
    const targetId = edge.target
    const sourceId = edge.source
    const defs = taskDefsRef.current
    const targetDef = defs.find(d => d.task_id === targetId)
    if (!targetDef) return

    let newCondition = targetDef.watch_condition || ''

    Modal.confirm({
      title: '编辑依赖',
      content: (
        <div>
          <p>
            <strong>watch_task:</strong> {sourceId}
          </p>
          <Input
            placeholder="触发条件（可选）"
            defaultValue={newCondition}
            onChange={e => { newCondition = e.target.value }}
          />
        </div>
      ),
      okText: '保存',
      cancelText: '删除依赖',
      onOk: async () => {
        try {
          await updateTaskDefinition(targetId, {
            ...targetDef,
            watch_condition: newCondition,
          })
          message.success('依赖已更新')
          loadData()
        } catch (err) {
          message.error(err instanceof Error ? err.message : 'Failed to update dependency')
        }
      },
      onCancel: async () => {
        Modal.confirm({
          title: '删除依赖？',
          content: <p>移除到 <strong>{sourceId}</strong> 的依赖链接？</p>,
          okText: '删除',
          okButtonProps: { danger: true },
          onOk: async () => {
            try {
              await updateTaskDefinition(targetId, {
                ...targetDef,
                watch_task_id: '',
                trigger: 'scheduled',
                watch_condition: '',
              })
              message.success('依赖已移除')
              loadData()
            } catch (err) {
              message.error(err instanceof Error ? err.message : 'Failed to remove dependency')
            }
          },
        })
      },
    })
  }, [loadData])

  const onEdgesDelete = useCallback(async (deletedEdges: Edge[]) => {
    const defs = taskDefsRef.current
    for (const edge of deletedEdges) {
      const targetDef = defs.find(d => d.task_id === edge.target)
      if (targetDef) {
        await updateTaskDefinition(targetDef.task_id, {
          ...targetDef,
          watch_task_id: '',
          trigger: 'scheduled',
          watch_condition: '',
        })
      }
    }
    message.success('依赖已移除')
    loadData()
  }, [loadData])

  const defaultEdgeOptions = useMemo(
    () => ({
      type: 'dependencyEdge' as const,
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

  if (nodes.length === 0) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          height: '100%',
          background: '#f5f5f5',
        }}
      >
        <Card>
          <Empty description="此管道中暂无任务" />
        </Card>
      </div>
    )
  }

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onConnect={onConnect}
      onEdgeClick={onEdgeClick}
      onEdgesDelete={onEdgesDelete}
      nodeTypes={NODE_TYPES}
      edgeTypes={EDGE_TYPES}
      defaultEdgeOptions={defaultEdgeOptions}
      fitView
      fitViewOptions={{ padding: 0.2 }}
      panOnScroll
      zoomOnPinch
      panOnScrollSpeed={1}
      selectionOnDrag
      attributionPosition="bottom-left"
      style={{ background: '#f5f5f5' }}
      connectionLineStyle={{ stroke: '#faad14', strokeWidth: 1.5, strokeDasharray: '5,5' }}
    >
      <Controls />
      <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#d9d9d9" />
    </ReactFlow>
  )
}
