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
import { Modal, Select, Card, Empty, message, Space, Button, Drawer, Descriptions, Tag, Typography, Segmented, Input } from 'antd'
import { useNavigate } from 'react-router-dom'

import { deleteTaskDependency, fetchPipelineTasks, fetchTaskGraph, updateTaskDefinition, upsertTaskDependency } from '../../api/client'
import type { TaskDefinition, TaskGraph, TaskGraphEdge } from '../../api/types'
import TaskNode from './TaskNode'
import DependencyEdge from './DependencyEdge'
import type { TaskNodeType, DependencyEdgeType } from './types'
import { KIND_COLORS, KIND_LABELS, runStatusColor, RUN_STATUS_LABELS, safeJson } from '../../utils/pulseops'

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

function buildGraph(graphData: TaskGraph): GraphData {
  const nodes: TaskNodeType[] = []
  const edges: DependencyEdgeType[] = []

  for (const node of graphData.nodes) {
    nodes.push({
      id: node.task_id,
      type: 'taskNode',
      position: { x: 0, y: 0 },
      data: {
        taskId: node.task_id,
        name: node.name,
        kind: node.kind,
        enabled: node.enabled,
        pipelineId: node.pipeline_id,
        status: node.runtime?.status,
        lastRunStatus: node.runtime?.last_run_status,
      },
    })
  }

  for (const edge of graphData.edges) {
    edges.push({
      id: edge.id,
      type: 'dependencyEdge' as const,
      source: edge.upstream_task_id,
      target: edge.downstream_task_id,
      data: {
        condition: edge.condition || undefined,
        sourceKey: edge.source_key,
        params: edge.params,
        dependencyId: edge.id,
        legacy: edge.legacy,
        valid: edge.valid,
        error: edge.error,
      },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        width: 14,
        height: 14,
        color: edge.valid ? '#faad14' : '#ff4d4f',
      },
    })
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
  const graphEdgesRef = useRef<TaskGraphEdge[]>([])
  const loadDataRef = useRef<() => Promise<void>>(async () => {})

  const [depEditorOpen, setDepEditorOpen] = useState(false)
  const [depEditorSource, setDepEditorSource] = useState('')
  const [depEditorTarget, setDepEditorTarget] = useState('')
  const [depEditorCondition, setDepEditorCondition] = useState('')
  const [depEditorSourceKey, setDepEditorSourceKey] = useState('')
  const [depEditorParams, setDepEditorParams] = useState('{}')

  const loadData = useCallback(async () => {
    try {
      const [defs, graphResponse] = await Promise.all([
        fetchPipelineTasks(pipelineId),
        fetchTaskGraph(pipelineId),
      ])
      taskDefsRef.current = defs
      graphEdgesRef.current = graphResponse.edges
      const graph = buildGraph(graphResponse)

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
  }, [pipelineId])

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
    let sourceKey = ''
    let params = '{}'

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
          <Input
            placeholder="数据源 Key，例如 upstream_a"
            style={{ marginTop: 12 }}
            onChange={(event) => { sourceKey = event.target.value }}
          />
          <Input.TextArea
            rows={4}
            placeholder='边参数 JSON，例如 {"headers":{"X-Env":"prod"}}'
            style={{ marginTop: 12 }}
            defaultValue="{}"
            onChange={(event) => { params = event.target.value }}
          />
        </div>
      ),
      onOk: async () => {
        try {
          const parsedParams = parseDependencyParams(params)
          await upsertTaskDependency({
            upstream_task_id: sourceId,
            downstream_task_id: targetId,
            condition,
            source_key: sourceKey.trim(),
            params: parsedParams,
          })
          message.success('依赖已创建')
          loadData()
        } catch (err) {
          message.error(err instanceof Error ? err.message : '创建依赖失败')
        }
      },
    })
  }, [loadData])

  const openDependencyEditor = useCallback((edge: DependencyEdgeType) => {
    const graphEdge = graphEdgesRef.current.find((item) => item.id === edge.id)
    if (!graphEdge) return
    setSelectedEdge(edge)
    setSelectedNode(null)
    setDepEditorSource(edge.source)
    setDepEditorTarget(edge.target)
    setDepEditorCondition(graphEdge.condition || '')
    setDepEditorSourceKey(graphEdge.source_key || '')
    setDepEditorParams(safeJson(graphEdge.params || {}))
    setDepEditorOpen(true)
  }, [])

  const handleSaveDependency = useCallback(async () => {
    const graphEdge = graphEdgesRef.current.find((item) => item.id === selectedEdge?.id)
    if (!graphEdge) return
    try {
      if (graphEdge.legacy) {
        const targetDef = taskDefsRef.current.find((def) => def.task_id === depEditorTarget)
        if (!targetDef) return
        await updateTaskDefinition(depEditorTarget, {
          ...targetDef,
          watch_condition: depEditorCondition,
        })
      } else {
        const parsedParams = parseDependencyParams(depEditorParams)
        await upsertTaskDependency({
          id: graphEdge.id,
          upstream_task_id: depEditorSource,
          downstream_task_id: depEditorTarget,
          condition: depEditorCondition,
          source_key: depEditorSourceKey.trim(),
          params: parsedParams,
        })
      }
      message.success('依赖已更新')
      loadData()
    } catch (err) {
      message.error(err instanceof Error ? err.message : '更新失败')
    }
    setDepEditorOpen(false)
  }, [selectedEdge?.id, depEditorTarget, depEditorCondition, depEditorSourceKey, depEditorParams, loadData])

  const handleDeleteDependency = useCallback(async () => {
    const graphEdge = graphEdgesRef.current.find((item) => item.id === selectedEdge?.id)
    if (!graphEdge) return
    Modal.confirm({
      title: '删除依赖？',
      content: <p>移除 <strong>{graphEdge.upstream_task_id}</strong> 到 <strong>{graphEdge.downstream_task_id}</strong> 的依赖链接？</p>,
      okText: '删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          if (graphEdge.legacy) {
            const targetDef = taskDefsRef.current.find((def) => def.task_id === depEditorTarget)
            if (!targetDef) return
            await updateTaskDefinition(depEditorTarget, {
              ...targetDef,
              watch_task_id: '',
              watch_condition: '',
              trigger: targetDef.dependencies?.length ? targetDef.trigger : 'scheduled',
            })
            message.success('依赖已移除')
          } else {
            await deleteTaskDependency(graphEdge.id)
            message.success('依赖已移除')
          }
          loadData()
        } catch (err) {
          message.error(err instanceof Error ? err.message : '删除失败')
        }
      },
    })
    setDepEditorOpen(false)
  }, [selectedEdge?.id, loadData])

  const onEdgesDelete = useCallback(async (deletedEdges: Edge[]) => {
    for (const edge of deletedEdges) {
      const graphEdge = graphEdgesRef.current.find((item) => item.id === edge.id)
      if (graphEdge?.legacy) {
        const targetDef = taskDefsRef.current.find((def) => def.task_id === graphEdge.downstream_task_id)
        if (targetDef) {
          await updateTaskDefinition(targetDef.task_id, {
            ...targetDef,
            watch_task_id: '',
            watch_condition: '',
            trigger: targetDef.dependencies?.length ? targetDef.trigger : 'scheduled',
          })
        }
      } else if (graphEdge) {
        await deleteTaskDependency(graphEdge.id)
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

  const selectedUpstreamEdges = selectedNode
    ? graphEdgesRef.current.filter((edge) => edge.downstream_task_id === selectedNode.id)
    : []
  const selectedUpstream = selectedUpstreamEdges
    .map((edge) => taskDefsRef.current.find((def) => def.task_id === edge.upstream_task_id))
    .filter((item): item is TaskDefinition => Boolean(item))
  const selectedDownstream = selectedNode
    ? graphEdgesRef.current
      .filter((edge) => edge.upstream_task_id === selectedNode.id)
      .map((edge) => taskDefsRef.current.find((def) => def.task_id === edge.downstream_task_id))
      .filter((item): item is TaskDefinition => Boolean(item))
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
            const graph = buildGraph({
              nodes: allNodes.map((node) => ({
                task_id: node.data.taskId,
                name: node.data.name,
                kind: node.data.kind,
                enabled: node.data.enabled,
                pipeline_id: node.data.pipelineId,
                labels: {},
                config_status: 'valid',
                runtime: {
                  status: node.data.status || '',
                  last_run_status: node.data.lastRunStatus,
                  consecutive_failures: 0,
                },
              })),
              edges: graphEdgesRef.current,
            })
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
        onEdgeClick={(_event, edge) => {
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
                {selectedUpstream.length > 0
                  ? selectedUpstream.map((item) => <Tag key={item.task_id}>{item.name}</Tag>)
                  : <Text type="secondary">无</Text>}
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
            <Descriptions.Item label="数据源 Key">{selectedEdge.data?.sourceKey || <Text type="secondary">未设置</Text>}</Descriptions.Item>
            <Descriptions.Item label="边参数">
              {selectedEdge.data?.params ? <Text code>{safeJson(selectedEdge.data.params)}</Text> : <Text type="secondary">无</Text>}
            </Descriptions.Item>
            <Descriptions.Item label="来源">{selectedEdge.data?.legacy ? '旧 watch_task_id 字段' : '依赖表'}</Descriptions.Item>
            <Descriptions.Item label="状态">
              {selectedEdge.data?.valid === false
                ? <Tag color="red">{selectedEdge.data.error || '无效'}</Tag>
                : <Tag color="green">有效</Tag>}
            </Descriptions.Item>
            <Descriptions.Item label="操作">
              <Space wrap>
                <Button size="small" onClick={() => openDependencyEditor(selectedEdge)}>
                  编辑依赖
                </Button>
                <Button size="small" danger onClick={handleDeleteDependency}>
                  删除依赖
                </Button>
              </Space>
            </Descriptions.Item>
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
          <p style={{ margin: '12px 0 6px' }}><strong>数据源 Key:</strong></p>
          <Input
            placeholder="例如 upstream_a"
            value={depEditorSourceKey}
            disabled={selectedEdge?.data?.legacy}
            onChange={(event) => setDepEditorSourceKey(event.target.value)}
          />
          <p style={{ margin: '12px 0 6px' }}><strong>边参数 JSON:</strong></p>
          <Input.TextArea
            rows={5}
            value={depEditorParams}
            disabled={selectedEdge?.data?.legacy}
            onChange={(event) => setDepEditorParams(event.target.value)}
          />
        </div>
      </Modal>
    </>
  )
}

function parseDependencyParams(raw: string): Record<string, unknown> {
  const trimmed = raw.trim()
  if (!trimmed) return {}
  const parsed = JSON.parse(trimmed)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('边参数必须是 JSON 对象')
  }
  return parsed as Record<string, unknown>
}
