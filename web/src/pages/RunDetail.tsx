import { useState, useEffect, useCallback } from 'react'
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
  Tabs,
  Empty,
  Space,
  Breadcrumb,
  Tooltip,
  Modal,
  message,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  ArrowLeftOutlined,
  DownloadOutlined,
  RobotOutlined,
  EyeOutlined,
} from '@ant-design/icons'
import {
  fetchTaskRun,
  fetchRunAIAnalysis,
  fetchRunArtifacts,
  fetchArtifactDetail,
  fetchArtifactContent,
} from '../api/client'
import type {
  RunRecord,
  AIAnalysisRecord,
  ArtifactRef,
  Finding,
} from '../api/types'

const { Title, Text, Paragraph } = Typography

function formatTime(t: string | null): string {
  if (!t) return '—'
  return new Date(t).toLocaleString()
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const s = ms / 1000
  return s < 60 ? `${s.toFixed(1)}s` : `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`
}

function shortRunID(id: string): string {
  return id.length > 8 ? id.slice(0, 12) + '\u2026' : id
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb.toFixed(1)} KB`
  const mb = kb / 1024
  return `${mb.toFixed(1)} MB`
}

const jsonKeyColor = '#881280'
const jsonStringColor = '#1a8000'
const jsonNumberColor = '#1750ac'
const jsonBooleanColor = '#b22452'
const jsonNullColor = '#808080'

function deepParseJSON(value: unknown): unknown {
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if ((trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'))) {
      try {
        return deepParseJSON(JSON.parse(trimmed))
      } catch {
        return value
      }
    }
    return value
  }
  if (Array.isArray(value)) {
    return value.map(deepParseJSON)
  }
  if (value && typeof value === 'object') {
    const result: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      result[k] = deepParseJSON(v)
    }
    return result
  }
  return value
}

function renderHighlightedJson(text: string): React.ReactNode {
  const lines = text.split('\n')
  return (
    <code style={{ fontFamily: 'ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, Consolas, monospace', fontSize: 13, lineHeight: '1.5', whiteSpace: 'pre' }}>
      {lines.map((line, i) => {
        const indent = line.match(/^(\s*)/)?.[0] || ''
        const content = line.slice(indent.length)
        const highlighted = content
          .replace(/(:\s*)("(?:[^"\\]|\\.)*")/g, (_m, prefix, str) => `${prefix}<span style="color:${jsonStringColor}">${str}</span>`)
          .replace(/("(?:[^"\\]|\\.)*")(\s*:)/g, (_m, key, suffix) => `<span style="color:${jsonKeyColor}">${key}</span>${suffix}`)
          .replace(/(?<!")(\b-?\d+\.?\d*(?:[eE][+-]?\d+)?\b)(?!\s*")/g, (_m, num) => {
            if (_m.includes('span')) return _m
            return `<span style="color:${jsonNumberColor}">${num}</span>`
          })
          .replace(/\b(true|false)\b/g, `<span style="color:${jsonBooleanColor}">$1</span>`)
          .replace(/\bnull\b/g, `<span style="color:${jsonNullColor}">null</span>`)
        return (
          <div key={i} style={{ display: 'flex' }}>
            <span style={{ color: '#aaa', userSelect: 'none', minWidth: '3em', textAlign: 'right', marginRight: '1em' }}>{i + 1}</span>
            <span>
              <span style={{ whiteSpace: 'pre' }}>{indent}</span>
              <span dangerouslySetInnerHTML={{ __html: highlighted }} />
            </span>
          </div>
        )
      })}
    </code>
  )
}

export default function RunDetail() {
  const { id, runId } = useParams<{ id: string; runId: string }>()

  const [run, setRun] = useState<RunRecord | null>(null)
  const [artifacts, setArtifacts] = useState<ArtifactRef[]>([])
  const [aiAnalysis, setAiAnalysis] = useState<AIAnalysisRecord | null>(null)
  const [aiLoading, setAiLoading] = useState(false)
  const [aiFetched, setAiFetched] = useState(false)
  const [activeTab, setActiveTab] = useState('overview')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [previewArtifact, setPreviewArtifact] = useState<{ title: string, content: string } | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)

  const fetchData = useCallback(async () => {
    if (!id || !runId) return
    try {
      const [r, arts] = await Promise.all([
        fetchTaskRun(id, runId),
        fetchRunArtifacts(id, runId),
      ])
      setRun(r)
      setArtifacts(arts)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载运行详情失败')
    } finally {
      setLoading(false)
    }
  }, [id, runId])

  useEffect(() => {
    setLoading(true)
    fetchData()
  }, [fetchData])

  const handleTabChange = (key: string) => {
    setActiveTab(key)
    if (key === 'ai' && !aiFetched && id && runId) {
      setAiLoading(true)
      fetchRunAIAnalysis(id, runId)
        .then((a) => setAiAnalysis(a))
        .catch(() => setAiAnalysis(null))
        .finally(() => {
          setAiLoading(false)
          setAiFetched(true)
        })
    }
  }

  const handleDownload = async (artifactID: string) => {
    try {
      const detail = await fetchArtifactDetail(artifactID)
      if (detail.download_url) {
        window.open(detail.download_url, '_blank')
      }
    } catch {
      // ignore download errors
    }
  }

  const handlePreview = async (artifactID: string) => {
    setPreviewLoading(true)
    try {
      const detail = await fetchArtifactDetail(artifactID)
      const artifactData = (detail as unknown as Record<string, unknown>).artifact as Record<string, unknown> | undefined
      let raw = (artifactData?.preview_text as string) || (detail as unknown as Record<string, unknown>).preview_text as string | undefined
      const uri = (artifactData?.uri as string) || (detail as unknown as Record<string, unknown>).uri as string || artifactID
      if (!raw) {
        message.info('该产物无可预览的文本内容')
        setPreviewLoading(false)
        return
      }
      const sizeBytes = (artifactData?.size_bytes as number) || (detail as unknown as Record<string, unknown>).size_bytes as number || 0
      const truncated = raw.length < sizeBytes && !(raw.trim().endsWith('}') || raw.trim().endsWith(']') || raw.trim().endsWith('"'))
      if (truncated) {
        try {
          raw = await fetchArtifactContent(artifactID)
        } catch {
          // fall back to truncated preview_text
        }
      }
      let formatted = raw
      try {
        const parsed = JSON.parse(raw)
        const deep = deepParseJSON(parsed)
        formatted = JSON.stringify(deep, null, 2)
      } catch {
        // not valid JSON, show raw text
      }
      setPreviewArtifact({
        title: uri.split('/').pop() || uri,
        content: formatted,
      })
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载产物内容失败')
    } finally {
      setPreviewLoading(false)
    }
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

  if (loading) {
    return <Spin size="large" style={{ display: 'block', margin: '48px auto' }} />
  }

  if (error) {
    return <Alert type="error" message="加载运行详情失败" description={error} style={{ margin: 24 }} />
  }

  if (!run) {
    return <Alert type="warning" message="运行记录未找到" style={{ margin: 24 }} />
  }

  const findingLevelTag = (finding: Finding) => {
    const level = (finding.data?.level as string) || finding.reason
    const colors: Record<string, string> = { error: 'red', warn: 'orange', info: 'blue' }
    return <Tag color={colors[level] || 'default'}>{level}</Tag>
  }

  const findingColumns: TableColumnsType<Finding> = [
    {
      title: '级别',
      key: 'level',
      render: (_: unknown, record: Finding) => findingLevelTag(record),
    },
    {
      title: '规则ID',
      dataIndex: 'sample_id',
      key: 'sample_id',
      render: (v: string) => <Text code>{v}</Text>,
    },
    {
      title: '消息',
      dataIndex: 'reason',
      key: 'reason',
      ellipsis: true,
    },
    {
      title: '详情',
      key: 'detail',
      render: (_: unknown, record: Finding) => (
        <Tooltip title={JSON.stringify(record.data, null, 2)}>
          <Text style={{ maxWidth: 200 }} ellipsis>
            {JSON.stringify(record.data)}
          </Text>
        </Tooltip>
      ),
    },
  ]

  const artifactColumns: TableColumnsType<ArtifactRef> = [
    {
      title: '文件名',
      dataIndex: 'uri',
      key: 'uri',
      render: (v: string) => (
        <Text code ellipsis style={{ maxWidth: 280 }}>
          {v.split('/').pop() || v}
        </Text>
      ),
    },
    {
      title: '类型',
      dataIndex: 'content_type',
      key: 'content_type',
      render: (v: string) => <Tag>{v}</Tag>,
    },
    {
      title: '大小',
      dataIndex: 'size_bytes',
      key: 'size_bytes',
      render: (v: number) => formatBytes(v),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: ArtifactRef) => (
        <Space>
          <Button
            type="link"
            icon={<DownloadOutlined />}
            onClick={() => handleDownload(record.artifact_id)}
          >
            下载
          </Button>
          {record.content_type && record.content_type.includes('json') && (
            <Button
              type="link"
              icon={<EyeOutlined />}
              loading={previewLoading}
              onClick={() => handlePreview(record.artifact_id)}
            >
              预览
            </Button>
          )}
        </Space>
      ),
    },
  ]

  const overviewTab = (
    <Card title="运行概述" style={{ marginBottom: 24 }}>
      <Descriptions bordered column={{ xs: 1, sm: 2 }}>
        <Descriptions.Item label="运行ID">
          <Text code>{run.run_id}</Text>
        </Descriptions.Item>
        <Descriptions.Item label="任务ID">
          <Text code>{run.task_id}</Text>
        </Descriptions.Item>
        <Descriptions.Item label="任务类型">
          <Tag>{run.task_kind}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="触发类型">
          {triggerTypeTag(run.trigger_type)}
        </Descriptions.Item>
        <Descriptions.Item label="运行状态">
          {runStatusBadge(run.run_status)}
        </Descriptions.Item>
        <Descriptions.Item label="检查状态">
          {checkStatusBadge(run.check_status)}
        </Descriptions.Item>
        <Descriptions.Item label="开始时间">
          {formatTime(run.started_at)}
        </Descriptions.Item>
        <Descriptions.Item label="结束时间">
          {run.ended_at ? formatTime(run.ended_at) : <Text type="secondary">—</Text>}
        </Descriptions.Item>
        <Descriptions.Item label="耗时">
          {formatDuration(run.duration_ms)}
        </Descriptions.Item>
        <Descriptions.Item label="错误信息">
          {run.error_message ? (
            <Text type="danger">{run.error_message}</Text>
          ) : (
            <Text type="secondary">—</Text>
          )}
        </Descriptions.Item>
      </Descriptions>
    </Card>
  )

  const tabItems = [
    {
      key: 'overview',
      label: '运行概述',
      children: overviewTab,
    },
    {
      key: 'stdout',
      label: '标准输出',
      children: (
        <Card>
          {run.stdout ? (
            <pre style={{ maxHeight: 400, overflow: 'auto', fontSize: 12 }}>{run.stdout}</pre>
          ) : (
            <Empty description="（无输出）" />
          )}
        </Card>
      ),
    },
    {
      key: 'stderr',
      label: '标准错误',
      children: (
        <Card>
          {run.stderr ? (
            <pre style={{ maxHeight: 400, overflow: 'auto', fontSize: 12, color: '#cf1322' }}>{run.stderr}</pre>
          ) : (
            <Empty description="（无错误输出）" />
          )}
        </Card>
      ),
    },
    {
      key: 'summary',
      label: '摘要',
      children: (
        <Card>
          {run.summary ? (
            <pre style={{ maxHeight: 400, overflow: 'auto', fontSize: 12 }}>
              {JSON.stringify(run.summary, null, 2)}
            </pre>
          ) : (
            <Empty description={<Text type="secondary">（无摘要）</Text>} />
          )}
        </Card>
      ),
    },
    ...(run.task_kind === 'data_process' && run.summary ? [{
      key: 'extracted',
      label: '提取数据',
      children: (
        <Card>
          <Table
            dataSource={Object.entries(run.summary).map(([field, value]) => ({
              key: field,
              field,
              value: value === null || value === undefined ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value),
              type: typeof value === 'number' ? 'number' : typeof value === 'boolean' ? 'boolean' : Array.isArray(value) ? 'array' : typeof value === 'object' ? 'object' : 'string',
            }))}
            pagination={false}
            size="small"
            columns={[
              { title: '字段', dataIndex: 'field', key: 'field', render: (v: string) => <Text code>{v}</Text>, width: 160 },
              { title: '值', dataIndex: 'value', key: 'value', render: (v: string) => <Text style={{ fontFamily: 'monospace' }}>{v}</Text> },
              { title: '类型', dataIndex: 'type', key: 'type', render: (v: string) => <Tag>{v}</Tag>, width: 80 },
            ]}
          />
        </Card>
      ),
    }] : []),
    {
      key: 'payload',
      label: '数据载荷',
      children: (
        <Card>
          {run.payload ? (
            <pre style={{ maxHeight: 400, overflow: 'auto', fontSize: 12 }}>
              {JSON.stringify(run.payload, null, 2)}
            </pre>
          ) : (
            <Empty description="（无数据载荷）" />
          )}
        </Card>
      ),
    },
    {
      key: 'findings',
      label: `检查发现${run.findings ? ` (${run.findings.length})` : ''}`,
      children: (
        <Card>
          {run.findings && run.findings.length > 0 ? (
            <Table
              columns={findingColumns}
              dataSource={run.findings}
              rowKey="finding_id"
              pagination={false}
              size="small"
            />
          ) : (
            <Empty description="暂无检查发现" />
          )}
        </Card>
      ),
    },
    {
      key: 'artifacts',
      label: `产物 (${artifacts.length})`,
      children: (
        <Card>
          {artifacts.length > 0 ? (
            <Table
              columns={artifactColumns}
              dataSource={artifacts}
              rowKey="artifact_id"
              pagination={false}
              size="small"
            />
          ) : (
            <Empty description="暂无产物" />
          )}
        </Card>
      ),
    },
    {
      key: 'ai',
      label: (
        <Space size={4}>
          <RobotOutlined />
          <span>AI 分析</span>
        </Space>
      ),
      children: aiLoading ? (
        <Spin size="large" style={{ display: 'block', margin: '48px auto' }} />
      ) : aiAnalysis ? (
        <Card
          title={
            <Space wrap>
              <Tag color="purple">{aiAnalysis.analysis_type}</Tag>
              <Text type="secondary">模型: {aiAnalysis.model}</Text>
              <Badge
                status={aiAnalysis.status === 'success' ? 'success' : 'error'}
                text={aiAnalysis.status === 'success' ? '成功' : '失败'}
              />
            </Space>
          }
          extra={
            <Text type="secondary">{formatTime(aiAnalysis.created_at)}</Text>
          }
          style={{ marginBottom: 12 }}
        >
          <Descriptions bordered size="small" column={{ xs: 1, sm: 3 }} style={{ marginBottom: 16 }}>
            <Descriptions.Item label="提示词 Token">{aiAnalysis.tokens_in}</Descriptions.Item>
            <Descriptions.Item label="回复 Token">{aiAnalysis.tokens_out}</Descriptions.Item>
            <Descriptions.Item label="总计 Token">{aiAnalysis.tokens_in + aiAnalysis.tokens_out}</Descriptions.Item>
            <Descriptions.Item label="耗时">{formatDuration(aiAnalysis.duration_ms)}</Descriptions.Item>
          </Descriptions>
          {aiAnalysis.error_message && (
            <Alert type="error" message={aiAnalysis.error_message} style={{ marginBottom: 12 }} />
          )}
          <Card type="inner" title="提示词" size="small" style={{ marginBottom: 12 }}>
            <Paragraph
              ellipsis={{ rows: 4, expandable: true, symbol: '展开' }}
              style={{
                fontSize: 12,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                fontFamily: 'monospace',
                background: '#fafafa',
                padding: 8,
                borderRadius: 4,
                margin: 0,
              }}
            >
              {aiAnalysis.prompt}
            </Paragraph>
          </Card>
          <Card type="inner" title="回复" size="small">
            <Paragraph
              ellipsis={{ rows: 6, expandable: true, symbol: '展开' }}
              style={{
                fontSize: 12,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                fontFamily: 'monospace',
                background: '#f6ffed',
                padding: 8,
                borderRadius: 4,
                margin: 0,
              }}
            >
              {aiAnalysis.response}
            </Paragraph>
          </Card>
        </Card>
      ) : (
        <Empty description="暂无 AI 分析" />
      ),
    },
  ]

  return (
    <>
      <div>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Breadcrumb
          items={[
            { title: <Link to="/tasks">任务列表</Link> },
            { title: <Link to={`/tasks/${id}`}>{'任务详情'}</Link> },
            { title: shortRunID(runId! || run.run_id) },
          ]}
        />

        <Link to={`/tasks/${id}`}>
          <Space>
            <ArrowLeftOutlined />
            <span>返回</span>
          </Space>
        </Link>

        <Title level={3} style={{ margin: 0 }}>
          运行详情: {shortRunID(run.run_id)}
        </Title>
      </Space>

      <Tabs
        activeKey={activeTab}
        onChange={handleTabChange}
        items={tabItems}
        style={{ marginTop: 16 }}
      />
    </div>
    <Modal
      title={previewArtifact?.title || '预览'}
      open={!!previewArtifact}
      onCancel={() => setPreviewArtifact(null)}
      footer={null}
      width={800}
      styles={{ body: { padding: 0 } }}
    >
      <div style={{ maxHeight: 500, overflow: 'auto', background: '#f8f8f8', padding: 16, borderRadius: '0 0 8px 8px', fontSize: 13 }}>
        {previewArtifact?.content ? renderHighlightedJson(previewArtifact.content) : null}
      </div>
    </Modal>
  </>
  )
}
