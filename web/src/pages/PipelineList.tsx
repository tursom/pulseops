import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Button,
  Card,
  Form,
  Input,
  message,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import {
  createPipeline,
  deletePipeline,
  fetchPipelineTasks,
  fetchPipelines,
  fetchTasks,
  updatePipeline,
} from '../api/client'
import type { Pipeline, TaskDefinition, TaskState } from '../api/types'
import { formatTime, runStatusColor, RUN_STATUS_LABELS } from '../utils/pulseops'

const { Title, Text } = Typography

interface PipelineHealth extends Pipeline {
  task_count: number
  abnormal_count: number
  latest_updated_at: string
}

function generatePipelineId(): string {
  return `group-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

function isAbnormal(def: TaskDefinition, state?: TaskState): boolean {
  if (!def.enabled) return false
  if (!state) return true
  return Boolean(
    state.last_reload_error ||
    state.last_run_status === 'failed' ||
    state.last_run_status === 'timeout' ||
    state.last_check_status === 'fail' ||
    state.status === 'unloaded',
  )
}

export default function PipelineList() {
  const navigate = useNavigate()
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [pipelineTasks, setPipelineTasks] = useState<Record<string, TaskDefinition[]>>({})
  const [taskStates, setTaskStates] = useState<TaskState[]>([])
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPipeline, setEditingPipeline] = useState<Pipeline | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm()

  const loadPipelines = useCallback(async () => {
    try {
      const [items, states] = await Promise.all([fetchPipelines(), fetchTasks()])
      setPipelines(items)
      setTaskStates(states)
      const taskMap: Record<string, TaskDefinition[]> = {}
      await Promise.all(items.map(async (pipeline) => {
        try {
          taskMap[pipeline.id] = await fetchPipelineTasks(pipeline.id)
        } catch {
          taskMap[pipeline.id] = []
        }
      }))
      setPipelineTasks(taskMap)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载依赖拓扑失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadPipelines()
  }, [loadPipelines])

  const stateMap = useMemo(() => new Map(taskStates.map((state) => [state.task_id, state])), [taskStates])

  const rows = useMemo<PipelineHealth[]>(() => pipelines.map((pipeline) => {
    const tasks = pipelineTasks[pipeline.id] || []
    const abnormal = tasks.filter((task) => isAbnormal(task, stateMap.get(task.task_id))).length
    const latest = tasks.reduce((acc, task) => {
      const current = new Date(task.updated_at).getTime()
      return current > acc ? current : acc
    }, new Date(pipeline.updated_at).getTime())
    return {
      ...pipeline,
      task_count: tasks.length,
      abnormal_count: abnormal,
      latest_updated_at: latest ? new Date(latest).toISOString() : pipeline.updated_at,
    }
  }), [pipelineTasks, pipelines, stateMap])

  const totals = useMemo(() => ({
    groups: rows.length,
    tasks: rows.reduce((sum, row) => sum + row.task_count, 0),
    abnormal: rows.reduce((sum, row) => sum + row.abnormal_count, 0),
  }), [rows])

  const handleCreate = () => {
    setEditingPipeline(null)
    form.resetFields()
    form.setFieldsValue({ id: generatePipelineId() })
    setModalOpen(true)
  }

  const handleEdit = (pipeline: Pipeline) => {
    setEditingPipeline(pipeline)
    form.setFieldsValue({
      id: pipeline.id,
      name: pipeline.name,
      description: pipeline.description,
    })
    setModalOpen(true)
  }

  const handleDelete = useCallback(async (id: string) => {
    try {
      await deletePipeline(id)
      message.success('任务组已删除')
      await loadPipelines()
    } catch (err) {
      message.error(err instanceof Error ? err.message : '删除失败')
    }
  }, [loadPipelines])

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields()
      setSubmitting(true)
      if (editingPipeline) {
        await updatePipeline(editingPipeline.id, {
          name: values.name,
          description: values.description || '',
        })
        message.success('任务组已更新')
      } else {
        await createPipeline({
          id: values.id,
          name: values.name,
          description: values.description || '',
        })
        message.success('任务组已创建')
      }
      setModalOpen(false)
      form.resetFields()
      await loadPipelines()
    } catch (err) {
      if (err instanceof Error) message.error(err.message)
    } finally {
      setSubmitting(false)
    }
  }

  const columns: ColumnsType<PipelineHealth> = [
    {
      title: '任务组',
      key: 'group',
      render: (_, record) => (
        <Space direction="vertical" size={0}>
          <a onClick={() => navigate(`/pipelines/${record.id}`)}>{record.name}</a>
          <Text type="secondary" style={{ fontSize: 12 }}>{record.id}</Text>
        </Space>
      ),
    },
    {
      title: '描述',
      dataIndex: 'description',
      render: (value: string) => value || <Text type="secondary">—</Text>,
    },
    {
      title: '健康状态',
      key: 'health',
      width: 170,
      render: (_, record) => {
        if (record.task_count === 0) return <Tag>空任务组</Tag>
        if (record.abnormal_count > 0) return <Tag color="red">{record.abnormal_count} 个异常</Tag>
        return <Tag color="green">正常</Tag>
      },
    },
    {
      title: '任务数',
      dataIndex: 'task_count',
      width: 90,
      render: (value: number) => <Tag>{value}</Tag>,
    },
    {
      title: '最近更新时间',
      dataIndex: 'latest_updated_at',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '最近结果',
      key: 'latest',
      width: 120,
      render: (_, record) => {
        const first = (pipelineTasks[record.id] || [])
          .map((task) => stateMap.get(task.task_id))
          .filter(Boolean)
          .sort((a, b) => new Date(b!.last_run_at || '').getTime() - new Date(a!.last_run_at || '').getTime())[0]
        if (!first?.last_run_status) return <Text type="secondary">—</Text>
        return <Tag color={runStatusColor(first.last_run_status)}>{RUN_STATUS_LABELS[first.last_run_status] || first.last_run_status}</Tag>
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 180,
      render: (_, record) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>
            编辑
          </Button>
          <Popconfirm
            title={`确定删除任务组 ${record.name}？已分配任务会变为未分配状态。`}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => handleDelete(record.id)}
          >
            <Button size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div className="page-shell">
      <div className="page-header">
        <div>
          <Title level={2} className="page-title">依赖拓扑</Title>
          <Text className="page-subtitle">任务组健康概览、上游输出、下游触发和异常传播入口。</Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>
          创建任务组
        </Button>
      </div>

      <div className="metric-strip" style={{ marginBottom: 14 }}>
        <div className="metric-tile">
          <div className="metric-label">任务组</div>
          <div className="metric-value">{totals.groups}</div>
        </div>
        <div className="metric-tile">
          <div className="metric-label">组内任务</div>
          <div className="metric-value">{totals.tasks}</div>
        </div>
        <div className="metric-tile">
          <div className="metric-label">异常节点</div>
          <div className="metric-value" style={{ color: totals.abnormal > 0 ? '#cf1322' : '#0f9f7a' }}>{totals.abnormal}</div>
        </div>
      </div>

      <Card className="ops-card">
        <Table<PipelineHealth>
          className="dense-table"
          columns={columns}
          dataSource={rows}
          rowKey="id"
          loading={loading}
          size="small"
          pagination={{
            pageSize: 20,
            showSizeChanger: true,
            showTotal: (total, range) => `${range[0]}-${range[1]} / 共 ${total} 个`,
          }}
          locale={{ emptyText: '暂无任务组，点击上方按钮创建' }}
        />
      </Card>

      <Modal
        title={editingPipeline ? '编辑任务组' : '创建任务组'}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => {
          setModalOpen(false)
          form.resetFields()
        }}
        confirmLoading={submitting}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="id" label="任务组 ID">
            <Input disabled />
          </Form.Item>
          <Form.Item name="name" label="任务组名称" rules={[{ required: true, message: '请输入任务组名称' }]}>
            <Input placeholder="例如：生产环境健康检查" />
          </Form.Item>
          <Form.Item name="description" label="任务组描述">
            <Input.TextArea placeholder="任务组用途说明" rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
