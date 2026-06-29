import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  Alert,
  Breadcrumb,
  Button,
  Card,
  Descriptions,
  Empty,
  Input,
  message,
  Modal,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  ArrowLeftOutlined,
  CopyOutlined,
  DownloadOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  RobotOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import {
  fetchArtifactContent,
  fetchArtifactDetail,
  fetchRunAIAnalysis,
  fetchRunArtifacts,
  fetchTaskRun,
  retriggerTaskRun,
} from '../api/client'
import type {
  AIAnalysisRecord,
  ArtifactRef,
  Finding,
  RunRecord,
} from '../api/types'
import {
  checkStatusColor,
  CHECK_STATUS_LABELS,
  firstLine,
  formatBytes,
  formatDuration,
  formatTime,
  runDiagnosis,
  runStatusColor,
  RUN_STATUS_LABELS,
  safeJson,
  shortID,
} from '../utils/pulseops'

const { Title, Text, Paragraph } = Typography
const { Search } = Input

function copyText(value: string, label = '内容') {
  if (!navigator.clipboard) {
    message.error('当前浏览器不支持直接复制')
    return
  }
  navigator.clipboard
    .writeText(value)
    .then(() => message.success(`${label}已复制`))
    .catch(() => message.error('复制失败'))
}

function downloadText(filename: string, value: string) {
  const blob = new Blob([value], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}

function DataBlock({ title, value }: { title: string; value: unknown }) {
  const [query, setQuery] = useState('')
  const text = safeJson(value)
  const visible = query ? text.split('\n').filter((line) => line.toLowerCase().includes(query.toLowerCase())).join('\n') : text

  return (
    <Card
      className="ops-card"
      title={title}
      extra={
        <Space>
          <Search
            allowClear
            size="small"
            placeholder="搜索"
            onSearch={setQuery}
            onChange={(event) => setQuery(event.target.value)}
            style={{ width: 180 }}
          />
          <Button size="small" icon={<CopyOutlined />} onClick={() => copyText(text, title)}>
            复制
          </Button>
          <Button size="small" icon={<DownloadOutlined />} onClick={() => downloadText(`${title}.txt`, text)}>
            下载
          </Button>
        </Space>
      }
    >
      {text ? <pre className="code-block">{visible || '没有匹配内容'}</pre> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`无${title}`} />}
    </Card>
  )
}

