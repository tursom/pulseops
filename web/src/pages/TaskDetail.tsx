import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, Link, useNavigate, useSearchParams } from 'react-router-dom'
import {
  Spin,
  Alert,
  Typography,
  Card,
  Descriptions,
  Tag,
  Badge,
  Table,
  Button,
  Switch,
  Collapse,
  Tooltip,
  Space,
  Select,
  message,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { PlayCircleOutlined, ArrowLeftOutlined, EditOutlined } from '@ant-design/icons'
import {
  fetchTask,
  fetchTaskRuns,
  fetchTaskAIAnalyses,
  triggerTaskRun,
  enableTask,
  disableTask,
} from '../api/client'
import type { TaskState, RunRecord, AIAnalysisRecord, TaskDefinition } from '../api/types'
import DurationChart from '../components/DurationChart'

const { Title, Text } = Typography

function formatTime(t: string | null): string {
  if (!t) return '—'
  return new Date(t).toLocaleString()
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function shortRunID(id: string): string {
  return id.substring(0, 12)
}

const CHAR_LIMIT = 500

const TIME_RANGE_OPTIONS = [
  { label: '最近24小时', value: '24h' },
  { label: '最近7天', value: '168h' },
  { label: '最近30天', value: '720h' },
  { label: '全部', value: '' },
]

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const returnUrl = searchParams.get('from') || '/tasks'

  const [task, setTask] = useState<TaskState | null>(null)
  const [runs, setRuns] = useState<RunRecord[]>([])
  const [analyses, setAnalyses] = useState<AIAnalysisRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [expandedAnalyses, setExpandedAnalyses] = useState<Set<number>>(new Set())
  const [timeRange, setTimeRange] = useState<string>('')

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchData = useCallback(async () => {
    if (!id) return
    try {
      const [t, r, a] = await Promise.all([
        fetchTask(id),
        fetchTaskRuns(id, 100, timeRange || undefined),
        fetchTaskAIAnalyses(id, 10),
      ])
      setTask(t)
      setRuns(r)
      setAnalyses(a)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败')
    } finally {
      setLoading(false)
    }
  }, [id, timeRange])

  useEffect(() => {
    setLoading(true)
    fetchData()
    intervalRef.current = setInterval(fetchData, 15_000)
    return () => {
      if (intervalRef.current !== null) clearInterval(intervalRef.current)
    }
  }, [fetchData])

  const handleRun = async () => {
    if (!id) return
    setActionLoading(true)
    try {
      await triggerTaskRun(id)
      message.success('任务已触发')
      await fetchData()
    } catch (err) {
      message.error(err instanceof Error ? err.message : '触发失败')
    } finally {
      setActionLoading(false)
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

  const toggleAnalysisExpand = (analysisId: number) => {
    setExpandedAnalyses((prev) => {
      const next = new Set(prev)
      if (next.has(analysisId)) {
        next.delete(analysisId)
      } else {
        next.add(analysisId)
      }
      return next
    })
  }

  const runStatusBadge = (status: string) => {
    const map: Record<string, 'success' | 'error' | 'warning' | 'processing'> = {
      success: 'success',
      failed: 'error',
      timeout: 'warning',
      running: 'processing',
    }
    const labels: Record<string, string> = {
      success: '成功',
      failed: '失败',
      timeout: '超时',
      running: '运行中',
    }
    return <Badge status={map[status] || 'default'} text={labels[status] || status} />
  }

  const checkStatusBadge = (status: string) => {
    const map: Record<string, 'success' | 'error'> = {
      pass: 'success',
      fail: 'error',
    }
    const labels: Record<string, string> = {
      pass: '通过',
      fail: '失败',
    }
    return <Badge status={map[status] || 'default'} text={labels[status] || status} />
  }

  const triggerTypeTag = (tt: string) => {
    const colors: Record<string, string> = {
      scheduled: 'blue',
      manual: 'green',
      rerun: 'orange',
      dependent: 'purple',
    }
    const labels: Record<string, string> = {
      scheduled: '定时',
      manual: '手动',
      rerun: '重跑',
      dependent: '依赖触发',
    }
    return <Tag color={colors[tt] || 'default'}>{labels[tt] || tt}</Tag>
  }

  const taskStatusBadge = (status: string) => {
    const s = status.toLowerCase()
    if (s === 'ok' || s === 'healthy') return <Badge status="success" text={status} />
    if (s === 'error' || s === 'unhealthy') return <Badge status="error" text={status} />
    if (s === 'degraded') return <Badge status="warning" text={status} />
    return <Badge status="default" text={status} />
  }

  if (loading) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          height: '60vh',
        }}
      >
        <Spin size="large" />
      </div>
    )
  }

  if (error) {
    return (
      <Alert
        type="error"
        message="加载任务失败"
        description={error}
        showIcon
      />
    )
  }

  if (!task) {
    return <Alert type="warning" message="任务未找到" showIcon />
  }

  const runColumns: TableColumnsType<RunRecord> = [
    {
      title: '运行ID',
      dataIndex: 'run_id',
      key: 'run_id',
      render: (v: string) => <Link to={`/tasks/${id}/runs/${v}`}><Text code>{shortRunID(v)}</Text></Link>,
    },
    {
      title: '触发方式',
      dataIndex: 'trigger_type',
      key: 'trigger_type',
      render: (v: string) => triggerTypeTag(v),
    },
    {
      title: '状态',
      dataIndex: 'run_status',
      key: 'run_status',
      render: (v: string) => runStatusBadge(v),
    },
    {
      title: '检查',
      dataIndex: 'check_status',
      key: 'check_status',
      render: (v: string) => checkStatusBadge(v),
    },
    {
      title: '开始时间',
      dataIndex: 'started_at',
      key: 'started_at',
      render: (v: string) => formatTime(v),
      sorter: (a, b) =>
        new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
      defaultSortOrder: 'descend',
    },
    {
      title: '耗时',
      dataIndex: 'duration_ms',
      key: 'duration_ms',
      render: (v: number) => formatDuration(v),
    },
    {
      title: '错误',
      dataIndex: 'error_message',
      key: 'error_message',
      render: (v: string) => {
        if (!v) return <Text type="secondary">—</Text>
        return (
          <Tooltip title={v}>
            <Text type="danger" ellipsis style={{ maxWidth: 200 }}>
              {v}
            </Text>
          </Tooltip>
        )
      },
    },
  ]

  const expandedRowRender = (record: RunRecord) => (
    <div style={{ padding: '8px 0' }}>
      {record.summary && (
        <div style={{ marginBottom: 12 }}>
          <Text strong>摘要</Text>
          <pre
            style={{
              background: '#f5f5f5',
              padding: 12,
              borderRadius: 6,
              maxHeight: 200,
              overflow: 'auto',
              fontSize: 12,
              marginTop: 4,
            }}
          >
            {JSON.stringify(record.summary, null, 2)}
          </pre>
        </div>
      )}
      {record.stdout && (
        <div style={{ marginBottom: 12 }}>
          <Text strong>标准输出</Text>
          <pre
            style={{
              background: '#f5f5f5',
              padding: 12,
              borderRadius: 6,
              maxHeight: 200,
              overflow: 'auto',
              fontSize: 12,
              marginTop: 4,
            }}
          >
            {record.stdout}
          </pre>
        </div>
      )}
      {record.stderr && (
        <div style={{ marginBottom: 12 }}>
          <Text strong>标准错误</Text>
          <pre
            style={{
              background: '#fff2f0',
              padding: 12,
              borderRadius: 6,
              maxHeight: 200,
              overflow: 'auto',
              fontSize: 12,
              color: '#cf1322',
              marginTop: 4,
              border: '1px solid #ffccc7',
            }}
          >
            {record.stderr}
          </pre>
        </div>
      )}
      {!record.summary && !record.stdout && !record.stderr && (
        <Text type="secondary">无详细信息</Text>
      )}
    </div>
  )

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <Link to={returnUrl} style={{ display: 'inline-block', marginBottom: 12 }}>
          <Space>
            <ArrowLeftOutlined />
            <span>返回任务列表</span>
          </Space>
        </Link>

        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            flexWrap: 'wrap',
            gap: 12,
          }}
        >
          <Space align="center" size={12} wrap>
            <Title level={2} style={{ margin: 0 }}>
              {task.name}
            </Title>
            <Tag>{task.kind}</Tag>
            {taskStatusBadge(task.status)}
          </Space>

          <Space>
            <span>已启用</span>
            <Switch
              checked={task.enabled}
              onChange={handleToggleEnabled}
              loading={actionLoading}
            />
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              onClick={handleRun}
              loading={actionLoading}
            >
              立即执行
            </Button>
            <Button
              icon={<EditOutlined />}
              onClick={() => navigate(`/task-defs/${encodeURIComponent(id!)}/edit?from=/tasks/${encodeURIComponent(id!)}`)}
            >
              编辑
            </Button>
          </Space>
        </div>
      </div>

      <Card title="任务信息" style={{ marginBottom: 24 }}>
        <Descriptions bordered column={{ xs: 1, sm: 2, lg: 3 }}>
          <Descriptions.Item label="任务ID">
            <Text code>{task.task_id}</Text>
          </Descriptions.Item>
          <Descriptions.Item label="类型">
            <Tag>{task.kind}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            {taskStatusBadge(task.status)}
          </Descriptions.Item>
          <Descriptions.Item label="已启用">
            <Tag color={task.enabled ? 'green' : 'red'}>
              {task.enabled ? '是' : '否'}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="上次运行">
            {formatTime(task.last_run_at)}
          </Descriptions.Item>
          <Descriptions.Item label="下次运行">
            {formatTime(task.next_run_at)}
          </Descriptions.Item>
          <Descriptions.Item label="上次状态">
            {runStatusBadge(task.last_run_status)}
          </Descriptions.Item>
          <Descriptions.Item label="上次错误">
            {task.last_error ? (
              <Text type="danger" ellipsis style={{ maxWidth: 300 }}>
                {task.last_error}
              </Text>
            ) : (
              <Text type="secondary">—</Text>
            )}
          </Descriptions.Item>
          <Descriptions.Item label="上次耗时">
            {formatDuration(task.last_duration_ms)}
          </Descriptions.Item>
          <Descriptions.Item label="来源路径">
            <Text code>{task.source_path}</Text>
          </Descriptions.Item>
          <Descriptions.Item label="更新时间">
            {formatTime(task.updated_at)}
          </Descriptions.Item>
          <Descriptions.Item label="标签">
            {Object.keys(task.labels).length > 0 ? (
              <Space wrap size={[4, 4]}>
                {Object.entries(task.labels).map(([k, v]) => (
                  <Tag key={k}>
                    {k}: {v}
                  </Tag>
                ))}
              </Space>
            ) : (
              <Text type="secondary">—</Text>
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {task.kind === 'data_process' && (() => {
        const def = (task as unknown as { definition?: TaskDefinition }).definition
        const exprs = def?.params?.extract_exprs as Array<{ field: string; source: string; jq_expr: string; agg_mode?: string }> | undefined
        if (!exprs || exprs.length === 0) return null
        const sourceLabels: Record<string, string> = {
          payload: 'Payload',
          summary: 'Summary',
          record: 'Record',
          'artifact:payload': 'Artifact:Payload',
          'artifact:stdout': 'Artifact:Stdout',
          'artifact:stderr': 'Artifact:Stderr',
        }
        const aggLabels: Record<string, string> = {
          '': '无',
          sum: '求和',
          avg: '平均值',
          count: '计数',
          min: '最小值',
          max: '最大值',
        }
        return (
          <Card title="数据处理规则" style={{ marginBottom: 24 }}>
            <Table
              dataSource={exprs.map((e, i) => ({ ...e, key: i }))}
              pagination={false}
              size="small"
              columns={[
                { title: '输出字段', dataIndex: 'field', key: 'field', render: (v: string) => <Text code>{v}</Text> },
                { title: '数据源', dataIndex: 'source', key: 'source', render: (v: string) => <Tag>{sourceLabels[v] || v}</Tag> },
                { title: 'JQ 表达式', dataIndex: 'jq_expr', key: 'jq_expr', render: (v: string) => <Text code>{v}</Text> },
                { title: '聚合', dataIndex: 'agg_mode', key: 'agg_mode', render: (v: string) => <Tag>{aggLabels[v] || v}</Tag> },
              ]}
            />
          </Card>
        )
      })()}

      <Card title={`耗时趋势 (${runs.length})`} style={{ marginBottom: 24 }}>
        <div style={{ marginBottom: 12 }}>
          <Select
            value={timeRange}
            onChange={setTimeRange}
            options={TIME_RANGE_OPTIONS}
            style={{ width: 140 }}
            size="small"
          />
        </div>
        <DurationChart runs={runs} />
      </Card>

      <Card title={`运行历史 (${runs.length})`} style={{ marginBottom: 24 }}>
        <Table
          columns={runColumns}
          dataSource={runs}
          rowKey="run_id"
          pagination={{ pageSize: 10 }}
          expandable={{
            expandedRowRender,
            rowExpandable: (record: RunRecord) =>
              !!(record.summary || record.stdout || record.stderr),
          }}
          size="small"
        />
      </Card>

      <Collapse
        items={[
          {
            key: 'ai',
            label: `AI 分析 (${analyses.length})`,
            children:
              analyses.length === 0 ? (
                <Text type="secondary">暂无AI分析</Text>
              ) : (
                <div>
                  {analyses.map((a) => {
                    const isExpanded = expandedAnalyses.has(a.id)
                    return (
                      <Card
                        key={a.id}
                        size="small"
                        style={{ marginBottom: 12 }}
                        title={
                          <Space wrap>
                            <Tag color="purple">{a.analysis_type}</Tag>
                            <Text type="secondary">模型: {a.model}</Text>
                            <Badge
                              status={
                                a.status === 'success' ? 'success' : 'error'
                              }
                              text={a.status === 'success' ? '成功' : '失败'}
                            />
                          </Space>
                        }
                        extra={
                          <Space>
                            <Text type="secondary">
                              Token: {a.tokens_in} 输入 / {a.tokens_out} 输出
                            </Text>
                            <Text type="secondary">
                              {formatTime(a.created_at)}
                            </Text>
                          </Space>
                        }
                      >
                        <div style={{ marginBottom: 8 }}>
                          <Text type="secondary">
                            耗时: {formatDuration(a.duration_ms)}
                          </Text>
                        </div>
                        <div>
                          <a
                            onClick={() => toggleAnalysisExpand(a.id)}
                            style={{ cursor: 'pointer', userSelect: 'none' }}
                          >
                            {isExpanded ? '隐藏' : '显示'}提示词和回复
                          </a>
                          {isExpanded && (
                            <div style={{ marginTop: 8 }}>
                              <div style={{ marginBottom: 8 }}>
                                <Text strong>提示词</Text>
                                <pre
                                  style={{
                                    background: '#f5f5f5',
                                    padding: 12,
                                    borderRadius: 6,
                                    maxHeight: 250,
                                    overflow: 'auto',
                                    fontSize: 12,
                                    marginTop: 4,
                                    whiteSpace: 'pre-wrap',
                                    wordBreak: 'break-word',
                                  }}
                                >
                                  {a.prompt.length > CHAR_LIMIT
                                    ? a.prompt.substring(0, CHAR_LIMIT) + '...'
                                    : a.prompt}
                                </pre>
                              </div>
                              <div>
                                <Text strong>回复</Text>
                                <pre
                                  style={{
                                    background: '#f5f5f5',
                                    padding: 12,
                                    borderRadius: 6,
                                    maxHeight: 250,
                                    overflow: 'auto',
                                    fontSize: 12,
                                    marginTop: 4,
                                    whiteSpace: 'pre-wrap',
                                    wordBreak: 'break-word',
                                  }}
                                >
                                  {a.response.length > CHAR_LIMIT
                                    ? a.response.substring(0, CHAR_LIMIT) +
                                      '...'
                                    : a.response}
                                </pre>
                              </div>
                            </div>
                          )}
                        </div>
                      </Card>
                    )
                  })}
                </div>
              ),
          },
        ]}
        defaultActiveKey={analyses.length > 0 ? ['ai'] : []}
        style={{ marginBottom: 24 }}
      />
    </div>
  )
}
