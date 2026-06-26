import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Empty,
  Progress,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  ApartmentOutlined,
  ClockCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'
import {
  fetchTaskDefinitions,
  fetchTaskRuns,
  fetchTasks,
} from '../api/client'
import type { RunRecord } from '../api/types'
import {
  aggregateLabels,
  AUTO_REFRESH_MS,
  checkStatusColor,
  CHECK_STATUS_LABELS,
  formatDuration,
  formatRelativeTime,
  formatTime,
  KIND_COLORS,
  KIND_LABELS,
  mergeTaskViews,
  runIsAbnormal,
  runStatusColor,
  RUN_STATUS_LABELS,
  shortID,
  summarizeTasks,
  type LabelAggregate,
  type TaskView,
} from '../utils/pulseops'

const { Title, Text } = Typography

function healthState(tasks: TaskView[], loadError: string | null): { color: string; label: string; detail: string } {
  if (loadError) {
    return { color: '#cf1322', label: '数据加载失败', detail: loadError }
  }
  if (tasks.some((task) => task.last_reload_error)) {
    return { color: '#fa8c16', label: '存在配置加载错误', detail: '至少一个任务定义无法加载到运行态' }
  }
  if (tasks.some((task) => task.severity === 'critical')) {
    return { color: '#cf1322', label: '存在失败任务', detail: '需要优先处理失败、超时或检查未通过任务' }
  }
  if (tasks.some((task) => task.severity === 'warning')) {
    return { color: '#fa8c16', label: '存在待确认任务', detail: '存在未加载或长时间未运行任务' }
  }
  return { color: '#0f9f7a', label: '正常', detail: '当前未发现需要立即处理的任务' }
}

function priority(tasks: TaskView[]): TaskView[] {
  return tasks
    .filter((task) => task.severity === 'critical' || task.severity === 'warning')
    .slice(0, 12)
}

