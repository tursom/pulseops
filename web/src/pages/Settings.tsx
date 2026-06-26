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
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import { DeleteOutlined, PlusOutlined, SaveOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { GlobalSettings, PlatformConfigSummary, SinkEntry } from '../api/types'
import { fetchPlatformConfig, fetchSettings, updatePlatformConfig, updateSettings } from '../api/client'

const { Title, Text } = Typography

export default function Settings() {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [platformSaving, setPlatformSaving] = useState(false)
  const [form] = Form.useForm()
  const [platformForm] = Form.useForm()
  const [sinks, setSinks] = useState<SinkEntry[]>([])
  const [lastSavedAt, setLastSavedAt] = useState<Date | null>(null)
  const [applied, setApplied] = useState(false)
  const [warnings, setWarnings] = useState<string[]>([])
  const [platform, setPlatform] = useState<PlatformConfigSummary | null>(null)

  useEffect(() => {
    Promise.all([fetchSettings(), fetchPlatformConfig()])
      .then(([response, platformConfig]) => {
        const settings = response.settings
        form.setFieldsValue({
          max_payload_bytes: settings.max_payload_bytes,
          default_retain_days: settings.default_retain_days,
        })
        setSinks(settings.sinks || [])
        setApplied(response.applied)
        setPlatform(platformConfig)
        platformForm.setFieldsValue(platformConfig)
        setWarnings([...(response.warnings || []), ...(platformConfig.warnings || [])])
      })
      .catch((err) => message.error(err instanceof Error ? err.message : '加载设置失败'))
      .finally(() => setLoading(false))
  }, [form, platformForm])

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
      const response = await updateSettings(settings)
      setApplied(response.applied)
      setWarnings(response.warnings || [])
      setLastSavedAt(new Date())
      message.success(response.applied ? '设置已保存并热应用' : '设置已保存')
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error(err instanceof Error ? err.message : '保存设置失败')
    } finally {
      setSaving(false)
    }
  }

  const handleSavePlatform = async () => {
    if (!platform) return
    try {
      setPlatformSaving(true)
      const values = await platformForm.validateFields()
      const updated: PlatformConfigSummary = {
        ...platform,
        ai: { ...platform.ai, ...(values.ai || {}) },
        artifact_store: { ...platform.artifact_store, ...(values.artifact_store || {}) },
      }
      const response = await updatePlatformConfig(updated)
      setPlatform(response)
      setWarnings(response.warnings || [])
      message.success('平台配置已保存，重启后应用')
    } catch (err) {
      if (err && typeof err === 'object' && 'errorFields' in err) return
      message.error(err instanceof Error ? err.message : '保存平台配置失败')
    } finally {
      setPlatformSaving(false)
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
            ? `最近保存：${lastSavedAt.toLocaleString()}。${applied ? '后端已返回热应用成功。' : '后端未完成热应用，请检查告警。'}`
            : `${applied ? '当前配置已应用。' : '当前配置仅已读取，保存后会返回应用状态。'}`
        }
      />

      {warnings.length > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 14 }}
          message="配置提示"
          description={warnings.join('；')}
        />
      )}

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
            <Form form={platformForm} layout="vertical">
              <Form.Item name={['ai', 'enabled']} label="AI 开关" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item name={['ai', 'endpoint']} label="Endpoint">
                <Input placeholder="https://api.example.com" />
              </Form.Item>
              <Form.Item name={['ai', 'model']} label="模型">
                <Input placeholder="deepseek-chat" />
              </Form.Item>
              <Space style={{ width: '100%' }} align="start">
                <Form.Item name={['ai', 'timeout']} label="超时">
                  <Input placeholder="30s" />
                </Form.Item>
                <Form.Item name={['ai', 'max_tokens']} label="Max Tokens">
                  <InputNumber min={0} />
                </Form.Item>
                <Form.Item name={['ai', 'temperature']} label="Temperature">
                  <InputNumber min={0} max={2} step={0.1} />
                </Form.Item>
              </Space>
              <Form.Item name={['ai', 'plugin_dir']} label="插件目录">
                <Input placeholder="plugins" />
              </Form.Item>
              <Tag color={platform?.ai.status === 'config_error' ? 'orange' : 'blue'}>{platform?.ai.status || '启动配置'}</Tag>
              {platform?.ai.error && <Text type="danger"> {platform.ai.error}</Text>}
            </Form>
          </Card>

          <Card className="ops-card" title="对象存储配置">
            <Form form={platformForm} layout="vertical">
              <Space style={{ width: '100%' }} align="start">
                <Form.Item name={['artifact_store', 'provider']} label="Provider">
                  <Input placeholder="minio" />
                </Form.Item>
                <Form.Item name={['artifact_store', 'bucket']} label="Bucket">
                  <Input />
                </Form.Item>
              </Space>
              <Space style={{ width: '100%' }} align="start">
                <Form.Item name={['artifact_store', 'endpoint']} label="Endpoint">
                  <Input />
                </Form.Item>
                <Form.Item name={['artifact_store', 'region']} label="Region">
                  <Input />
                </Form.Item>
              </Space>
              <Space style={{ width: '100%' }} align="start">
                <Form.Item name={['artifact_store', 'base_path']} label="Base Path">
                  <Input />
                </Form.Item>
                <Form.Item name={['artifact_store', 'presign_ttl']} label="Presign TTL">
                  <Input placeholder="15m" />
                </Form.Item>
              </Space>
              <Space>
                <Form.Item name={['artifact_store', 'force_path_style']} valuePropName="checked">
                  <Switch checkedChildren="Path Style" unCheckedChildren="Auto" />
                </Form.Item>
                <Form.Item name={['artifact_store', 'use_ssl']} valuePropName="checked">
                  <Switch checkedChildren="SSL" unCheckedChildren="Plain" />
                </Form.Item>
              </Space>
              <Tag color={platform?.artifact_store.status === 'config_error' ? 'orange' : 'blue'}>{platform?.artifact_store.status || '启动配置'}</Tag>
              {platform?.artifact_store.error && <Text type="danger"> {platform.artifact_store.error}</Text>}
            </Form>
          </Card>

          <Card className="ops-card" title="平台配置保存">
            <Space direction="vertical" style={{ width: '100%' }}>
              <Alert
                type="info"
                showIcon
                message="AI 和对象存储配置保存到 DB 后需要重启服务应用"
                description="Trace Sink 和最大 Payload 字节数在上方保存后可热应用。"
              />
              <Button block type="primary" icon={<SaveOutlined />} loading={platformSaving} onClick={handleSavePlatform}>
                保存平台配置
              </Button>
            </Space>
          </Card>

          <Card className="ops-card" title="运行配置摘要">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="服务地址"><Text type="secondary">{platform?.server.addr || '—'}</Text></Descriptions.Item>
              <Descriptions.Item label="任务配置目录"><Text type="secondary">{platform?.task.config_dir || '—'}</Text></Descriptions.Item>
              <Descriptions.Item label="状态存储"><Tag color="blue">{platform?.state.backend || 'Postgres'}</Tag></Descriptions.Item>
              <Descriptions.Item label="对象存储"><Tag color={platform?.artifact_store.status === 'config_error' ? 'orange' : 'blue'}>{platform?.artifact_store.kind || '—'}</Tag></Descriptions.Item>
              <Descriptions.Item label="AI"><Tag color={platform?.ai.enabled ? 'green' : 'default'}>{platform?.ai.enabled ? platform.ai.model || '启用' : '关闭'}</Tag></Descriptions.Item>
            </Descriptions>
          </Card>

          <Card className="ops-card" title="人工恢复能力">
            <Alert
              type={platform?.mode === 'degraded' ? 'warning' : 'success'}
              showIcon
              message={platform?.mode === 'degraded' ? '当前处于 degraded / config_error 模式' : '当前平台配置已应用'}
              description={platform?.mode === 'degraded'
                ? '非核心配置错误不会阻断管理界面；请根据上方错误修复对象存储或 AI 配置。Postgres 状态库仍是启动核心依赖。'
                : '后端已返回平台配置摘要；对象存储和 AI 状态可在本页确认。'}
            />
          </Card>
        </Space>
      </div>
    </div>
  )
}
