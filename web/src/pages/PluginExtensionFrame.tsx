import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Alert, Button, Card, Spin, Typography } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'
import type { PluginCapability } from '../api/types'
import { fetchPluginCapabilities } from '../api/client'

const { Title, Text } = Typography

export default function PluginExtensionFrame() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [capabilities, setCapabilities] = useState<PluginCapability[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const pluginID = params.get('plugin') || ''
  const capabilityName = params.get('capability') || ''

  useEffect(() => {
    fetchPluginCapabilities('ui_extension')
      .then((items) => {
        setCapabilities(items)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : '加载插件入口失败'))
      .finally(() => setLoading(false))
  }, [])

  const capability = useMemo(
    () => capabilities.find((item) => item.plugin_id === pluginID && item.name === capabilityName),
    [capabilities, capabilityName, pluginID],
  )

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
          <Title level={2} className="page-title">{capability?.title || capabilityName || '插件入口'}</Title>
          <Text className="page-subtitle">{capability?.plugin_name || pluginID}</Text>
        </div>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/plugins')}>返回插件中心</Button>
      </div>

      {(error || !capability?.path) ? (
        <Alert
          type="error"
          showIcon
          message="插件入口不可用"
          description={error || '未在当前 active generation 中找到该前端入口'}
        />
      ) : (
        <Card className="ops-card" bodyStyle={{ padding: 0, overflow: 'hidden' }}>
          <iframe
            title={capability.title || capability.name}
            src={capability.path}
            sandbox="allow-forms allow-popups allow-same-origin allow-scripts"
            style={{ display: 'block', width: '100%', height: 'calc(100vh - 190px)', minHeight: 560, border: 0 }}
          />
        </Card>
      )}
    </div>
  )
}
