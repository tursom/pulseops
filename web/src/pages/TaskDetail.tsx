import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
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
  message,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { PlayCircleOutlined, ArrowLeftOutlined } from '@ant-design/icons'
import {
  fetchTask,
  fetchTaskRuns,
  fetchTaskAIAnalyses,
  triggerTaskRun,
  enableTask,
  disableTask,
} from '../api/client'
import type { TaskState, RunRecord, AIAnalysisRecord } from '../api/types'

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

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>()

  const [task, setTask] = useState<TaskState | null>(null)
  const [runs, setRuns] = useState<RunRecord[]>([])
  const [analyses, setAnalyses] = useState<AIAnalysisRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [expandedAnalyses, setExpandedAnalyses] = useState<Set<number>>(new Set())

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchData = useCallback(async () => {
    if (!id) return
    try {
      const [t, r, a] = await Promise.all([
        fetchTask(id),
        fetchTaskRuns(id, 20),
        fetchTaskAIAnalyses(id, 10),
      ])
      setTask(t)
      setRuns(r)
      setAnalyses(a)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load task')
    } finally {
      setLoading(false)
    }
  }, [id])

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
      message.success('Task triggered')
      await fetchData()
    } catch (err) {
      message.error(err instanceof Error ? err.message : 'Failed to trigger task')
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
        message.success('Task enabled')
      } else {
        await disableTask(id)
        message.success('Task disabled')
      }
      await fetchData()
    } catch (err) {
      message.error(err instanceof Error ? err.message : 'Failed to toggle task')
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
    return <Badge status={map[status] || 'default'} text={status} />
  }

  const checkStatusBadge = (status: string) => {
    const map: Record<string, 'success' | 'error'> = {
      pass: 'success',
      fail: 'error',
    }
    return <Badge status={map[status] || 'default'} text={status} />
  }

  const triggerTypeTag = (tt: string) => {
    const colors: Record<string, string> = {
      scheduled: 'blue',
      manual: 'green',
      rerun: 'orange',
      dependent: 'purple',
    }
    return <Tag color={colors[tt] || 'default'}>{tt}</Tag>
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
        message="Failed to load task"
        description={error}
        showIcon
      />
    )
  }

  if (!task) {
    return <Alert type="warning" message="Task not found" showIcon />
  }

  const runColumns: TableColumnsType<RunRecord> = [
    {
      title: 'Run ID',
      dataIndex: 'run_id',
      key: 'run_id',
      render: (v: string) => <Text code>{shortRunID(v)}</Text>,
    },
    {
      title: 'Trigger',
      dataIndex: 'trigger_type',
      key: 'trigger_type',
      render: (v: string) => triggerTypeTag(v),
    },
    {
      title: 'Status',
      dataIndex: 'run_status',
      key: 'run_status',
      render: (v: string) => runStatusBadge(v),
    },
    {
      title: 'Check',
      dataIndex: 'check_status',
      key: 'check_status',
      render: (v: string) => checkStatusBadge(v),
    },
    {
      title: 'Started',
      dataIndex: 'started_at',
      key: 'started_at',
      render: (v: string) => formatTime(v),
      sorter: (a, b) =>
        new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
      defaultSortOrder: 'descend',
    },
    {
      title: 'Duration',
      dataIndex: 'duration_ms',
      key: 'duration_ms',
      render: (v: number) => formatDuration(v),
    },
    {
      title: 'Error',
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
          <Text strong>Summary</Text>
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
          <Text strong>Stdout</Text>
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
          <Text strong>Stderr</Text>
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
        <Text type="secondary">No additional details</Text>
      )}
    </div>
  )

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <Link to="/" style={{ display: 'inline-block', marginBottom: 12 }}>
          <Space>
            <ArrowLeftOutlined />
            <span>Back to Dashboard</span>
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
            <span>Enabled</span>
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
              Run Now
            </Button>
          </Space>
        </div>
      </div>

      <Card title="Task Information" style={{ marginBottom: 24 }}>
        <Descriptions bordered column={{ xs: 1, sm: 2, lg: 3 }}>
          <Descriptions.Item label="Task ID">
            <Text code>{task.task_id}</Text>
          </Descriptions.Item>
          <Descriptions.Item label="Kind">
            <Tag>{task.kind}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="Status">
            {taskStatusBadge(task.status)}
          </Descriptions.Item>
          <Descriptions.Item label="Enabled">
            <Tag color={task.enabled ? 'green' : 'red'}>
              {task.enabled ? 'Yes' : 'No'}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="Last Run At">
            {formatTime(task.last_run_at)}
          </Descriptions.Item>
          <Descriptions.Item label="Next Run At">
            {formatTime(task.next_run_at)}
          </Descriptions.Item>
          <Descriptions.Item label="Last Status">
            {runStatusBadge(task.last_run_status)}
          </Descriptions.Item>
          <Descriptions.Item label="Last Error">
            {task.last_error ? (
              <Text type="danger" ellipsis style={{ maxWidth: 300 }}>
                {task.last_error}
              </Text>
            ) : (
              <Text type="secondary">—</Text>
            )}
          </Descriptions.Item>
          <Descriptions.Item label="Last Duration">
            {formatDuration(task.last_duration_ms)}
          </Descriptions.Item>
          <Descriptions.Item label="Source Path">
            <Text code>{task.source_path}</Text>
          </Descriptions.Item>
          <Descriptions.Item label="Updated At">
            {formatTime(task.updated_at)}
          </Descriptions.Item>
          <Descriptions.Item label="Labels">
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

      <Card title={`Run History (${runs.length})`} style={{ marginBottom: 24 }}>
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
            label: `AI Analyses (${analyses.length})`,
            children:
              analyses.length === 0 ? (
                <Text type="secondary">No AI analyses available</Text>
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
                            <Text type="secondary">Model: {a.model}</Text>
                            <Badge
                              status={
                                a.status === 'success' ? 'success' : 'error'
                              }
                              text={a.status}
                            />
                          </Space>
                        }
                        extra={
                          <Space>
                            <Text type="secondary">
                              Tokens: {a.tokens_in} in / {a.tokens_out} out
                            </Text>
                            <Text type="secondary">
                              {formatTime(a.created_at)}
                            </Text>
                          </Space>
                        }
                      >
                        <div style={{ marginBottom: 8 }}>
                          <Text type="secondary">
                            Duration: {formatDuration(a.duration_ms)}
                          </Text>
                        </div>
                        <div>
                          <a
                            onClick={() => toggleAnalysisExpand(a.id)}
                            style={{ cursor: 'pointer', userSelect: 'none' }}
                          >
                            {isExpanded ? 'Hide' : 'Show'} prompt &amp; response
                          </a>
                          {isExpanded && (
                            <div style={{ marginTop: 8 }}>
                              <div style={{ marginBottom: 8 }}>
                                <Text strong>Prompt</Text>
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
                                <Text strong>Response</Text>
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
