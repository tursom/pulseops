import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import {
  Table,
  Card,
  Row,
  Col,
  Spin,
  Alert,
  Empty,
  Tag,
  Switch,
  Button,
  message,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlayCircleOutlined } from '@ant-design/icons'
import { fetchTasks, triggerTaskRun, enableTask, disableTask } from '../api/client'
import type { TaskState } from '../api/types'
const KIND_COLORS: Record<string, string> = {
  http_check: 'blue',
  tcp_check: 'green',
  script_exec: 'orange',
  scenario_check: 'purple',
  process_check: 'cyan',
  ai_analyze: 'red',
}

const STATUS_COLORS: Record<string, string> = {
  running: 'green',
  loaded: 'blue',
  disabled: 'default',
}

const RUN_STATUS_COLORS: Record<string, string> = {
  success: 'green',
  failed: 'red',
}

export default function Dashboard() {
  const [tasks, setTasks] = useState<TaskState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState<Record<string, boolean>>({})

  const loadTasks = useCallback(async () => {
    try {
      const data = await fetchTasks()
      setTasks(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch tasks')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadTasks()
    const interval = setInterval(loadTasks, 15000)
    return () => clearInterval(interval)
  }, [loadTasks])

  const handleToggleEnabled = useCallback(
    async (taskId: string, enabled: boolean) => {
      setActionLoading((prev) => ({ ...prev, [taskId]: true }))
      try {
        if (enabled) {
          await enableTask(taskId)
          message.success('Task enabled')
        } else {
          await disableTask(taskId)
          message.success('Task disabled')
        }
        await loadTasks()
      } catch (err) {
        message.error(err instanceof Error ? err.message : 'Operation failed')
      } finally {
        setActionLoading((prev) => ({ ...prev, [taskId]: false }))
      }
    },
    [loadTasks],
  )

  const handleRunNow = useCallback(
    async (taskId: string) => {
      setActionLoading((prev) => ({ ...prev, [taskId]: true }))
      try {
        await triggerTaskRun(taskId)
        message.success('Task triggered')
        await loadTasks()
      } catch (err) {
        message.error(err instanceof Error ? err.message : 'Trigger failed')
      } finally {
        setActionLoading((prev) => ({ ...prev, [taskId]: false }))
      }
    },
    [loadTasks],
  )
  const columns: ColumnsType<TaskState> = [
    {
      title: 'Task Name',
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: TaskState) => (
        <Link to={`/tasks/${record.task_id}`}>{name}</Link>
      ),
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: 'Kind',
      dataIndex: 'kind',
      key: 'kind',
      render: (kind: string) => (
        <Tag color={KIND_COLORS[kind] || 'default'}>{kind}</Tag>
      ),
      filters: Object.keys(KIND_COLORS).map((k) => ({ text: k, value: k })),
      onFilter: (value, record) => record.kind === value,
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => (
        <Tag color={STATUS_COLORS[status] || 'default'}>{status}</Tag>
      ),
      filters: [
        { text: 'Running', value: 'running' },
        { text: 'Loaded', value: 'loaded' },
        { text: 'Disabled', value: 'disabled' },
      ],
      onFilter: (value, record) => record.status === value,
    },
    {
      title: 'Last Run',
      dataIndex: 'last_run_at',
      key: 'last_run_at',
      render: (val: string | null) =>
        val ? new Date(val).toLocaleString() : '—',
      sorter: (a, b) => {
        const da = a.last_run_at ? new Date(a.last_run_at).getTime() : 0
        const db = b.last_run_at ? new Date(b.last_run_at).getTime() : 0
        return da - db
      },
    },
    {
      title: 'Last Status',
      dataIndex: 'last_run_status',
      key: 'last_run_status',
      render: (status: string) => {
        if (!status) return <Tag>—</Tag>
        return (
          <Tag color={RUN_STATUS_COLORS[status] || 'default'}>{status}</Tag>
        )
      },
      filters: [
        { text: 'Success', value: 'success' },
        { text: 'Failed', value: 'failed' },
      ],
      onFilter: (value, record) => record.last_run_status === value,
    },
    {
      title: 'Labels',
      dataIndex: 'labels',
      key: 'labels',
      render: (labels: Record<string, string>) => {
        const entries = Object.entries(labels ?? {})
        if (entries.length === 0) return '—'
        return (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {entries.map(([k, v]) => (
              <Tag key={k}>{`${k}: ${v}`}</Tag>
            ))}
          </div>
        )
      },
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_val, record: TaskState) => (
        <div
          style={{ display: 'flex', gap: 8, alignItems: 'center' }}
          onClick={(e) => e.stopPropagation()}
        >
          <Switch
            checked={record.enabled}
            loading={actionLoading[record.task_id]}
            onChange={(checked) => handleToggleEnabled(record.task_id, checked)}
          />
          <Button
            type="primary"
            size="small"
            icon={<PlayCircleOutlined />}
            loading={actionLoading[record.task_id]}
            onClick={() => handleRunNow(record.task_id)}
          >
            Run Now
          </Button>
        </div>
      ),
    },
  ]
  const totalTasks = tasks.length
  const enabledTasks = tasks.filter((t) => t.enabled).length
  const failedTasks = tasks.filter((t) => t.last_run_status === 'failed').length
  const errorTasks = tasks.filter((t) => t.last_error !== '').length
  if (error) {
    return (
      <div style={{ padding: 24 }}>
        <Alert
          type="error"
          message="Failed to load tasks"
          description={error}
          showIcon
        />
      </div>
    )
  }

  return (
    <div style={{ padding: 24 }}>
      <Typography.Title level={3} style={{ marginBottom: 24 }}>
        Dashboard
      </Typography.Title>

      {loading ? (
        <div style={{ textAlign: 'center', padding: 80 }}>
          <Spin size="large" />
        </div>
      ) : (
        <>
          <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Typography.Text type="secondary">Total Tasks</Typography.Text>
                <Typography.Title level={2} style={{ margin: '8px 0 0' }}>
                  {totalTasks}
                </Typography.Title>
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Typography.Text type="secondary">Enabled</Typography.Text>
                <Typography.Title
                  level={2}
                  style={{ margin: '8px 0 0', color: '#52c41a' }}
                >
                  {enabledTasks}
                </Typography.Title>
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Typography.Text type="secondary">Failed</Typography.Text>
                <Typography.Title
                  level={2}
                  style={{ margin: '8px 0 0', color: '#ff4d4f' }}
                >
                  {failedTasks}
                </Typography.Title>
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Typography.Text type="secondary">With Errors</Typography.Text>
                <Typography.Title
                  level={2}
                  style={{ margin: '8px 0 0', color: '#faad14' }}
                >
                  {errorTasks}
                </Typography.Title>
              </Card>
            </Col>
          </Row>

          {tasks.length === 0 ? (
            <Empty description="No tasks configured" />
          ) : (
            <Table<TaskState>
              columns={columns}
              dataSource={tasks}
              rowKey="task_id"
              pagination={{
                pageSize: 20,
                showSizeChanger: true,
                showTotal: (total, range) =>
                  `${range[0]}-${range[1]} of ${total} tasks`,
              }}
            />
          )}
        </>
      )}
    </div>
  )
}
