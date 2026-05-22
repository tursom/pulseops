import { useState, useEffect, useCallback, useMemo } from 'react'
import { Typography, Button, Space, Input, Select, Table, Tag, Switch, Spin, Alert, Empty, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined, PlayCircleOutlined, PauseCircleOutlined, ThunderboltOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { Link, useNavigate } from 'react-router-dom'
import { fetchTasks, fetchTaskDefinitions, triggerTaskRun, enableTask, disableTask } from '../api/client'
import type { TaskState } from '../api/types'

const KIND_COLORS: Record<string, string> = {
  http_check: 'blue',
  script_exec: 'green',
  data_compare: 'orange',
  schema_check: 'purple',
  custom: 'cyan',
  ai_analyze: 'red',
}

const STATUS_COLORS: Record<string, string> = {
  running: 'green',
  loaded: 'blue',
  disabled: 'default',
  unloaded: 'orange',
}

const RUN_STATUS_COLORS: Record<string, string> = {
  success: 'green',
  failed: 'red',
}

export default function TaskList() {
  const navigate = useNavigate()
  const [tasks, setTasks] = useState<TaskState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState<Record<string, boolean>>({})
  const [searchText, setSearchText] = useState('')
  const [kindFilter, setKindFilter] = useState<string | undefined>(undefined)
  const [statusFilter, setStatusFilter] = useState<string | undefined>(undefined)
  const [runStatusFilter, setRunStatusFilter] = useState<string | undefined>(undefined)
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([])
  const [batchLoading, setBatchLoading] = useState(false)

  const loadTasks = useCallback(async () => {
    try {
      const [defs, states] = await Promise.all([fetchTaskDefinitions(), fetchTasks()])
      const stateMap = new Map(states.map((s) => [s.task_id, s]))
      const merged: TaskState[] = defs.map((def) => {
        const state = stateMap.get(def.task_id)
        if (state) return state
        return {
          task_id: def.task_id,
          name: def.name || def.task_id,
          kind: def.kind,
          enabled: def.enabled,
          status: 'unloaded',
          labels: def.labels || {},
          last_run_at: null,
          next_run_at: null,
          last_run_status: '',
          last_check_status: '',
          last_error: '',
          last_duration_ms: 0,
          last_reload_error: '',
          last_sample_seed: 0,
          last_sample_count: 0,
          last_mismatch_count: 0,
          source_path: '',
          updated_at: '',
        } as TaskState
      })
      setTasks(merged)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败')
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
          message.success('任务已启用')
        } else {
          await disableTask(taskId)
          message.success('任务已禁用')
        }
        await loadTasks()
      } catch (err) {
        message.error(err instanceof Error ? err.message : '操作失败')
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
        message.success('任务已触发')
        await loadTasks()
      } catch (err) {
        message.error(err instanceof Error ? err.message : '触发失败')
      } finally {
        setActionLoading((prev) => ({ ...prev, [taskId]: false }))
      }
    },
    [loadTasks],
  )

  const handleBatchAction = useCallback(
    async (action: 'enable' | 'disable' | 'run') => {
      setBatchLoading(true)
      let successCount = 0
      let failCount = 0
      for (const taskId of selectedRowKeys) {
        try {
          if (action === 'enable') {
            await enableTask(taskId)
          } else if (action === 'disable') {
            await disableTask(taskId)
          } else {
            await triggerTaskRun(taskId)
          }
          successCount++
        } catch {
          failCount++
        }
      }
      setBatchLoading(false)
      if (failCount === 0) {
        const label = action === 'enable' ? '启用' : action === 'disable' ? '禁用' : '执行'
        message.success(`已成功${label} ${successCount} 个任务`)
      } else {
        message.warning(`成功 ${successCount} 个，失败 ${failCount} 个`)
      }
      setSelectedRowKeys([])
      await loadTasks()
    },
    [selectedRowKeys, loadTasks],
  )

  const handleResetFilters = useCallback(() => {
    setSearchText('')
    setKindFilter(undefined)
    setStatusFilter(undefined)
    setRunStatusFilter(undefined)
  }, [])

  const filteredTasks = useMemo(() => {
    return tasks.filter((t) => {
      if (searchText && !t.name.toLowerCase().includes(searchText.toLowerCase())) {
        return false
      }
      if (kindFilter && t.kind !== kindFilter) {
        return false
      }
      if (statusFilter && t.status !== statusFilter) {
        return false
      }
      if (runStatusFilter && t.last_run_status !== runStatusFilter) {
        return false
      }
      return true
    })
  }, [tasks, searchText, kindFilter, statusFilter, runStatusFilter])

  const columns: ColumnsType<TaskState> = [
    {
      title: '任务名称',
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: TaskState) => (
        <Link to={`/tasks/${record.task_id}`}>{name}</Link>
      ),
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      render: (kind: string) => (
        <Tag color={KIND_COLORS[kind] || 'default'}>{kind}</Tag>
      ),
      filters: Object.keys(KIND_COLORS).map((k) => ({ text: k, value: k })),
      onFilter: (value, record) => record.kind === value,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => (
        <Tag color={STATUS_COLORS[status] || 'default'}>{status}</Tag>
      ),
      filters: [
        { text: '运行中', value: 'running' },
        { text: '已加载', value: 'loaded' },
        { text: '已禁用', value: 'disabled' },
        { text: '未加载', value: 'unloaded' },
      ],
      onFilter: (value, record) => record.status === value,
    },
    {
      title: '上次运行',
      dataIndex: 'last_run_at',
      key: 'last_run_at',
      render: (val: string | null) =>
        val ? new Date(val).toLocaleString() : '-',
      sorter: (a, b) => {
        const da = a.last_run_at ? new Date(a.last_run_at).getTime() : 0
        const db = b.last_run_at ? new Date(b.last_run_at).getTime() : 0
        return da - db
      },
    },
    {
      title: '运行结果',
      dataIndex: 'last_run_status',
      key: 'last_run_status',
      render: (status: string) => {
        if (!status) return <Tag>-</Tag>
        return (
          <Tag color={RUN_STATUS_COLORS[status] || 'default'}>{status}</Tag>
        )
      },
      filters: [
        { text: '成功', value: 'success' },
        { text: '失败', value: 'failed' },
      ],
      onFilter: (value, record) => record.last_run_status === value,
    },
    {
      title: '标签',
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
      title: '操作',
      key: 'actions',
      render: (_val, record: TaskState) => (
        <div
          style={{ display: 'flex', gap: 8, alignItems: 'center' }}
          onClick={(e) => e.stopPropagation()}
        >
          <Switch
            checked={record.enabled}
            disabled={record.status === 'unloaded'}
            loading={actionLoading[record.task_id]}
            onChange={(checked) => handleToggleEnabled(record.task_id, checked)}
          />
          <Button
            type="primary"
            size="small"
            icon={<PlayCircleOutlined />}
            disabled={record.status === 'unloaded'}
            loading={actionLoading[record.task_id]}
            onClick={() => handleRunNow(record.task_id)}
          >
            立即执行
          </Button>
          <Button
            size="small"
            icon={<EditOutlined />}
            onClick={() => navigate(`/task-defs/${encodeURIComponent(record.task_id)}/edit?from=/tasks`)}
          >
            编辑
          </Button>
        </div>
      ),
    },
  ]

  if (error) {
    return (
      <div style={{ padding: 24 }}>
        <Alert type="error" message="加载失败" description={error} style={{ margin: 24 }} />
      </div>
    )
  }

  return (
    <div style={{ padding: 24 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Typography.Title level={3} style={{ margin: 0 }}>任务列表</Typography.Title>
        <Space>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/task-defs/new?from=/tasks')}>创建任务</Button>
          <Button icon={<ReloadOutlined />} onClick={loadTasks}>刷新</Button>
        </Space>
      </div>

      <div style={{ marginBottom: 16 }}>
        <Space wrap>
          <Input
            placeholder="搜索任务名称..."
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            style={{ width: 200 }}
            allowClear
          />
          <Select
            placeholder="类型"
            value={kindFilter}
            onChange={(v) => setKindFilter(v as string | undefined)}
            allowClear
            style={{ width: 140 }}
            options={Object.keys(KIND_COLORS).map((k) => ({ label: k, value: k }))}
          />
          <Select
            placeholder="状态"
            value={statusFilter}
            onChange={(v) => setStatusFilter(v as string | undefined)}
            allowClear
            style={{ width: 120 }}
            options={[
            { label: '运行中', value: 'running' },
            { label: '已加载', value: 'loaded' },
            { label: '已禁用', value: 'disabled' },
            { label: '未加载', value: 'unloaded' },
            ]}
          />
          <Select
            placeholder="运行结果"
            value={runStatusFilter}
            onChange={(v) => setRunStatusFilter(v as string | undefined)}
            allowClear
            style={{ width: 120 }}
            options={[
              { label: '成功', value: 'success' },
              { label: '失败', value: 'failed' },
            ]}
          />
          <Button onClick={handleResetFilters}>重置</Button>
          <Typography.Text type="secondary">
            共 {filteredTasks.length} 个任务
          </Typography.Text>
        </Space>
      </div>

      {selectedRowKeys.length > 0 && (
        <div style={{ marginBottom: 16, padding: '8px 16px', background: '#e6f7ff', borderRadius: 6 }}>
          <Space>
            <span>已选 {selectedRowKeys.length} 项</span>
            <Button
              icon={<PlayCircleOutlined />}
              loading={batchLoading}
              onClick={() => handleBatchAction('enable')}
            >
              批量启用
            </Button>
            <Button
              icon={<PauseCircleOutlined />}
              loading={batchLoading}
              onClick={() => handleBatchAction('disable')}
            >
              批量禁用
            </Button>
            <Button
              icon={<ThunderboltOutlined />}
              loading={batchLoading}
              onClick={() => handleBatchAction('run')}
            >
              批量执行
            </Button>
          </Space>
        </div>
      )}

      {loading ? (
        <Spin size="large" style={{ display: 'block', margin: '48px auto' }} />
      ) : filteredTasks.length === 0 ? (
        <Empty description="暂无任务" style={{ marginTop: 48 }} />
      ) : (
        <Table<TaskState>
          rowSelection={{
            type: 'checkbox',
            selectedRowKeys,
            onChange: (keys) => setSelectedRowKeys(keys as string[]),
          }}
          columns={columns}
          dataSource={filteredTasks}
          rowKey="task_id"
          pagination={{
            pageSize: 20,
            showSizeChanger: true,
            showTotal: (total, range) => `${range[0]}-${range[1]} / 共 ${total} 个`,
          }}
        />
      )}
    </div>
  )
}
