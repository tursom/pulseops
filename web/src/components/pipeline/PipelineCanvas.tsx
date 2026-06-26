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
import { Modal, Select, Card, Empty, message, Space, Button, Drawer, Descriptions, Tag, Typography, Segmented } from 'antd'
import { useNavigate } from 'react-router-dom'

import { fetchPipelineTasks, fetchTasks, updateTaskDefinition } from '../../api/client'
import type { TaskDefinition, TaskState } from '../../api/types'
import TaskNode from './TaskNode'
import DependencyEdge from './DependencyEdge'
import type { TaskNodeType, DependencyEdgeType } from './types'
import { KIND_COLORS, KIND_LABELS, runStatusColor, RUN_STATUS_LABELS } from '../../utils/pulseops'

const { Text } = Typography

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

type StatusFilter = 'all' | 'abnormal' | 'disabled'

export default function PipelineCanvas({ pipelineId }: Props) {
  const navigate = useNavigate()
  const [nodes, setNodes, onNodesChange] = useNodesState<TaskNodeType>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<DependencyEdgeType>([])
  const [allNodes, setAllNodes] = useState<TaskNodeType[]>([])
  const [allEdges, setAllEdges] = useState<DependencyEdgeType[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [selectedNode, setSelectedNode] = useState<TaskNodeType | null>(null)
  const [selectedEdge, setSelectedEdge] = useState<DependencyEdgeType | null>(null)

  const taskDefsRef = useRef<TaskDefinition[]>([])
  const loadDataRef = useRef<() => Promise<void>>(async () => {})

  const [depEditorOpen, setDepEditorOpen] = useState(false)
  const [depEditorSource, setDepEditorSource] = useState('')
  const [depEditorTarget, setDepEditorTarget] = useState('')
  const [depEditorCondition, setDepEditorCondition] = useState('')

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

      setAllNodes(graph.nodes)
      setAllEdges(graph.edges)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载依赖拓扑失败')
    } finally {
      setLoading(false)
    }
  }, [pipelineId, setNodes, setEdges])

  useEffect(() => {
    loadDataRef.current = loadData
  }, [loadData])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, 15_000)
    return () => clearInterval(interval)
  }, [loadData])

  useEffect(() => {
    let nextNodes = allNodes
    if (statusFilter === 'abnormal') {
      nextNodes = allNodes.filter((node) => {
        const run = node.data.lastRunStatus
        return run === 'failed' || run === 'timeout' || node.data.status === 'unloaded'
      })
    } else if (statusFilter === 'disabled') {
      nextNodes = allNodes.filter((node) => !node.data.enabled || node.data.status === 'disabled')
    }
    const nodeIDs = new Set(nextNodes.map((node) => node.id))
    setNodes(nextNodes)
    setEdges(allEdges.filter((edge) => nodeIDs.has(edge.source) && nodeIDs.has(edge.target)))
  }, [allEdges, allNodes, setEdges, setNodes, statusFilter])

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
      maskClosable: true,
      content: (
        <div>
          <p>
            将 <strong>{sourceDef.name}</strong> 设为 <strong>{targetDef.name}</strong> 的监听任务？
          </p>
          <Select
            allowClear
            placeholder="不限制（总是触发）"
            style={{ width: '100%' }}
            onChange={(v) => { condition = v || '' }}
            options={[
              { value: 'check_status == pass', label: '上游检查通过时触发' },
              { value: 'run_status == success', label: '上游运行成功时触发' },
            ]}
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
          message.error(err instanceof Error ? err.message : '创建依赖失败')
        }
      },
    })
  }, [loadData])

  const onEdgeClick = useCallback((_event: React.MouseEvent, edge: Edge) => {
    const defs = taskDefsRef.current
    const targetDef = defs.find(d => d.task_id === edge.target)
    if (!targetDef) return
    setDepEditorSource(edge.source)
    setDepEditorTarget(edge.target)
    setDepEditorCondition(targetDef.watch_condition || '')
    setDepEditorOpen(true)
  }, [])

  const handleSaveDependency = useCallback(async () => {
    const defs = taskDefsRef.current
    const targetDef = defs.find(d => d.task_id === depEditorTarget)
    if (!targetDef) return
    try {
      await updateTaskDefinition(depEditorTarget, {
        ...targetDef,
        watch_condition: depEditorCondition,
      })
      message.success('依赖已更新')
      loadData()
    } catch (err) {
      message.error(err instanceof Error ? err.message : '更新失败')
    }
    setDepEditorOpen(false)
  }, [depEditorTarget, depEditorCondition, loadData])

  const handleDeleteDependency = useCallback(async () => {
    const defs = taskDefsRef.current
    const targetDef = defs.find(d => d.task_id === depEditorTarget)
    if (!targetDef) return
    Modal.confirm({
      title: '删除依赖？',
      content: <p>移除到 <strong>{depEditorSource}</strong> 的依赖链接？</p>,
      okText: '删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await updateTaskDefinition(depEditorTarget, {
            ...targetDef,
            watch_task_id: '',
            trigger: 'scheduled',
            watch_condition: '',
          })
      message.success('依赖已移除')
          loadData()
        } catch (err) {
          message.error(err instanceof Error ? err.message : '删除失败')
        }
      },
    })
    setDepEditorOpen(false)
  }, [depEditorSource, depEditorTarget, loadData])

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

  const selectedTargetDef = selectedNode ? taskDefsRef.current.find((def) => def.task_id === selectedNode.id) : null
  const selectedUpstream = selectedTargetDef?.watch_task_id
    ? taskDefsRef.current.find((def) => def.task_id === selectedTargetDef.watch_task_id)
    : null
  const selectedDownstream = selectedNode
    ? taskDefsRef.current.filter((def) => def.watch_task_id === selectedNode.id)
    : []

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
          正在加载依赖拓扑...
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
          <Empty description="此任务组中暂无任务" />
        </Card>
      </div>
    )
  }

  return (
    <>
      <div
        style={{
          height: 44,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 12px',
          borderBottom: '1px solid #edf0f2',
          background: '#fff',
        }}
      >
        <Space>
          <Segmented
            size="small"
            value={statusFilter}
            onChange={(value) => setStatusFilter(value as StatusFilter)}
            options={[
              { label: '全部', value: 'all' },
              { label: '异常', value: 'abnormal' },
              { label: '禁用', value: 'disabled' },
            ]}
          />
          <Text type="secondary">节点 {nodes.length} / {allNodes.length}，依赖 {edges.length}</Text>
        </Space>
        <Space>
          <Button size="small" onClick={() => {
            const graph = buildGraph(taskDefsRef.current, [])
            setAllNodes((prev) => graph.nodes.map((node) => ({
              ...node,
              data: prev.find((item) => item.id === node.id)?.data || node.data,
            })))
            setAllEdges((prev) => prev)
          }}>
            自动布局
          </Button>
          <Button size="small" onClick={() => loadData()}>
            刷新
          </Button>
        </Space>
      </div>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeClick={(_, node) => {
          setSelectedNode(node as TaskNodeType)
          setSelectedEdge(null)
        }}
        onEdgeClick={(event, edge) => {
          onEdgeClick(event, edge)
          setSelectedEdge(edge as DependencyEdgeType)
          setSelectedNode(null)
        }}
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
        style={{ background: '#f7f9fb', height: 'calc(100% - 44px)', minHeight: 590 }}
        connectionLineStyle={{ stroke: '#faad14', strokeWidth: 1.5, strokeDasharray: '5,5' }}
      >
        <Controls />
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#d9d9d9" />
      </ReactFlow>

      <Drawer
        title={selectedNode ? '节点详情' : '依赖详情'}
        open={Boolean(selectedNode || selectedEdge)}
        onClose={() => {
          setSelectedNode(null)
          setSelectedEdge(null)
        }}
        width={420}
      >
        {selectedNode && (
          <Space direction="vertical" size={14} style={{ width: '100%' }}>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="任务名称">{selectedNode.data.name}</Descriptions.Item>
              <Descriptions.Item label="任务 ID"><Text code>{selectedNode.data.taskId}</Text></Descriptions.Item>
              <Descriptions.Item label="类型">
                <Tag color={KIND_COLORS[selectedNode.data.kind] || 'default'}>
                  {KIND_LABELS[selectedNode.data.kind] || selectedNode.data.kind}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="启用">{selectedNode.data.enabled ? '是' : '否'}</Descriptions.Item>
              <Descriptions.Item label="运行态">{selectedNode.data.status || '—'}</Descriptions.Item>
              <Descriptions.Item label="最近结果">
                {selectedNode.data.lastRunStatus
                  ? <Tag color={runStatusColor(selectedNode.data.lastRunStatus)}>{RUN_STATUS_LABELS[selectedNode.data.lastRunStatus] || selectedNode.data.lastRunStatus}</Tag>
                  : <Text type="secondary">—</Text>}
              </Descriptions.Item>
              <Descriptions.Item label="上游任务">
                {selectedUpstream ? selectedUpstream.name : <Text type="secondary">无</Text>}
              </Descriptions.Item>
              <Descriptions.Item label="下游任务">
                {selectedDownstream.length > 0
                  ? selectedDownstream.map((item) => <Tag key={item.task_id}>{item.name}</Tag>)
                  : <Text type="secondary">无</Text>}
              </Descriptions.Item>
            </Descriptions>
            <Space wrap>
              <Button type="primary" onClick={() => navigate(`/tasks/${selectedNode.data.taskId}?from=/pipelines/${pipelineId}`)}>
                查看详情
              </Button>
              <Button onClick={() => navigate(`/task-defs/${selectedNode.data.taskId}/edit?from=/pipelines/${pipelineId}`)}>
                编辑配置
              </Button>
              <Button onClick={() => navigate(`/task-defs/new?upstream_task_id=${selectedNode.data.taskId}&upstream_name=${encodeURIComponent(selectedNode.data.name)}&pipeline=${pipelineId}&from=/pipelines/${pipelineId}`)}>
                创建下游
              </Button>
            </Space>
          </Space>
        )}
        {selectedEdge && (
          <Descriptions column={1} size="small">
            <Descriptions.Item label="上游">{selectedEdge.source}</Descriptions.Item>
            <Descriptions.Item label="下游">{selectedEdge.target}</Descriptions.Item>
            <Descriptions.Item label="触发条件">{selectedEdge.data?.condition || '总是触发'}</Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>

      <Modal
        title="编辑依赖"
        open={depEditorOpen}
        onCancel={() => setDepEditorOpen(false)}
        maskClosable
        footer={
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <Button danger onClick={handleDeleteDependency}>删除依赖</Button>
            <Space>
              <Button onClick={() => setDepEditorOpen(false)}>取消</Button>
              <Button type="primary" onClick={handleSaveDependency}>保存</Button>
            </Space>
          </div>
        }
      >
        <p>
          <strong>监听任务:</strong> {depEditorSource}
        </p>
        <div style={{ marginTop: 12 }}>
          <p style={{ marginBottom: 6 }}><strong>触发条件:</strong></p>
          <Select
            allowClear
            placeholder="不限制（总是触发）"
            value={depEditorCondition || undefined}
            onChange={(v) => setDepEditorCondition(v || '')}
            style={{ width: '100%' }}
            options={[
              { value: 'check_status == pass', label: '上游检查通过时触发' },
              { value: 'run_status == success', label: '上游运行成功时触发' },
            ]}
          />
        </div>
      </Modal>
    </>
  )
}
