import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Popconfirm,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  ApartmentOutlined,
  ArrowLeftOutlined,
  EditOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import {
  disableTask,
  enableTask,
  fetchTask,
  fetchTaskAIAnalyses,
  fetchTaskDefinitions,
  fetchTaskRunsPaginated,
  fetchTaskRunStats,
  fetchTaskSample,
  triggerTaskRun,
} from '../api/client'
import type {
  AIAnalysisRecord,
  RunListItem,
  RunStat,
  SampleResponse,
  TaskDefinition,
  TaskView as APITaskView,
} from '../api/types'
import DurationChart from '../components/DurationChart'
import {
  AUTO_REFRESH_MS,
  checkStatusColor,
  CHECK_STATUS_LABELS,
  collectDownstream,
  collectUpstream,
  firstLine,
  formatDuration,
  formatRelativeTime,
  formatTime,
  KIND_COLORS,
  KIND_LABELS,
  runStatusColor,
  RUN_STATUS_LABELS,
  safeJson,
  shortID,
  statusColorForTask,
  decorateTaskView,
  type TaskView,
} from '../utils/pulseops'

const { Title, Text, Paragraph } = Typography

const TIME_RANGE_OPTIONS = [
  { label: '最近 24 小时', value: '24h' },
  { label: '最近 7 天', value: '168h' },
  { label: '最近 30 天', value: '720h' },
  { label: '全部', value: '' },
]

function sampleDisplayData(sample: SampleResponse | null | undefined): unknown {
  return sample?.display_data ?? sample?.data
}

function isSamplePreviewSource(source: string): boolean {
  return !source.startsWith('artifact:') || source === 'artifact:payload'
}

