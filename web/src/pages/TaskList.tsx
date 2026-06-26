import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import {
  Alert,
  Button,
  Empty,
  Input,
  message,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  EditOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import {
  disableTask,
  enableTask,
  fetchTaskDefinitions,
  fetchTasks,
  triggerTaskRun,
} from '../api/client'
import {
  AUTO_REFRESH_MS,
  checkStatusColor,
  CHECK_STATUS_LABELS,
  formatDuration,
  formatRelativeTime,
  formatTime,
  KIND_COLORS,
  KIND_LABELS,
  mergeTaskViews,
  runStatusColor,
  RUN_STATUS_LABELS,
  statusColorForTask,
  summarizeTasks,
  TASK_STATUS_LABELS,
  type TaskView,
} from '../utils/pulseops'

const { Title, Text } = Typography

type BatchAction = 'enable' | 'disable' | 'run'
type SortKey = 'abnormal' | 'lastRun' | 'nextRun' | 'duration' | 'updatedAt'

function getParam(params: URLSearchParams, key: string): string | undefined {
  const value = params.get(key)
  return value || undefined
}

function setParam(params: URLSearchParams, key: string, value: string | undefined): URLSearchParams {
  const next = new URLSearchParams(params)
  if (value) next.set(key, value)
  else next.delete(key)
  next.delete('page')
  return next
}

function matchesLabel(task: TaskView, key: string, value: string | undefined): boolean {
  if (!value) return true
  return task.labels?.[key] === value
}

function timeValue(value: string | null | undefined): number {
  if (!value) return 0
  const time = new Date(value).getTime()
  return Number.isFinite(time) ? time : 0
}

export default function TaskList() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [tasks, setTasks] = useState<TaskView[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState<Record<string, boolean>>({})
  const [selectedRowKeys, setSelectedRowKeys] = useState<string[]>([])
  const [batchLoading, setBatchLoading] = useState(false)
  const [lastRefreshAt, setLastRefreshAt] = useState<Date | null>(null)

  const filters = {
    search: getParam(searchParams, 'search') || '',
    kind: getParam(searchParams, 'kind'),
    enabled: getParam(searchParams, 'enabled'),
    status: getParam(searchParams, 'status'),
    runStatus: getParam(searchParams, 'runStatus'),
    checkStatus: getParam(searchParams, 'checkStatus'),
    loadError: getParam(searchParams, 'loadError'),
    env: getParam(searchParams, 'env'),
    service: getParam(searchParams, 'service'),
    domain: getParam(searchParams, 'domain'),
    sort: (getParam(searchParams, 'sort') || 'abnormal') as SortKey,
  }

  const loadTasks = useCallback(async () => {
    try {
      const [defs, states] = await Promise.all([fetchTaskDefinitions(), fetchTasks()])
      setTasks(mergeTaskViews(defs, states))
      setError(null)
      setLastRefreshAt(new Date())
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadTasks()
    const interval = setInterval(loadTasks, AUTO_REFRESH_MS)
    return () => clearInterval(interval)
  }, [loadTasks])

  const optionSets = useMemo(() => {
    const kinds = new Set<string>()
    const envs = new Set<string>()
    const services = new Set<string>()
    const domains = new Set<string>()
    for (const task of tasks) {
      if (task.kind) kinds.add(task.kind)
      if (task.labels?.env) envs.add(task.labels.env)
      if (task.labels?.service) services.add(task.labels.service)
      if (task.labels?.domain) domains.add(task.labels.domain)
    }
    return {
      kinds: Array.from(kinds).sort(),
      envs: Array.from(envs).sort(),
      services: Array.from(services).sort(),
      domains: Array.from(domains).sort(),
    }
  }, [tasks])

  const filteredTasks = useMemo(() => {
    const search = filters.search.trim().toLowerCase()
    const severityOrder: Record<TaskView['severity'], number> = { critical: 0, warning: 1, normal: 2, disabled: 3 }

    return tasks
      .filter((task) => {
        if (search) {
          const haystack = [
            task.name,
            task.task_id,
            task.kind,
            task.last_error,
            task.last_reload_error,
            task.anomaly_reason,
          ].join('\n').toLowerCase()
          if (!haystack.includes(search)) return false
        }
        if (filters.kind && task.kind !== filters.kind) return false
        if (filters.enabled === 'true' && !task.enabled) return false
        if (filters.enabled === 'false' && task.enabled) return false
        if (filters.status && task.status !== filters.status) return false
        if (filters.runStatus && task.last_run_status !== filters.runStatus) return false
        if (filters.checkStatus && task.last_check_status !== filters.checkStatus) return false
        if (filters.loadError === '1' && !task.last_reload_error) return false
        if (!matchesLabel(task, 'env', filters.env)) return false
        if (!matchesLabel(task, 'service', filters.service)) return false
        if (!matchesLabel(task, 'domain', filters.domain)) return false
        return true
      })
      .sort((a, b) => {
        if (filters.sort === 'lastRun') return timeValue(b.last_run_at) - timeValue(a.last_run_at)
        if (filters.sort === 'nextRun') return timeValue(a.next_run_at) - timeValue(b.next_run_at)
        if (filters.sort === 'duration') return (b.last_duration_ms || 0) - (a.last_duration_ms || 0)
        if (filters.sort === 'updatedAt') return timeValue(b.updated_at) - timeValue(a.updated_at)
        return severityOrder[a.severity] - severityOrder[b.severity] || timeValue(b.last_run_at) - timeValue(a.last_run_at)
      })
  }, [tasks, filters])

  const summary = useMemo(() => summarizeTasks(tasks), [tasks])

  const updateFilter = (key: string, value: string | undefined) => {
    setSearchParams(setParam(searchParams, key, value), { replace: true })
  }

  const resetFilters = () => {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

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
        const run = await triggerTaskRun(taskId)
        message.success('任务已触发')
        navigate(`/tasks/${encodeURIComponent(taskId)}/runs/${encodeURIComponent(run.run_id)}`)
      } catch (err) {
        message.error(err instanceof Error ? err.message : '触发失败')
      } finally {
        setActionLoading((prev) => ({ ...prev, [taskId]: false }))
        loadTasks()
      }
    },
    [loadTasks, navigate],
  )

  const handleBatchAction = useCallback(
    async (action: BatchAction) => {
      setBatchLoading(true)
      const details: string[] = []
      let successCount = 0
      let failCount = 0

      for (const taskId of selectedRowKeys) {
        try {
          if (action === 'enable') await enableTask(taskId)
          if (action === 'disable') await disableTask(taskId)
          if (action === 'run') await triggerTaskRun(taskId)
          successCount += 1
        } catch (err) {
          failCount += 1
          details.push(`${taskId}: ${err instanceof Error ? err.message : '失败'}`)
        }
      }

      setBatchLoading(false)
      setSelectedRowKeys([])
      await loadTasks()

      const label = action === 'enable' ? '启用' : action === 'disable' ? '禁用' : '执行'
      if (failCount === 0) {
        message.success(`已${label} ${successCount} 个任务`)
      } else {
        message.warning({
          content: `已${label} ${successCount} 个，失败 ${failCount} 个。${details.slice(0, 2).join('；')}`,
          duration: 6,
        })
      }
    },
    [selectedRowKeys, loadTasks],
  )

  const columns: ColumnsType<TaskView> = [
    {
      title: '任务名 / ID',
      key: 'name',
      fixed: 'left',
      width: 250,
      render: (_, task) => (
        <Space direction="vertical" size={0}>
          <Link to={`/tasks/${encodeURIComponent(task.task_id)}`}>{task.name}</Link>
          <Text type="secondary" style={{ fontSize: 12 }}>{task.task_id}</Text>
        </Space>
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      width: 120,
      render: (kind: string) => <Tag color={KIND_COLORS[kind] || 'default'}>{KIND_LABELS[kind] || kind}</Tag>,
    },
    {
      title: '标签',
      dataIndex: 'labels',
      width: 220,
      render: (labels: Record<string, string>) => {
        const entries = Object.entries(labels || {})
        if (entries.length === 0) return <Text type="secondary">—</Text>
        return (
          <Space wrap size={[4, 4]}>
            {entries.slice(0, 4).map(([key, value]) => <Tag key={key}>{key}: {value}</Tag>)}
            {entries.length > 4 && <Tag>+{entries.length - 4}</Tag>}
          </Space>
        )
      },
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 86,
      render: (_: boolean, task) => (
        <Switch
          checked={task.enabled}
          disabled={task.status === 'unloaded' && !task.definition}
          loading={actionLoading[task.task_id]}
          size="small"
          onChange={(checked) => handleToggleEnabled(task.task_id, checked)}
        />
      ),
    },
    {
      title: '运行态',
      dataIndex: 'status',
      width: 110,
      render: (_: string, task) => <Tag color={statusColorForTask(task)}>{TASK_STATUS_LABELS[task.status] || task.status || '—'}</Tag>,
    },
    {
      title: '上次运行',
      dataIndex: 'last_run_at',
      width: 136,
      render: (value: string | null) => <Tooltip title={formatTime(value)}>{formatRelativeTime(value)}</Tooltip>,
    },
    {
      title: '上次结果',
      dataIndex: 'last_run_status',
      width: 104,
      render: (status: string) => status
        ? <Tag color={runStatusColor(status)}>{RUN_STATUS_LABELS[status] || status}</Tag>
        : <Text type="secondary">—</Text>,
    },
    {
      title: '检查',
      dataIndex: 'last_check_status',
      width: 96,
      render: (status: string) => status
        ? <Tag color={checkStatusColor(status)}>{CHECK_STATUS_LABELS[status] || status}</Tag>
        : <Text type="secondary">—</Text>,
    },
    {
      title: '耗时',
      dataIndex: 'last_duration_ms',
      width: 86,
      render: (value: number) => formatDuration(value),
    },
    {
      title: '下次运行',
      dataIndex: 'next_run_at',
      width: 136,
      render: (value: string | null) => <Tooltip title={formatTime(value)}>{formatRelativeTime(value)}</Tooltip>,
    },
    {
      title: '错误摘要',
      key: 'error',
      width: 260,
      render: (_, task) => {
        const text = task.last_reload_error || task.last_error || task.anomaly_reason
        if (!text) return <Text type="secondary">—</Text>
        return (
          <Tooltip title={text}>
            <Text type="danger" ellipsis style={{ maxWidth: 240 }}>{text}</Text>
          </Tooltip>
        )
      },
    },
    {
      title: '操作',
      key: 'actions',
      fixed: 'right',
      width: 238,
      render: (_, task) => (
        <Space onClick={(event) => event.stopPropagation()}>
          <Button
            type="primary"
            size="small"
            icon={<PlayCircleOutlined />}
            disabled={task.status === 'unloaded'}
            loading={actionLoading[task.task_id]}
            onClick={() => handleRunNow(task.task_id)}
          >
            执行
          </Button>
          <Button
            size="small"
            icon={<EditOutlined />}
            onClick={() => navigate(`/task-defs/${encodeURIComponent(task.task_id)}/edit?from=/tasks`)}
          >
            编辑
          </Button>
          <Button size="small" onClick={() => navigate(`/tasks/${encodeURIComponent(task.task_id)}`)}>
            详情
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="page-shell">
      <div className="page-header">
        <div>
          <Title level={2} className="page-title">任务监控</Title>
          <Text className="page-subtitle">
            URL 同步筛选，异常优先排序；最近刷新：{lastRefreshAt ? lastRefreshAt.toLocaleTimeString() : '—'}
          </Text>
        </div>
        <Space wrap>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/task-defs/new?from=/tasks')}>
            创建任务
          </Button>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => { setLoading(true); loadTasks() }}>
            刷新
          </Button>
        </Space>
      </div>

      {error && (
        <Alert
          type="error"
          message="任务列表加载失败"
          description={error}
          action={<Button size="small" onClick={() => { setLoading(true); loadTasks() }}>重试</Button>}
          showIcon
          style={{ marginBottom: 14 }}
        />
      )}

      <div className="metric-strip" style={{ marginBottom: 14 }}>
        <div className="metric-tile" onClick={() => resetFilters()}>
          <div className="metric-label">任务总数</div>
          <div className="metric-value">{summary.total}</div>
        </div>
        <div className="metric-tile" onClick={() => updateFilter('enabled', 'true')}>
          <div className="metric-label">启用</div>
          <div className="metric-value" style={{ color: '#0f9f7a' }}>{summary.enabled}</div>
        </div>
        <div className="metric-tile" onClick={() => updateFilter('runStatus', 'failed')}>
          <div className="metric-label">失败</div>
          <div className="metric-value" style={{ color: '#cf1322' }}>{summary.failed}</div>
        </div>
        <div className="metric-tile" onClick={() => updateFilter('checkStatus', 'fail')}>
          <div className="metric-label">检查未通过</div>
          <div className="metric-value" style={{ color: '#cf1322' }}>{summary.checkFailed}</div>
        </div>
        <div className="metric-tile" onClick={() => updateFilter('loadError', '1')}>
          <div className="metric-label">加载失败</div>
          <div className="metric-value" style={{ color: '#fa8c16' }}>{summary.loadFailed}</div>
        </div>
      </div>

      <div className="toolbar-band">
        <Space wrap>
          <Input
            allowClear
            placeholder="搜索任务名称、ID、错误信息"
            value={filters.search}
            onChange={(event) => updateFilter('search', event.target.value || undefined)}
            style={{ width: 260 }}
          />
          <Select
            allowClear
            placeholder="任务类型"
            value={filters.kind}
            onChange={(value) => updateFilter('kind', value)}
            style={{ width: 150 }}
            options={optionSets.kinds.map((kind) => ({ value: kind, label: KIND_LABELS[kind] || kind }))}
          />
          <Select
            allowClear
            placeholder="启用状态"
            value={filters.enabled}
            onChange={(value) => updateFilter('enabled', value)}
            style={{ width: 130 }}
            options={[
              { value: 'true', label: '已启用' },
              { value: 'false', label: '已禁用' },
            ]}
          />
          <Select
            allowClear
            placeholder="运行态"
            value={filters.status}
            onChange={(value) => updateFilter('status', value)}
            style={{ width: 130 }}
            options={Object.entries(TASK_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          />
          <Select
            allowClear
            placeholder="运行结果"
            value={filters.runStatus}
            onChange={(value) => updateFilter('runStatus', value)}
            style={{ width: 130 }}
            options={Object.entries(RUN_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          />
          <Select
            allowClear
            placeholder="检查状态"
            value={filters.checkStatus}
            onChange={(value) => updateFilter('checkStatus', value)}
            style={{ width: 130 }}
            options={Object.entries(CHECK_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          />
          <Select
            allowClear
            placeholder="env"
            value={filters.env}
            onChange={(value) => updateFilter('env', value)}
            style={{ width: 120 }}
            options={optionSets.envs.map((value) => ({ value, label: value }))}
          />
          <Select
            allowClear
            placeholder="service"
            value={filters.service}
            onChange={(value) => updateFilter('service', value)}
            style={{ width: 140 }}
            options={optionSets.services.map((value) => ({ value, label: value }))}
          />
          <Select
            allowClear
            placeholder="domain"
            value={filters.domain}
            onChange={(value) => updateFilter('domain', value)}
            style={{ width: 140 }}
            options={optionSets.domains.map((value) => ({ value, label: value }))}
          />
          <Select
            value={filters.sort}
            onChange={(value) => updateFilter('sort', value)}
            style={{ width: 150 }}
            options={[
              { value: 'abnormal', label: '异常优先' },
              { value: 'lastRun', label: '上次运行时间' },
              { value: 'nextRun', label: '下次运行时间' },
              { value: 'duration', label: '耗时' },
              { value: 'updatedAt', label: '更新时间' },
            ]}
          />
          <Button onClick={resetFilters}>重置</Button>
          <Text type="secondary">当前 {filteredTasks.length} / {tasks.length}</Text>
        </Space>
      </div>

      {selectedRowKeys.length > 0 && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 14 }}
          message={
            <Space wrap>
              <Text>已选 {selectedRowKeys.length} 个任务</Text>
              <Button size="small" icon={<ThunderboltOutlined />} loading={batchLoading} onClick={() => handleBatchAction('run')}>
                批量执行
              </Button>
              <Button size="small" icon={<PlayCircleOutlined />} loading={batchLoading} onClick={() => handleBatchAction('enable')}>
                批量启用
              </Button>
              <Popconfirm
                title="确定批量禁用所选任务？"
                okText="禁用"
                cancelText="取消"
                okButtonProps={{ danger: true }}
                onConfirm={() => handleBatchAction('disable')}
              >
                <Button size="small" danger icon={<PauseCircleOutlined />} loading={batchLoading}>
                  批量禁用
                </Button>
              </Popconfirm>
            </Space>
          }
        />
      )}

      <Table<TaskView>
        className="dense-table"
        rowSelection={{
          type: 'checkbox',
          selectedRowKeys,
          onChange: (keys) => setSelectedRowKeys(keys as string[]),
        }}
        columns={columns}
        dataSource={filteredTasks}
        rowKey="task_id"
        loading={loading}
        scroll={{ x: 1580 }}
        size="small"
        pagination={{
          pageSize: 20,
          showSizeChanger: true,
          showTotal: (total, range) => `${range[0]}-${range[1]} / 共 ${total} 个`,
        }}
        locale={{
          emptyText: (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="没有匹配任务"
            >
              <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/task-defs/new?from=/tasks')}>
                创建任务
              </Button>
            </Empty>
          ),
        }}
      />
    </div>
  )
}
