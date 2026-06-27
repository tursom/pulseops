import { useState, useEffect, useCallback } from 'react'
import { useParams, useNavigate, useSearchParams, Link } from 'react-router-dom'
import { Typography, Spin, Alert, Space, Select, Card, message } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'
import {
  fetchTaskDefinition,
  createTaskDefinition,
  updateTaskDefinition,
  fetchPipelines,
} from '../api/client'
import type { TaskDefinition, Pipeline } from '../api/types'
import TaskForm from '../components/task-form/TaskForm'
import {
  buildTopologyDependency,
  buildTopologyParams,
  buildTopologyTaskName,
  DEPENDENCY_CAPABLE_KINDS,
  isDependencyCapableKind,
} from '../utils/taskCreationDefaults'

const { Title } = Typography

function generateTaskId(): string {
  return `task-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

function recordToFormList(
  rec: Record<string, string> | undefined,
): { key: string; value: string }[] {
  if (!rec) return []
  return Object.entries(rec).map(([key, value]) => ({ key, value }))
}

function prepareInitialValues(
  def: TaskDefinition,
): Record<string, unknown> {
  const values: Record<string, unknown> = { ...def }

  values.labels = recordToFormList(def.labels)

  const params = values.params as Record<string, unknown> | undefined
  if (params) {
    if (params.headers && typeof params.headers === 'object') {
      params.headers = recordToFormList(params.headers as Record<string, string>)
    }
    if (params.env && typeof params.env === 'object') {
      params.env = recordToFormList(params.env as Record<string, string>)
    }
    if (params.prompt && typeof params.prompt === 'string') {
      params.prompt = { text: params.prompt }
    }
    if (Array.isArray(params.outputs)) {
      params.outputs = params.outputs.map((item) => {
        if (typeof item === 'string') {
          return { type: 'summary', config: { field: item } }
        }
        return item
      })
    }
    const source = params.source as Record<string, unknown> | undefined
    if (source?.headers && typeof source.headers === 'object') {
      source.headers = recordToFormList(source.headers as Record<string, string>)
    }
  }

  return values
}

export default function TaskEditor() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const pipelineId = searchParams.get('pipeline')
  const from = searchParams.get('from')
  const upstreamTaskId = searchParams.get('upstream_task_id')
  const upstreamName = searchParams.get('upstream_name')
  const initialKind = searchParams.get('kind')
  const returnUrl = from || (pipelineId ? `/pipelines/${pipelineId}` : '/tasks')
  const isEdit = Boolean(id)
  const isTopologySource = Boolean(returnUrl.startsWith('/pipelines'))
  const isTopologyCreate = !isEdit && isTopologySource
  const isTopologyDownstreamCreate = !isEdit && isTopologySource && Boolean(upstreamTaskId)

  const [initialValues, setInitialValues] = useState<
    Record<string, unknown> | undefined
  >(undefined)
  const getDefaultTitle = () => {
    if (isEdit) return '编辑任务'
    if (upstreamName) return `为 "${upstreamName}" 创建下游任务`
    return '创建任务'
  }
  const [pageTitle, setPageTitle] = useState(getDefaultTitle())
  const [loading, setLoading] = useState(isEdit)
  const [error, setError] = useState<string | null>(null)
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [selectedPipelineId, setSelectedPipelineId] = useState<string | undefined>(
    pipelineId || undefined,
  )

  const loadTaskDef = useCallback(async () => {
    if (!id) return
    try {
      setLoading(true)
      const def = await fetchTaskDefinition(id)
      setInitialValues(prepareInitialValues(def))
      setPageTitle(`编辑任务：${def.name}`)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    if (isEdit) {
      loadTaskDef()
    } else {
      const taskID = generateTaskId()
      const effectiveInitialKind = upstreamTaskId && !initialKind ? 'data_process' : initialKind
      const hasDependencyKind = isDependencyCapableKind(effectiveInitialKind)
      const hasDependencyDefault = Boolean(upstreamTaskId && hasDependencyKind)
      const initial: Record<string, unknown> = {
        task_id: taskID,
        enabled: true,
        trigger: hasDependencyKind ? 'on_run' : isTopologySource ? 'manual' : 'scheduled',
      }

      if (effectiveInitialKind) {
        initial.kind = effectiveInitialKind
      }
      if (upstreamTaskId && effectiveInitialKind && hasDependencyDefault) {
        initial.name = buildTopologyTaskName(effectiveInitialKind, { upstreamTaskId, upstreamName: upstreamName || undefined })
        initial.dependencies = [buildTopologyDependency(taskID, upstreamTaskId)]
        initial.params = buildTopologyParams(effectiveInitialKind, { upstreamTaskId, upstreamName: upstreamName || undefined })
        initial.labels = [{ key: 'source', value: 'topology' }]
      }
      if (pipelineId) {
        initial.pipeline_id = pipelineId
      }

      setInitialValues(initial)
      setLoading(false)
    }
  }, [isEdit, loadTaskDef, pipelineId, upstreamTaskId, upstreamName, initialKind, isTopologySource])

  useEffect(() => {
    fetchPipelines()
      .then(setPipelines)
      .catch(() => {})
  }, [])

  const handleSubmit = async (def: TaskDefinition) => {
    try {
      if (selectedPipelineId) {
        def.pipeline_id = selectedPipelineId
      } else if (!isEdit && def.pipeline_id === undefined) {
        def.pipeline_id = null
      }
      if (isEdit && id) {
        const saved = await updateTaskDefinition(id, def)
        message.success('任务已更新')
        if (isTopologySource) {
          navigate(returnUrl)
        } else {
          navigate(`/tasks/${encodeURIComponent(saved.task_id || id)}`)
        }
      } else {
        const saved = await createTaskDefinition(def)
        message.success('任务已创建')
        if (isTopologySource) {
          navigate(returnUrl)
        } else {
          navigate(`/tasks/${encodeURIComponent(saved.task_id || def.task_id)}`)
        }
      }
    } catch (err) {
      message.error(err instanceof Error ? err.message : '操作失败')
    }
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
      <div>
        <Link to={returnUrl} style={{ display: 'inline-block', marginBottom: 16 }}>
          <Space>
            <ArrowLeftOutlined />
            <span>返回</span>
          </Space>
        </Link>
        <Alert
          type="error"
          message="加载任务失败"
          description={error}
          showIcon
        />
      </div>
    )
  }

  return (
    <div>
      <Link to={returnUrl} style={{ display: 'inline-block', marginBottom: 16 }}>
        <Space>
          <ArrowLeftOutlined />
          <span>返回</span>
        </Space>
      </Link>

      <div className="page-header">
        <div>
          <Title level={2} className="page-title">{pageTitle}</Title>
          <span className="page-subtitle">
            {isTopologyDownstreamCreate
              ? '已根据依赖拓扑带入任务组、上游依赖和推荐配置。'
              : '默认使用向导填写；需要底层字段时展开高级 JSON 预览。'}
          </span>
        </div>
      </div>

      <Card className="ops-card" title="所属任务组" style={{ marginBottom: 16 }}>
        {isTopologySource && selectedPipelineId ? (
          <Alert
            type="info"
            showIcon
            message={pipelines.find((p) => p.id === selectedPipelineId)?.name || selectedPipelineId}
            description="从依赖拓扑进入时，任务会固定创建到当前任务组。"
          />
        ) : (
          <Select
            allowClear
            placeholder="选择所属任务组（可选）"
            value={selectedPipelineId}
            onChange={(val) => setSelectedPipelineId(val)}
            options={pipelines.map((p) => ({ value: p.id, label: p.name }))}
            style={{ width: '100%' }}
          />
        )}
      </Card>

      {initialValues && (
        <TaskForm
          initialValues={initialValues}
          mode={isEdit ? 'edit' : 'create'}
          creationContext={isTopologyCreate ? {
            source: 'topology',
            lockedPipelineId: pipelineId || undefined,
            lockedUpstreamTaskId: isTopologyDownstreamCreate ? upstreamTaskId || undefined : undefined,
            lockedUpstreamName: isTopologyDownstreamCreate ? upstreamName || undefined : undefined,
            recommendedKinds: isTopologyDownstreamCreate ? DEPENDENCY_CAPABLE_KINDS : undefined,
          } : undefined}
          onSubmit={handleSubmit}
        />
      )}
    </div>
  )
}