export default function Dashboard() {
  const navigate = useNavigate()
  const [tasks, setTasks] = useState<TaskView[]>([])
  const [recentRuns, setRecentRuns] = useState<RunRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [lastRefreshAt, setLastRefreshAt] = useState<Date | null>(null)

  const loadData = useCallback(async () => {
    try {
      const [defs, states] = await Promise.all([fetchTaskDefinitions(), fetchTasks()])
      const merged = mergeTaskViews(defs, states)
      setTasks(merged)

      const active = merged
        .filter((task) => task.status !== 'unloaded')
        .sort((a, b) => {
          const aw = a.severity === 'critical' ? 0 : 1
          const bw = b.severity === 'critical' ? 0 : 1
          return aw - bw
        })
        .slice(0, 10)

      const runResults = await Promise.allSettled(active.map((task) => fetchTaskRuns(task.task_id, 4)))
      const runs = runResults
        .flatMap((result) => (result.status === 'fulfilled' ? result.value : []))
        .sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime())
        .slice(0, 12)

      setRecentRuns(runs)
      setError(null)
      setLastRefreshAt(new Date())
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载工作台失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData()
    const interval = setInterval(loadData, AUTO_REFRESH_MS)
    return () => clearInterval(interval)
  }, [loadData])

  const summary = useMemo(() => summarizeTasks(tasks), [tasks])
  const health = useMemo(() => healthState(tasks, error), [tasks, error])
  const anomalies = useMemo(() => priority(tasks), [tasks])
  const labelAgg = useMemo(() => aggregateLabels(tasks), [tasks])

  const abnormalRuns = recentRuns.filter(runIsAbnormal).length

  const anomalyColumns: ColumnsType<TaskView> = [
    {
      title: '优先级',
      key: 'severity',
      width: 90,
      render: (_, task) => (
        <Tag color={task.severity === 'critical' ? 'red' : 'orange'}>
          {task.severity === 'critical' ? '立即处理' : '待确认'}
        </Tag>
      ),
    },
    {
      title: '任务',
      key: 'task',
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
      title: '异常',
      key: 'reason',
      render: (_, task) => (
        <Space direction="vertical" size={0}>
          <Text strong>{task.anomaly_type || '待确认'}</Text>
          <Tooltip title={task.anomaly_reason}>
            <Text type="secondary" ellipsis style={{ maxWidth: 320 }}>
              {task.anomaly_reason || '暂无详情'}
            </Text>
          </Tooltip>
        </Space>
      ),
    },
    {
      title: '最近运行',
      dataIndex: 'last_run_at',
      width: 150,
      render: (value: string | null) => (
        <Tooltip title={formatTime(value)}>{formatRelativeTime(value)}</Tooltip>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 130,
      render: (_, task) => (
        <Space>
          <Button size="small" onClick={() => navigate(`/tasks/${encodeURIComponent(task.task_id)}`)}>
            详情
          </Button>
          <Button
            size="small"
            type="primary"
            onClick={() => navigate(`/tasks?search=${encodeURIComponent(task.task_id)}`)}
          >
            定位
          </Button>
        </Space>
      ),
    },
  ]

  const runColumns: ColumnsType<RunRecord> = [
    {
      title: '运行',
      key: 'run',
      render: (_, run) => (
        <Space direction="vertical" size={0}>
          <Link to={`/tasks/${encodeURIComponent(run.task_id)}/runs/${encodeURIComponent(run.run_id)}`}>
            {shortID(run.run_id)}
          </Link>
          <Text type="secondary" style={{ fontSize: 12 }}>{run.task_id}</Text>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'run_status',
      width: 96,
      render: (status: string) => <Tag color={runStatusColor(status)}>{RUN_STATUS_LABELS[status] || status || '—'}</Tag>,
    },
    {
      title: '检查',
      dataIndex: 'check_status',
      width: 96,
      render: (status: string) => <Tag color={checkStatusColor(status)}>{CHECK_STATUS_LABELS[status] || status || '—'}</Tag>,
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      width: 90,
      render: (value: number) => formatDuration(value),
    },
    {
      title: '开始',
      dataIndex: 'started_at',
      width: 150,
      render: (value: string) => <Tooltip title={formatTime(value)}>{formatRelativeTime(value)}</Tooltip>,
    },
    {
      title: '错误',
      dataIndex: 'error_message',
      render: (value: string) => value ? <Text type="danger" ellipsis>{value}</Text> : <Text type="secondary">—</Text>,
    },
  ]

  if (loading) {
    return (
      <div className="page-shell" style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div className="page-shell">
      <div className="page-header">
        <div>
          <Title level={2} className="page-title">工作台</Title>
          <Text className="page-subtitle">
            异常优先展示运行态、影响标签和最近执行，刷新间隔 {AUTO_REFRESH_MS / 1000} 秒。
          </Text>
        </div>
        <Space wrap>
          <Text type="secondary">
            最近刷新：{lastRefreshAt ? lastRefreshAt.toLocaleTimeString() : '—'}
          </Text>
          <Button icon={<ReloadOutlined />} onClick={() => { setLoading(true); loadData() }}>
            手动刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/task-defs/new?from=/')}>
            创建任务
          </Button>
        </Space>
      </div>

      {error && (
        <Alert
          type="error"
          message="工作台数据加载失败"
          description={error}
          action={<Button size="small" onClick={() => { setLoading(true); loadData() }}>重试</Button>}
          showIcon
          style={{ marginBottom: 16 }}
        />
      )}

      <Card className="ops-card" style={{ marginBottom: 14 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 18, flexWrap: 'wrap' }}>
          <Space size={14}>
            <span
              style={{
                width: 14,
                height: 14,
                borderRadius: '50%',
                background: health.color,
                display: 'inline-block',
              }}
            />
            <div>
              <Text strong style={{ fontSize: 17 }}>{health.label}</Text>
              <Text type="secondary" style={{ display: 'block', marginTop: 2 }}>{health.detail}</Text>
            </div>
          </Space>
          <Space wrap>
            <Button icon={<UnorderedListOutlined />} onClick={() => navigate('/tasks?runStatus=failed')}>
              查看全部失败
            </Button>
            <Button icon={<ApartmentOutlined />} onClick={() => navigate('/pipelines')}>
              进入依赖拓扑
            </Button>
          </Space>
        </div>
      </Card>

      <div className="metric-strip" style={{ marginBottom: 14 }}>
        <div className="metric-tile" onClick={() => navigate('/tasks')}>
          <div className="metric-label">任务总数</div>
          <div className="metric-value">{summary.total}</div>
        </div>
        <div className="metric-tile" onClick={() => navigate('/tasks?enabled=true')}>
          <div className="metric-label">启用任务</div>
          <div className="metric-value" style={{ color: '#0f9f7a' }}>{summary.enabled}</div>
        </div>
        <div className="metric-tile" onClick={() => navigate('/tasks?runStatus=failed')}>
          <div className="metric-label">失败任务</div>
          <div className="metric-value" style={{ color: '#cf1322' }}>{summary.failed}</div>
        </div>
        <div className="metric-tile" onClick={() => navigate('/tasks?checkStatus=fail')}>
          <div className="metric-label">检查未通过</div>
          <div className="metric-value" style={{ color: '#cf1322' }}>{summary.checkFailed}</div>
        </div>
        <div className="metric-tile" onClick={() => navigate('/tasks?loadError=1')}>
          <div className="metric-label">加载失败</div>
          <div className="metric-value" style={{ color: '#fa8c16' }}>{summary.loadFailed}</div>
        </div>
      </div>

      <div className="two-column-wide">
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card
            className="ops-card"
            title="异常队列"
            extra={<Text type="secondary">{anomalies.length} 项</Text>}
          >
            {anomalies.length === 0 ? (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="当前没有异常任务"
              />
            ) : (
              <Table<TaskView>
                className="dense-table"
                columns={anomalyColumns}
                dataSource={anomalies}
                rowKey="task_id"
                pagination={false}
                size="small"
              />
            )}
          </Card>

          <Card
            className="ops-card"
            title="最近运行"
            extra={
              <Space size={8}>
                <ClockCircleOutlined />
                <Text type="secondary">{abnormalRuns} 条异常执行</Text>
              </Space>
            }
          >
            {recentRuns.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无运行记录" />
            ) : (
              <Table<RunRecord>
                className="dense-table"
                columns={runColumns}
                dataSource={recentRuns}
                rowKey="run_id"
                pagination={false}
                size="small"
              />
            )}
          </Card>
        </Space>

        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card className="ops-card" title="标签异常分布">
            {labelAgg.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无标签数据" />
            ) : (
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                {labelAgg.slice(0, 10).map((item: LabelAggregate) => {
                  const percent = item.total === 0 ? 0 : Math.round((item.abnormal / item.total) * 100)
                  return (
                    <div key={`${item.key}:${item.value}`}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                        <Space size={6}>
                          <Tag>{item.key}</Tag>
                          <Text strong>{item.value}</Text>
                        </Space>
                        <Text type={item.abnormal > 0 ? 'danger' : 'secondary'}>
                          {item.abnormal}/{item.total}
                        </Text>
                      </div>
                      <Progress
                        percent={percent}
                        size="small"
                        strokeColor={item.abnormal > 0 ? '#cf1322' : '#0f9f7a'}
                        showInfo={false}
                      />
                    </div>
                  )
                })}
              </Space>
            )}
          </Card>

          <Card className="ops-card" title="快捷入口">
            <Space direction="vertical" style={{ width: '100%' }}>
              <Button block icon={<PlusOutlined />} onClick={() => navigate('/task-defs/new?from=/')}>
                创建巡检任务
              </Button>
              <Button block icon={<UnorderedListOutlined />} onClick={() => navigate('/tasks?sort=abnormal')}>
                打开异常优先列表
              </Button>
              <Button block icon={<ApartmentOutlined />} onClick={() => navigate('/pipelines')}>
                查看依赖拓扑
              </Button>
              <Button
                block
                icon={<ReloadOutlined />}
                onClick={() => {
                  message.info('已触发刷新')
                  loadData()
                }}
              >
                刷新工作台数据
              </Button>
            </Space>
          </Card>
        </Space>
      </div>
    </div>
  )
}
