import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined, SaveOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { GlobalSettings, SinkEntry } from '../api/types'
import { fetchSettings, updateSettings } from '../api/client'

const { Title, Text } = Typography

export default function Settings() {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()
  const [sinks, setSinks] = useState<SinkEntry[]>([])
  const [lastSavedAt, setLastSavedAt] = useState<Date | null>(null)

  useEffect(() => {
    fetchSettings()
      .then((settings) => {
        form.setFieldsValue({
          max_payload_bytes: settings.max_payload_bytes,
          default_retain_days: settings.default_retain_days,
        })
        setSinks(settings.sinks || [])
      })
      .catch((err) => message.error(err instanceof Error ? err.message : '加载设置失败'))
      .finally(() => setLoading(false))
  }, [form])

  const postgresCount = useMemo(() => sinks.filter((sink) => sink.kind === 'postgres').length, [sinks])
  const hasWebhook = sinks.some((sink) => sink.kind === 'webhook')

  const handleSave = async () => {
    try {
      setSaving(true)
      const values = await form.validateFields()
      if (postgresCount === 0) {
        message.error('至少保留一个 Postgres Sink')
        return
      }
      const settings: GlobalSettings = {
        max_payload_bytes: values.max_payload_bytes,
        default_retain_days: values.default_retain_days,
        sinks,
      }
      await updateSettings(settings)
      setLastSavedAt(new Date())
      message.success('设置已保存')
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error(err instanceof Error ? err.message : '保存设置失败')
    } finally {
      setSaving(false)
    }
  }

  const addSink = () => {
    setSinks([...sinks, { name: `sink_${sinks.length + 1}`, kind: 'postgres' }])
  }

  const updateSink = (index: number, field: keyof SinkEntry, value: unknown) => {
    const updated = [...sinks]
    updated[index] = { ...updated[index], [field]: value }
    if (field === 'kind' && value === 'postgres') {
      delete updated[index].url
      delete updated[index].timeout
    }
    setSinks(updated)
  }

  const deleteSink = (index: number) => {
    const target = sinks[index]
    if (target.kind === 'postgres' && postgresCount <= 1) {
      message.warning('至少保留一个 Postgres Sink')
      return
    }
    setSinks(sinks.filter((_, i) => i !== index))
  }

  const columns: ColumnsType<SinkEntry> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (_, record, index) => (
        <Input
          value={record.name}
          onChange={(event) => updateSink(index, 'name', event.target.value)}
          placeholder="Sink 名称"
        />
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      width: 150,
      render: (_, record, index) => (
        <Select
          value={record.kind}
          onChange={(value) => updateSink(index, 'kind', value)}
          style={{ width: '100%' }}
          options={[
            { value: 'postgres', label: 'Postgres' },
            { value: 'webhook', label: 'Webhook' },
          ]}
        />
      ),
    },
    {
      title: 'URL',
      dataIndex: 'url',
      render: (_, record, index) => record.kind === 'webhook' ? (
        <Input
          value={record.url || ''}
          onChange={(event) => updateSink(index, 'url', event.target.value)}
          placeholder="https://..."
        />
      ) : <Text type="secondary">使用状态库</Text>,
    },
    {
      title: '超时',
      dataIndex: 'timeout',
      width: 130,
      render: (_, record, index) => record.kind === 'webhook' ? (
        <Input
          value={record.timeout || ''}
          onChange={(event) => updateSink(index, 'timeout', event.target.value)}
          placeholder="3s"
        />
      ) : <Text type="secondary">—</Text>,
    },
    {
      title: '操作',
      key: 'action',
      width: 90,
      render: (_, record, index) => {
        const isLastPostgres = record.kind === 'postgres' && postgresCount <= 1
        return (
          <Popconfirm
            title={isLastPostgres ? '至少保留一个 Postgres Sink，无法删除' : '确定删除该 Sink？'}
            onConfirm={() => deleteSink(index)}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            disabled={isLastPostgres}
          >
            <Button type="text" danger icon={<DeleteOutlined />} disabled={isLastPostgres} />
          </Popconfirm>
        )
      },
    },
  ]

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
          <Title level={2} className="page-title">平台设置</Title>
          <Text className="page-subtitle">管理 Trace 默认值和 Sink；AI、对象存储和启动配置仍依赖后端配置接口补齐。</Text>
        </div>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={handleSave}>
          保存设置
        </Button>
      </div>

      <Alert
        type={postgresCount > 0 ? 'info' : 'error'}
        showIcon
        style={{ marginBottom: 14 }}
        message={postgresCount > 0 ? '配置应用状态：已可保存到 DB' : '配置风险：缺少 Postgres Sink'}
        description={
          lastSavedAt
            ? `最近保存：${lastSavedAt.toLocaleString()}。当前后端保存 /api/settings 后未明确返回 trace sink 热生效状态。`
            : '保存后后端会写入 DB；运行中 sink 和 max payload 是否热生效仍需要后端 reload 返回 applied 状态。'
        }
      />

      <div className="two-column-settings">
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card className="ops-card" title="追踪默认值">
            <Form form={form} layout="vertical">
              <Form.Item
                name="max_payload_bytes"
                label="最大内联 Payload 字节数"
                rules={[{ required: true, message: '请输入最大字节数' }]}
              >
                <InputNumber min={0} style={{ width: '100%' }} placeholder="超出后外存到对象存储" />
              </Form.Item>
              <Form.Item
                name="default_retain_days"
                label="默认保留天数"
                rules={[{ required: true, message: '请输入保留天数' }]}
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Form>
          </Card>

          <Card
            className="ops-card"
            title="Sink 管理"
            extra={<Button icon={<PlusOutlined />} onClick={addSink}>添加 Sink</Button>}
          >
            {hasWebhook && (
              <Alert
                type="warning"
                showIcon
                message="Webhook Sink 变更属于风险操作"
                description="删除 Sink、缩短保留期或关闭留痕前需要确认下游消费方。"
                style={{ marginBottom: 12 }}
              />
            )}
            <Table<SinkEntry>
              className="dense-table"
              dataSource={sinks}
              columns={columns}
              rowKey={(_, index) => String(index)}
              pagination={false}
              size="small"
              locale={{ emptyText: '暂无 Sink，点击添加 Sink 创建' }}
            />
          </Card>
        </Space>

        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Card className="ops-card" title="AI 配置">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="开关"><Tag color="default">启动配置</Tag></Descriptions.Item>
              <Descriptions.Item label="Provider / Endpoint"><Text type="secondary">需要平台配置 API</Text></Descriptions.Item>
              <Descriptions.Item label="模型 / 超时 / Token"><Text type="secondary">需要平台配置 API</Text></Descriptions.Item>
              <Descriptions.Item label="插件目录"><Text type="secondary">当前由 TOML 启动参数提供</Text></Descriptions.Item>
            </Descriptions>
          </Card>

          <Card className="ops-card" title="对象存储配置">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="provider / bucket"><Text type="secondary">需要平台配置 API</Text></Descriptions.Item>
              <Descriptions.Item label="endpoint / region"><Text type="secondary">需要平台配置 API</Text></Descriptions.Item>
              <Descriptions.Item label="base path / presign TTL"><Text type="secondary">当前由启动配置提供</Text></Descriptions.Item>
              <Descriptions.Item label="path style"><Text type="secondary">当前由启动配置提供</Text></Descriptions.Item>
            </Descriptions>
          </Card>

          <Card className="ops-card" title="运行配置摘要">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="服务地址"><Text type="secondary">后端未返回</Text></Descriptions.Item>
              <Descriptions.Item label="任务配置目录"><Text type="secondary">后端未返回</Text></Descriptions.Item>
              <Descriptions.Item label="状态存储"><Tag color="blue">Postgres</Tag></Descriptions.Item>
              <Descriptions.Item label="对象存储"><Tag color="default">启动配置</Tag></Descriptions.Item>
              <Descriptions.Item label="AI"><Tag color="default">启动配置</Tag></Descriptions.Item>
            </Descriptions>
          </Card>

          <Card className="ops-card" title="人工恢复能力">
            <Alert
              type="warning"
              showIcon
              message="Degraded / config_error 启动模式仍缺后端支持"
              description="当前初始化对象存储、Postgres 或 AI 插件失败时仍可能阻断管理界面启动，需要后端补 degraded 模式和 /api/platform-config。"
            />
          </Card>
        </Space>
      </div>
    </div>
  )
}