export default function RunDetail() {
  const { id, runId } = useParams<{ id: string; runId: string }>()
  const navigate = useNavigate()
  const [run, setRun] = useState<RunRecord | null>(null)
  const [artifacts, setArtifacts] = useState<ArtifactRef[]>([])
  const [aiAnalysis, setAiAnalysis] = useState<AIAnalysisRecord | null>(null)
  const [aiFetched, setAiFetched] = useState(false)
  const [aiLoading, setAiLoading] = useState(false)
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState('diagnosis')
  const [previewArtifact, setPreviewArtifact] = useState<{ title: string; content: string } | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)

  const fetchData = useCallback(async () => {
    if (!id || !runId) return
    try {
      const [record, refs] = await Promise.all([fetchTaskRun(id, runId), fetchRunArtifacts(id, runId)])
      setRun(record)
      setArtifacts(refs)
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

  const loadAI = useCallback(() => {
    if (!id || !runId || aiFetched) return
    setAiLoading(true)
    fetchRunAIAnalysis(id, runId)
      .then(setAiAnalysis)
      .catch(() => setAiAnalysis(null))
      .finally(() => {
        setAiFetched(true)
        setAiLoading(false)
      })
  }, [aiFetched, id, runId])

  useEffect(() => {
    if (activeTab === 'ai') loadAI()
  }, [activeTab, loadAI])

  useEffect(() => {
    if (run?.run_status === 'failed' || run?.check_status === 'fail') loadAI()
  }, [loadAI, run])

  const handleRerun = async () => {
    if (!id || !runId) return
    setActionLoading(true)
    try {
      const next = await retriggerTaskRun(id, runId)
      message.success('已触发重跑')
      navigate(`/tasks/${encodeURIComponent(id)}/runs/${encodeURIComponent(next.run_id)}`)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '重跑失败')
    } finally {
      setActionLoading(false)
    }
  }

  const handleDownload = async (artifactID: string) => {
    try {
      const detail = await fetchArtifactDetail(artifactID)
      if (detail.download_url) window.open(detail.download_url, '_blank')
      else message.info('后端未返回下载链接')
    } catch (err) {
      message.error(err instanceof Error ? err.message : '获取下载链接失败')
    }
  }

  const handlePreview = async (artifact: ArtifactRef) => {
    setPreviewLoading(true)
    try {
      let raw = artifact.preview_text
      if (!raw) raw = await fetchArtifactContent(artifact.artifact_id)
      setPreviewArtifact({
        title: artifact.uri.split('/').pop() || artifact.artifact_id,
        content: raw,
      })
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载产物预览失败')
    } finally {
      setPreviewLoading(false)
    }
  }

  const findingColumns: TableColumnsType<Finding> = [
    {
      title: '级别',
      key: 'level',
      width: 90,
      render: (_, finding) => {
        const level = (finding.data?.level as string) || finding.reason || 'info'
        const color = level === 'error' ? 'red' : level === 'warn' ? 'orange' : 'blue'
        return <Tag color={color}>{level}</Tag>
      },
    },
    {
      title: '规则 ID',
      dataIndex: 'sample_id',
      width: 160,
      render: (value: string) => <Text code>{value || '—'}</Text>,
    },
    {
      title: '消息',
      dataIndex: 'reason',
      render: (value: string) => value || '—',
    },
    {
      title: '详情',
      key: 'detail',
      render: (_, finding) => (
        <Tooltip title={safeJson(finding.data)}>
          <Text ellipsis style={{ maxWidth: 280 }}>{safeJson(finding.data)}</Text>
        </Tooltip>
      ),
    },
  ]

  const artifactColumns: TableColumnsType<ArtifactRef> = [
    {
      title: '产物',
      dataIndex: 'uri',
      render: (value: string) => <Text code ellipsis>{value.split('/').pop() || value}</Text>,
    },
    {
      title: '类型',
      dataIndex: 'kind',
      width: 110,
      render: (value: string) => <Tag>{value || 'artifact'}</Tag>,
    },
    {
      title: '大小',
      dataIndex: 'size_bytes',
      width: 100,
      render: (value: number) => formatBytes(value),
    },
    {
      title: '内容类型',
      dataIndex: 'content_type',
      width: 170,
      render: (value: string) => value ? <Tag>{value}</Tag> : <Text type="secondary">—</Text>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 170,
      render: (_, artifact) => (
        <Space>
          <Button size="small" icon={<EyeOutlined />} loading={previewLoading} onClick={() => handlePreview(artifact)}>
            预览
          </Button>
          <Button size="small" icon={<DownloadOutlined />} onClick={() => handleDownload(artifact.artifact_id)}>
            下载
          </Button>
        </Space>
      ),
    },
  ]

  const diagnosisCards = useMemo(() => {
    if (!run) return []
    return [
      {
        key: 'error',
        title: '错误',
        content: run.error_message || (run.run_status === 'success' ? '无运行错误' : '未返回错误信息'),
        type: run.error_message ? 'error' : 'success',
      },
      {
        key: 'findings',
        title: '检查发现',
        content: run.findings?.length ? `${run.findings.length} 条 finding，优先查看下方明细。` : '无检查发现',
        type: run.findings?.length ? 'warning' : 'success',
      },
      {
        key: 'ai',
        title: 'AI 结论',
        content: aiLoading ? 'AI 分析加载中' : aiAnalysis?.response ? firstLine(aiAnalysis.response, 220) : '暂无 AI 分析',
        type: aiAnalysis?.status === 'failed' ? 'error' : aiAnalysis?.response ? 'info' : 'warning',
      },
    ] as const
  }, [aiAnalysis, aiLoading, run])

  if (loading) {
    return (
      <div className="page-shell" style={{ display: 'grid', placeItems: 'center', minHeight: '60vh' }}>
        <Spin size="large" />
      </div>
    )
  }

  if (error || !run) {
    return (
      <div className="page-shell">
        <Alert
          type="error"
          message="加载运行详情失败"
          description={error || '运行记录未找到'}
          action={<Button onClick={() => { setLoading(true); fetchData() }}>重试</Button>}
          showIcon
        />
      </div>
    )
  }

  const tabItems = [
    {
      key: 'diagnosis',
      label: '诊断',
      children: (
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>
            {diagnosisCards.map((item) => (
              <Alert
                key={item.key}
                type={item.type}
                message={item.title}
                description={item.content}
                showIcon
              />
            ))}
          </div>
          <Card className="ops-card" title="结构化摘要">
            {run.summary ? <pre className="code-block">{safeJson(run.summary)}</pre> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无 summary" />}
          </Card>
          <Card className="ops-card" title="插件配置追踪">
            <Descriptions column={{ xs: 1, md: 3 }} size="small">
              <Descriptions.Item label="plugin_config_versions">
                <pre className="code-block">{safeJson(run.plugin_config_versions || {})}</pre>
              </Descriptions.Item>
              <Descriptions.Item label="plugin_asset_versions">
                <pre className="code-block">{safeJson(run.plugin_asset_versions || {})}</pre>
              </Descriptions.Item>
              <Descriptions.Item label="plugin_task_overrides">
                <pre className="code-block">{safeJson(run.plugin_task_overrides || {})}</pre>
              </Descriptions.Item>
            </Descriptions>
          </Card>
          <Card className="ops-card" title={`Findings (${run.findings?.length || 0})`}>
            {run.findings?.length ? (
              <Table<Finding> className="dense-table" columns={findingColumns} dataSource={run.findings} rowKey="finding_id" size="small" pagination={false} />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无检查发现" />
            )}
          </Card>
        </Space>
      ),
    },
    {
      key: 'raw',
      label: '原始数据',
      children: (
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <DataBlock title="payload" value={run.payload} />
          <DataBlock title="stdout" value={run.stdout} />
          <DataBlock title="stderr" value={run.stderr} />
        </Space>
      ),
    },
    {
      key: 'artifacts',
      label: `产物 (${artifacts.length})`,
      children: (
        <Card className="ops-card">
          {artifacts.length ? (
            <Table<ArtifactRef> className="dense-table" columns={artifactColumns} dataSource={artifacts} rowKey="artifact_id" size="small" pagination={false} />
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无产物" />
          )}
        </Card>
      ),
    },
    {
      key: 'ai',
      label: (
        <Space size={5}>
          <RobotOutlined />
          <span>AI 分析</span>
        </Space>
      ),
      children: aiLoading ? (
        <Spin size="large" style={{ display: 'block', margin: '48px auto' }} />
      ) : aiAnalysis ? (
        <Card className="ops-card">
          <Space direction="vertical" size={14} style={{ width: '100%' }}>
            <Space wrap>
              <Tag color="purple">{aiAnalysis.analysis_type}</Tag>
              <Tag color={aiAnalysis.status === 'success' ? 'green' : 'red'}>{aiAnalysis.status === 'success' ? '成功' : '失败'}</Tag>
              <Text type="secondary">模型：{aiAnalysis.model}</Text>
              <Text type="secondary">Token：{aiAnalysis.tokens_in + aiAnalysis.tokens_out}</Text>
              <Text type="secondary">耗时：{formatDuration(aiAnalysis.duration_ms)}</Text>
            </Space>
            {aiAnalysis.error_message && <Alert type="error" message={aiAnalysis.error_message} showIcon />}
            <Card size="small" title="结论">
              <Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 0 }}>
                {aiAnalysis.response || '无回复'}
              </Paragraph>
            </Card>
            <Card size="small" title="提示词">
              <pre className="code-block">{aiAnalysis.prompt}</pre>
            </Card>
          </Space>
        </Card>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 AI 分析" />
      ),
    },
  ]

  return (
    <div className="page-shell">
      <div className="page-header">
        <div>
          <Breadcrumb
            items={[
              { title: <Link to="/tasks">任务监控</Link> },
              { title: <Link to={`/tasks/${id}`}>任务详情</Link> },
              { title: shortID(run.run_id) },
            ]}
            style={{ marginBottom: 8 }}
          />
          <Space align="center" size={10} wrap>
            <Title level={2} className="page-title">运行详情 {shortID(run.run_id)}</Title>
            <Tag color={runStatusColor(run.run_status)}>{RUN_STATUS_LABELS[run.run_status] || run.run_status}</Tag>
            <Tag color={checkStatusColor(run.check_status)}>{CHECK_STATUS_LABELS[run.check_status] || run.check_status}</Tag>
          </Space>
          <Text className="page-subtitle">{runDiagnosis(run, aiAnalysis)}</Text>
        </div>
        <Space wrap>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(`/tasks/${id}`)}>
            返回任务
          </Button>
          <Button type="primary" icon={<PlayCircleOutlined />} loading={actionLoading} onClick={handleRerun}>
            重跑
          </Button>
          <Button icon={<SettingOutlined />} onClick={() => navigate(`/task-defs/${encodeURIComponent(id || '')}/edit?from=/tasks/${encodeURIComponent(id || '')}/runs/${encodeURIComponent(run.run_id)}`)}>
            打开配置
          </Button>
        </Space>
      </div>

      <Card className="ops-card" title="概述" style={{ marginBottom: 14 }}>
        <Descriptions column={{ xs: 1, md: 2, xl: 4 }} size="small">
          <Descriptions.Item label="运行 ID"><Text code>{run.run_id}</Text></Descriptions.Item>
          <Descriptions.Item label="任务 ID"><Text code>{run.task_id}</Text></Descriptions.Item>
          <Descriptions.Item label="任务类型"><Tag>{run.task_kind}</Tag></Descriptions.Item>
          <Descriptions.Item label="插件 Generation">
            {run.plugin_generation_id ? <Text code>{run.plugin_generation_id}</Text> : <Text type="secondary">—</Text>}
          </Descriptions.Item>
          <Descriptions.Item label="触发方式"><Tag>{run.trigger_type || '—'}</Tag></Descriptions.Item>
          <Descriptions.Item label="运行状态"><Tag color={runStatusColor(run.run_status)}>{RUN_STATUS_LABELS[run.run_status] || run.run_status}</Tag></Descriptions.Item>
          <Descriptions.Item label="检查状态"><Tag color={checkStatusColor(run.check_status)}>{CHECK_STATUS_LABELS[run.check_status] || run.check_status}</Tag></Descriptions.Item>
          <Descriptions.Item label="开始时间">{formatTime(run.started_at)}</Descriptions.Item>
          <Descriptions.Item label="结束时间">{formatTime(run.ended_at)}</Descriptions.Item>
          <Descriptions.Item label="耗时">{formatDuration(run.duration_ms)}</Descriptions.Item>
          <Descriptions.Item label="错误信息">
            {run.error_message ? <Text type="danger">{run.error_message}</Text> : <Text type="secondary">—</Text>}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Tabs activeKey={activeTab} onChange={setActiveTab} items={tabItems} />

      <Modal
        title={previewArtifact?.title || '产物预览'}
        open={Boolean(previewArtifact)}
        onCancel={() => setPreviewArtifact(null)}
        footer={null}
        width={920}
      >
        <DataBlock title={previewArtifact?.title || 'artifact'} value={previewArtifact?.content || ''} />
      </Modal>
    </div>
  )
}
