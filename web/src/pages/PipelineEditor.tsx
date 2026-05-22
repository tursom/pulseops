import { useState, useEffect, useCallback } from 'react'
import { Typography, Button, Space, Spin, Breadcrumb } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import PipelineCanvas from '../components/pipeline/PipelineCanvas'
import { fetchPipeline } from '../api/client'
import type { Pipeline } from '../api/types'

const { Title, Text } = Typography

export default function PipelineEditor() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [pipeline, setPipeline] = useState<Pipeline | null>(null)
  const [pipelineLoading, setPipelineLoading] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    if (!id) return
    fetchPipeline(id)
      .then(setPipeline)
      .catch(() => {
        setPipeline({ id, name: id, description: '', created_at: '', updated_at: '' })
      })
      .finally(() => setPipelineLoading(false))
  }, [id])

  const handleRefresh = useCallback(() => {
    setRefreshKey(k => k + 1)
  }, [])

  if (pipelineLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '60vh' }}>
        <Spin />
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ marginBottom: 16 }}>
        <Breadcrumb
          items={[
            { title: <a onClick={() => navigate('/pipelines')}>返回流水线列表</a> },
            { title: pipeline?.name || id },
          ]}
          style={{ marginBottom: 8 }}
        />
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <Title level={3} style={{ margin: 0 }}>
              流水线: {pipeline?.name}
            </Title>
            {pipeline?.description && (
              <Text type="secondary">{pipeline.description}</Text>
            )}
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={handleRefresh}>
              刷新
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate(`/task-defs/new?pipeline=${id}`)}>
              创建任务
            </Button>
          </Space>
        </div>
      </div>
      <div style={{ flex: 1, minHeight: 0, border: '1px solid #e8e8e8', borderRadius: 8, overflow: 'hidden' }}>
        <PipelineCanvas key={refreshKey} pipelineId={id!} />
      </div>
    </div>
  )
}
