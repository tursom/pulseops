import { useState, useEffect, useCallback } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { Typography, Spin, Alert, Space, message } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'
import {
  fetchTaskDefinition,
  createTaskDefinition,
  updateTaskDefinition,
} from '../api/client'
import type { TaskDefinition } from '../api/types'
import TaskForm from '../components/task-form/TaskForm'

const { Title } = Typography

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
  const isEdit = Boolean(id)

  const [initialValues, setInitialValues] = useState<
    Record<string, unknown> | undefined
  >(undefined)
  const [pageTitle, setPageTitle] = useState(
    isEdit ? 'Edit Task' : 'Create Task',
  )
  const [loading, setLoading] = useState(isEdit)
  const [error, setError] = useState<string | null>(null)

  const loadTaskDef = useCallback(async () => {
    if (!id) return
    try {
      setLoading(true)
      const def = await fetchTaskDefinition(id)
      setInitialValues(prepareInitialValues(def))
      setPageTitle(`Edit Task: ${def.name}`)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load task')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    if (isEdit) {
      loadTaskDef()
    } else {
      setInitialValues({ enabled: true, trigger: 'scheduled' })
      setLoading(false)
    }
  }, [isEdit, loadTaskDef])

  const handleSubmit = async (def: TaskDefinition) => {
    try {
      if (isEdit && id) {
        await updateTaskDefinition(id, def)
        message.success('Task updated')
      } else {
        await createTaskDefinition(def)
        message.success('Task created')
      }
      navigate('/pipeline')
    } catch (err) {
      message.error(err instanceof Error ? err.message : 'Operation failed')
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
        <Link
          to="/pipeline"
          style={{ display: 'inline-block', marginBottom: 16 }}
        >
          <Space>
            <ArrowLeftOutlined />
            <span>Back to Pipeline</span>
          </Space>
        </Link>
        <Alert
          type="error"
          message="Failed to load task"
          description={error}
          showIcon
        />
      </div>
    )
  }

  return (
    <div>
      <Link to="/pipeline" style={{ display: 'inline-block', marginBottom: 16 }}>
        <Space>
          <ArrowLeftOutlined />
          <span>Back to Pipeline</span>
        </Space>
      </Link>

      <Title level={2} style={{ marginBottom: 24 }}>
        {pageTitle}
      </Title>

      {initialValues && (
        <TaskForm
          initialValues={initialValues}
          mode={isEdit ? 'edit' : 'create'}
          onSubmit={handleSubmit}
        />
      )}
    </div>
  )
}