function taskAdvice(task: TaskView): string {
  if (task.last_reload_error) return '先编辑配置并修复加载错误，再刷新任务运行态。'
  if (task.last_run_status === 'failed' || task.last_run_status === 'timeout') return '优先打开最近失败运行，确认错误和 payload，再决定重跑或修改配置。'
  if (task.last_check_status === 'fail') return '检查未通过，建议查看 summary/findings 并确认阈值或上游数据。'
  if (task.status === 'unloaded') return '任务定义存在但运行态未加载，建议刷新或检查后端加载日志。'
  return '当前任务未发现阻断问题，可查看趋势或进入依赖拓扑确认影响面。'
}

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const returnUrl = searchParams.get('from') || '/tasks'

  const [task, setTask] = useState<APITaskView | null>(null)
  const [defs, setDefs] = useState<TaskDefinition[]>([])
  const [chartRuns, setChartRuns] = useState<RunStat[]>([])
  const [tableRuns, setTableRuns] = useState<RunListItem[]>([])
  const [total, setTotal] = useState(0)
  const [analyses, setAnalyses] = useState<AIAnalysisRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [timeRange, setTimeRange] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [lastRefreshAt, setLastRefreshAt] = useState<Date | null>(null)

  const [samples, setSamples] = useState<Record<string, SampleResponse | null>>({})
  const [samplesLoading, setSamplesLoading] = useState(false)
  const [jqResults, setJqResults] = useState<Record<string, unknown>>({})
  const [jqLoading, setJqLoading] = useState(false)

  const fetchData = useCallback(async () => {
    if (!id) return
    try {
      const offset = (page - 1) * pageSize
      const [taskData, allDefs, stats, runs, ai] = await Promise.all([
        fetchTask(id),
        fetchTaskDefinitions(),
        fetchTaskRunStats(id, timeRange || undefined),
        fetchTaskRunsPaginated(id, pageSize, offset, timeRange || undefined),
        fetchTaskAIAnalyses(id, 10),
      ])
      setTask(taskData)
      setDefs(allDefs)
      setChartRuns(stats)
      setTableRuns(runs.records)
      setTotal(runs.total)
      setAnalyses(ai)
      setLastRefreshAt(new Date())
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败')
    } finally {
      setLoading(false)
    }
  }, [id, page, pageSize, timeRange])

  useEffect(() => {
    setLoading(true)
    fetchData()
    const interval = setInterval(fetchData, AUTO_REFRESH_MS)
    return () => clearInterval(interval)
  }, [fetchData])

  const def = useMemo(() => task?.definition || defs.find((item) => item.task_id === id), [defs, id, task])
  const view = useMemo(() => (task ? decorateTaskView(task) : null), [task])
  const upstream = useMemo(() => collectUpstream(defs, def), [defs, def])
  const downstream = useMemo(() => (id ? collectDownstream(defs, id) : []), [defs, id])
  const latestFailedRun = useMemo(
    () => tableRuns.find((run) => run.run_status === 'failed' || run.run_status === 'timeout' || run.check_status === 'fail'),
    [tableRuns],
  )
  const latestAI = analyses[0]

  useEffect(() => {
    if (!def) return
    const params = def.params || {}
    const upstreamId = (params.source_task_id as string) || def.watch_task_id || def.dependencies?.[0]?.upstream_task_id || ''
    const exprs = params.extract_exprs as Array<{ source: string; jq_expr?: string }> | undefined
    if (def.kind !== 'data_process' || !upstreamId || !exprs?.length) {
      setSamples({})
      setJqResults({})
      setSamplesLoading(false)
      setJqLoading(false)
      return
    }

    const uniqueSources = [...new Set(exprs.map((expr) => expr.source).filter(isSamplePreviewSource))]
    setSamplesLoading(uniqueSources.length > 0)
    Promise.allSettled(uniqueSources.map((source) => fetchTaskSample(upstreamId, source).then((result) => [source, result] as const)))
      .then((results) => {
        const next: Record<string, SampleResponse | null> = {}
        for (const result of results) {
          if (result.status === 'fulfilled') {
            const [source, sample] = result.value
            next[source] = sample
          }
        }
        setSamples(next)
      })
      .finally(() => setSamplesLoading(false))

    const jqExprs = exprs.filter((expr) => expr.source && expr.jq_expr && isSamplePreviewSource(expr.source))
    setJqLoading(jqExprs.length > 0)
    Promise.allSettled(jqExprs.map((expr) => {
      const key = `${expr.source}::${expr.jq_expr}`
      return fetchTaskSample(upstreamId, expr.source, expr.jq_expr).then((result) => [key, result.jq_result] as const)
    }))
      .then((results) => {
        const next: Record<string, unknown> = {}
        for (const result of results) {
          if (result.status === 'fulfilled') {
            const [key, value] = result.value
            next[key] = value
          }
        }
        setJqResults(next)
      })
      .finally(() => setJqLoading(false))
  }, [def])

  const handleRun = async () => {
    if (!id) return
    setActionLoading(true)
    try {
      const run = await triggerTaskRun(id)
      message.success('任务已触发')
      navigate(`/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(run.run_id)}`)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '触发失败')
    } finally {
      setActionLoading(false)
      fetchData()
    }
  }

  const handleToggleEnabled = async (enabled: boolean) => {
    if (!id) return
    setActionLoading(true)
    try {
      if (enabled) {
        await enableTask(id)
        message.success('任务已启用')
      } else {
        await disableTask(id)
        message.success('任务已禁用')
      }
      await fetchData()
    } catch (err) {
      message.error(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActionLoading(false)
    }
  }

  const runColumns: ColumnsType<RunListItem> = [
    {
      title: '运行 ID',
      dataIndex: 'run_id',
      width: 140,
      render: (value: string) => <Link to={`/tasks/${id}/runs/${value}`}><Text code>{shortID(value)}</Text></Link>,
    },
    {
      title: '触发方式',
      dataIndex: 'trigger_type',
      width: 100,
      render: (value: string) => <Tag>{value || '—'}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'run_status',
      width: 100,
      render: (value: string) => <Tag color={runStatusColor(value)}>{RUN_STATUS_LABELS[value] || value || '—'}</Tag>,
    },
    {
      title: '检查',
      dataIndex: 'check_status',
      width: 100,
      render: (value: string) => <Tag color={checkStatusColor(value)}>{CHECK_STATUS_LABELS[value] || value || '—'}</Tag>,
    },
    {
      title: '开始时间',
      dataIndex: 'started_at',
      width: 160,
      render: (value: string) => <Tooltip title={formatTime(value)}>{formatRelativeTime(value)}</Tooltip>,
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      width: 90,
      render: (value: number) => formatDuration(value),
    },
    {
      title: '错误',
      dataIndex: 'error_message',
      render: (value: string) => value
        ? <Tooltip title={value}><Text type="danger" ellipsis>{value}</Text></Tooltip>
        : <Text type="secondary">—</Text>,
    },
  ]

  if (loading && !task) {
    return (
      <div className="page-shell" style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  if (error && !task) {
    return (
      <div className="page-shell">
        <Alert
          type="error"
          message="加载任务失败"
          description={error}
          action={<Button onClick={() => { setLoading(true); fetchData() }}>重试</Button>}
          showIcon
        />
      </div>
    )
  }

  if (!task || !view) {
    return (
      <div className="page-shell">
        <Alert type="warning" message="任务未找到" showIcon />
      </div>
    )
  }

  const sourceLabels: Record<string, string> = {
    payload: 'Payload',
    summary: 'Summary',
    record: 'Record',
    'artifact:payload': 'Artifact Payload',
    'artifact:stdout': 'Artifact Stdout',
    'artifact:stderr': 'Artifact Stderr',
  }
  const extractExprs = def?.params?.extract_exprs as Array<{ field: string; source: string; jq_expr: string; agg_mode?: string }> | undefined
  const dataSourceTaskId = ((def?.params || {}).source_task_id as string) || def?.watch_task_id || def?.dependencies?.[0]?.upstream_task_id || ''

  return (
    <div className="page-shell">
      <div className="page-header">
        <div>
          <Link to={returnUrl}>
            <Space size={6}>
              <ArrowLeftOutlined />
              <span>返回</span>
            </Space>
          </Link>
          <Space align="center" size={10} wrap style={{ marginTop: 10 }}>
            <Title level={2} className="page-title">{task.name}</Title>
            <Tag color={KIND_COLORS[task.kind] || 'default'}>{KIND_LABELS[task.kind] || task.kind}</Tag>
            <Tag color={statusColorForTask(view)}>{view.anomaly_type || (task.enabled ? '运行态正常' : '已禁用')}</Tag>
          </Space>
          <Text className="page-subtitle">
            {task.task_id}，最近刷新：{lastRefreshAt ? lastRefreshAt.toLocaleTimeString() : '—'}
          </Text>
        </div>
        <Space wrap>
          <Text>启用</Text>
          <Switch checked={task.enabled} loading={actionLoading} onChange={handleToggleEnabled} />
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => { setLoading(true); fetchData() }}>
            刷新
          </Button>
          <Button type="primary" icon={<PlayCircleOutlined />} loading={actionLoading} disabled={task.status === 'unloaded'} onClick={handleRun}>
            立即执行
          </Button>
          <Button icon={<EditOutlined />} onClick={() => navigate(`/task-defs/${encodeURIComponent(task.task_id)}/edit?from=/tasks/${encodeURIComponent(task.task_id)}`)}>
            编辑配置
          </Button>
          <Button icon={<ApartmentOutlined />} onClick={() => navigate(def?.pipeline_id ? `/pipelines/${def.pipeline_id}` : '/pipelines')}>
            进入拓扑
          </Button>
        </Space>
      </div>

      {error && (
        <Alert type="warning" message="刷新失败，当前展示最近一次成功数据" description={error} showIcon style={{ marginBottom: 14 }} />
      )}

      <div className="two-column-main">
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card className="ops-card" title="健康摘要">
            <Descriptions column={{ xs: 1, md: 2, xl: 4 }} size="small">
              <Descriptions.Item label="运行态">
                <Tag color={statusColorForTask(view)}>{task.status || '—'}</Tag>
              </Descriptions.Item>
              <Descriptions.Item label="上次运行">
                <Tag color={runStatusColor(task.last_run_status)}>{RUN_STATUS_LABELS[task.last_run_status] || task.last_run_status || '—'}</Tag>
              </Descriptions.Item>
              <Descriptions.Item label="检查结果">
                <Tag color={checkStatusColor(task.last_check_status)}>{CHECK_STATUS_LABELS[task.last_check_status] || task.last_check_status || '—'}</Tag>
              </Descriptions.Item>
              <Descriptions.Item label="最近耗时">{formatDuration(task.last_duration_ms)}</Descriptions.Item>
              <Descriptions.Item label="最近成功">{formatTime(tableRuns.find((run) => run.run_status === 'success')?.started_at)}</Descriptions.Item>
              <Descriptions.Item label="上次运行时间">{formatTime(task.last_run_at)}</Descriptions.Item>
              <Descriptions.Item label="下次运行时间">{formatTime(task.next_run_at)}</Descriptions.Item>
              <Descriptions.Item label="连续失败">{tableRuns.filter((run) => run.run_status === 'failed' || run.run_status === 'timeout').length}</Descriptions.Item>
            </Descriptions>

            {(task.last_error || task.last_reload_error || latestFailedRun) && (
              <Alert
                type={view.severity === 'critical' ? 'error' : 'warning'}
                message={view.anomaly_type || '最近异常'}
                description={task.last_reload_error || task.last_error || latestFailedRun?.error_message || '检查最近运行详情'}
                action={latestFailedRun ? (
                  <Button size="small" onClick={() => navigate(`/tasks/${task.task_id}/runs/${latestFailedRun.run_id}`)}>
                    打开运行
                  </Button>
                ) : undefined}
                showIcon
                style={{ marginTop: 14 }}
              />
            )}
          </Card>

          <Card className="ops-card" title="依赖信息">
            <Descriptions column={{ xs: 1, md: 3 }} size="small">
              <Descriptions.Item label="所属任务组">
                {def?.pipeline_id ? <Link to={`/pipelines/${def.pipeline_id}`}>{def.pipeline_id}</Link> : <Text type="secondary">未分配</Text>}
              </Descriptions.Item>
              <Descriptions.Item label="上游任务">
                {upstream.length > 0 ? upstream.map((item) => <Tag key={item.task_id}>{item.name}</Tag>) : <Text type="secondary">无</Text>}
              </Descriptions.Item>
              <Descriptions.Item label="下游任务">
                {downstream.length > 0 ? downstream.map((item) => <Tag key={item.task_id}>{item.name}</Tag>) : <Text type="secondary">无</Text>}
              </Descriptions.Item>
              <Descriptions.Item label="触发条件">
                {def?.dependencies?.length
                  ? def.dependencies.map((dep) => <Tag key={dep.id || dep.upstream_task_id}>{dep.condition || '总是触发'}</Tag>)
                  : def?.watch_condition ? <Text code>{def.watch_condition}</Text> : <Text type="secondary">不限制或非依赖触发</Text>}
              </Descriptions.Item>
            </Descriptions>
          </Card>

          <Card
            className="ops-card"
            title={`耗时趋势 (${chartRuns.length})`}
            extra={<Select size="small" value={timeRange} onChange={setTimeRange} options={TIME_RANGE_OPTIONS} style={{ width: 136 }} />}
          >
            <DurationChart runs={chartRuns} />
          </Card>

          <Card className="ops-card" title={`运行历史 (${total})`}>
              <Table<RunListItem>
              className="dense-table"
              columns={runColumns}
              dataSource={tableRuns}
              rowKey="run_id"
              size="small"
              pagination={{
                current: page,
                pageSize,
                total,
                showSizeChanger: true,
                showTotal: (value) => `共 ${value} 条`,
                onChange: (nextPage, nextPageSize) => {
                  setPage(nextPage)
                  setPageSize(nextPageSize)
                },
              }}
              expandable={{
                rowExpandable: (run) => Boolean(run.summary || run.has_payload || run.artifact_count || run.finding_count),
                expandedRowRender: (run) => (
                  <Space direction="vertical" style={{ width: '100%' }}>
                    {run.summary && (
                      <pre className="code-block">{safeJson(run.summary)}</pre>
                    )}
                    <Text type="secondary">
                      Payload：{run.has_payload ? '有' : '无'}，产物 {run.artifact_count} 个，检查发现 {run.finding_count} 条。
                    </Text>
                  </Space>
                ),
              }}
            />
          </Card>
        </Space>

        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card className="ops-card" title="推荐动作">
            <Space direction="vertical" style={{ width: '100%' }}>
              <Paragraph style={{ marginBottom: 4 }}>{taskAdvice(view)}</Paragraph>
              {latestFailedRun && (
                <Button block type="primary" onClick={() => navigate(`/tasks/${task.task_id}/runs/${latestFailedRun.run_id}`)}>
                  打开最近异常运行
                </Button>
              )}
              <Button block onClick={handleRun} disabled={task.status === 'unloaded'} loading={actionLoading}>
                重跑任务
              </Button>
              <Popconfirm
                title={task.enabled ? '确认禁用该任务？' : '确认启用该任务？'}
                okText={task.enabled ? '禁用' : '启用'}
                cancelText="取消"
                okButtonProps={{ danger: task.enabled }}
                onConfirm={() => handleToggleEnabled(!task.enabled)}
              >
                <Button block danger={task.enabled}>
                  {task.enabled ? '禁用任务' : '启用任务'}
                </Button>
              </Popconfirm>
            </Space>
          </Card>

          <Card className="ops-card" title="配置摘要">
            {def ? (
              <Descriptions column={1} size="small">
                <Descriptions.Item label="调度">
                  {def.trigger === 'manual' ? '手动' : def.cron || def.interval || '未设置'}
                </Descriptions.Item>
                <Descriptions.Item label="超时">{def.timeout || '默认'}</Descriptions.Item>
                <Descriptions.Item label="留痕">
                  {def.trace?.level || '默认'} / 保留 {def.trace?.retain_days || '默认'} 天
                </Descriptions.Item>
                <Descriptions.Item label="标签">
                  {Object.entries(def.labels || {}).length > 0
                    ? Object.entries(def.labels || {}).map(([key, value]) => <Tag key={key}>{key}: {value}</Tag>)
                    : <Text type="secondary">—</Text>}
                </Descriptions.Item>
              </Descriptions>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未找到任务定义" />
            )}
          </Card>

          <Card className="ops-card" title="AI 分析">
            {latestAI ? (
              <Space direction="vertical" style={{ width: '100%' }}>
                <Space wrap>
                  <Tag color="purple">{latestAI.analysis_type}</Tag>
                  <Tag color={latestAI.status === 'success' ? 'green' : 'red'}>{latestAI.status === 'success' ? '成功' : '失败'}</Tag>
                  <Text type="secondary">{latestAI.model}</Text>
                </Space>
                {latestAI.error_message && <Alert type="error" message={latestAI.error_message} showIcon />}
                <Paragraph ellipsis={{ rows: 5, expandable: true, symbol: '展开' }} style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>
                  {firstLine(latestAI.response, 600) || '暂无结论'}
                </Paragraph>
                <Text type="secondary">
                  Token {latestAI.tokens_in + latestAI.tokens_out}，耗时 {formatDuration(latestAI.duration_ms)}
                </Text>
              </Space>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 AI 分析" />
            )}
          </Card>
        </Space>
      </div>

      {def?.kind === 'data_process' && extractExprs && extractExprs.length > 0 && (
        <Card className="ops-card" title="数据处理规则和样本" style={{ marginTop: 14 }}>
          <Descriptions column={{ xs: 1, md: 2 }} size="small" style={{ marginBottom: 12 }}>
            <Descriptions.Item label="源任务">
              {dataSourceTaskId ? <Link to={`/tasks/${dataSourceTaskId}`}>{dataSourceTaskId}</Link> : <Text type="secondary">未设置</Text>}
            </Descriptions.Item>
            <Descriptions.Item label="表达式数量">{extractExprs.length}</Descriptions.Item>
          </Descriptions>
          <Table
            className="dense-table"
            dataSource={extractExprs.map((expr, index) => ({ ...expr, key: index }))}
            pagination={false}
            size="small"
            columns={[
              { title: '输出字段', dataIndex: 'field', render: (value: string) => <Text code>{value}</Text> },
              { title: '数据源', dataIndex: 'source', render: (value: string) => <Tag>{sourceLabels[value] || value}</Tag> },
              { title: 'JQ / 字段选择', dataIndex: 'jq_expr', render: (value: string) => <Text code>{value}</Text> },
              {
                title: '样本值',
                render: (_, record: { source: string; jq_expr: string }) => {
                  const key = `${record.source}::${record.jq_expr}`
                  const value = jqResults[key]
                  if (jqLoading && !(key in jqResults)) return <Text type="secondary">加载中</Text>
                  if (value === undefined || value === null) return <Text type="secondary">—</Text>
                  return <Text code>{typeof value === 'object' ? safeJson(value) : String(value)}</Text>
                },
              },
              { title: '聚合', dataIndex: 'agg_mode', render: (value: string) => value ? <Tag>{value}</Tag> : <Text type="secondary">无</Text> },
            ]}
          />
          <div style={{ marginTop: 12 }}>
            {samplesLoading ? (
              <Spin />
            ) : Object.keys(samples).length > 0 ? (
              <Space direction="vertical" style={{ width: '100%' }}>
                {Object.entries(samples).map(([source, sample]) => (
                  <Card key={source} size="small" title={<Tag>{sourceLabels[source] || source}</Tag>}>
                    {sample?.available ? (
                      <pre className="code-block">{safeJson(sampleDisplayData(sample))}</pre>
                    ) : (
                      <Text type="secondary">{sample?.message || '暂无样本数据'}</Text>
                    )}
                  </Card>
                ))}
              </Space>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无可预览样本" />
            )}
          </div>
        </Card>
      )}
    </div>
  )
}
