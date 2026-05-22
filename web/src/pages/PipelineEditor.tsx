import { Typography } from 'antd'
import PipelineCanvas from '../components/pipeline/PipelineCanvas'

const { Title, Text } = Typography

export default function PipelineEditor() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div style={{ marginBottom: 16 }}>
        <Title level={3} style={{ margin: 0 }}>
          Pipeline Editor
        </Title>
        <Text type="secondary">
          Visual task orchestration — click a node to edit its definition.
        </Text>
      </div>
      <div style={{ flex: 1, minHeight: 0, border: '1px solid #e8e8e8', borderRadius: 8, overflow: 'hidden' }}>
        <PipelineCanvas />
      </div>
    </div>
  )
}
