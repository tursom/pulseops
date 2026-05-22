import { useState, useCallback } from 'react'
import { Typography, Button, Space } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import PipelineCanvas from '../components/pipeline/PipelineCanvas'

const { Title, Text } = Typography

export default function PipelineEditor() {
  const navigate = useNavigate()
  const [refreshKey, setRefreshKey] = useState(0)

  const handleRefresh = useCallback(() => {
    setRefreshKey(k => k + 1)
  }, [])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <Title level={3} style={{ margin: 0 }}>
              Pipeline Editor
            </Title>
            <Text type="secondary">
              Visual task orchestration — click a node to edit its definition.
            </Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={handleRefresh}>
              Refresh
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/task-defs/new')}>
              Create Task
            </Button>
          </Space>
        </div>
      </div>
      <div style={{ flex: 1, minHeight: 0, border: '1px solid #e8e8e8', borderRadius: 8, overflow: 'hidden' }}>
        <PipelineCanvas key={refreshKey} />
      </div>
    </div>
  )
}
