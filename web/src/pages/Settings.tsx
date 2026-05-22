import { useState, useEffect } from 'react'
import {
  Card,
  Form,
  InputNumber,
  Button,
  Table,
  Input,
  Select,
  Popconfirm,
  message,
  Spin,
  Space,
  Typography,
} from 'antd'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons'
import type { GlobalSettings, SinkEntry } from '../api/types'
import { fetchSettings, updateSettings } from '../api/client'

const { Title } = Typography

export default function Settings() {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()
  const [sinks, setSinks] = useState<SinkEntry[]>([])

  useEffect(() => {
    fetchSettings()
      .then((settings) => {
        form.setFieldsValue({
          max_payload_bytes: settings.max_payload_bytes,
          default_retain_days: settings.default_retain_days,
        })
        setSinks(settings.sinks || [])
      })
      .catch(() => message.error('加载设置失败'))
      .finally(() => setLoading(false))
  }, [form])

  const handleSave = async () => {
    try {
      setSaving(true)
      const values = await form.validateFields()
      const postgresSinks = sinks.filter((s) => s.kind === 'postgres')
      if (postgresSinks.length === 0) {
        message.error('至少保留一个 postgres Sink')
        return
      }
      const settings: GlobalSettings = {
        max_payload_bytes: values.max_payload_bytes,
        default_retain_days: values.default_retain_days,
        sinks,
      }
      await updateSettings(settings)
      message.success('设置已保存')
    } catch (e) {
      if (e && typeof e === 'object' && 'errorFields' in e) return
      message.error('保存设置失败')
    } finally {
      setSaving(false)
    }
  }

  const addSink = () => {
    setSinks([...sinks, { name: '', kind: 'postgres' }])
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
    const postgresSinks = sinks.filter((s) => s.kind === 'postgres')
    const target = sinks[index]
    if (target.kind === 'postgres' && postgresSinks.length <= 1) {
      message.warning('至少保留一个 postgres Sink')
      return
    }
    setSinks(sinks.filter((_, i) => i !== index))
  }

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (_: string, record: SinkEntry, index: number) => (
        <Input
          value={record.name}
          onChange={(e) => updateSink(index, 'name', e.target.value)}
          placeholder="Sink 名称"
        />
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 140,
      render: (_: string, record: SinkEntry, index: number) => (
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
      key: 'url',
      render: (_: string, record: SinkEntry, index: number) => {
        if (record.kind !== 'webhook') return null
        return (
          <Input
            value={record.url || ''}
            onChange={(e) => updateSink(index, 'url', e.target.value)}
            placeholder="https://..."
          />
        )
      },
    },
    {
      title: '超时',
      dataIndex: 'timeout',
      key: 'timeout',
      width: 120,
      render: (_: string, record: SinkEntry, index: number) => {
        if (record.kind !== 'webhook') return null
        return (
          <Input
            value={record.timeout || ''}
            onChange={(e) => updateSink(index, 'timeout', e.target.value)}
            placeholder="3s"
          />
        )
      },
    },
    {
      title: '操作',
      key: 'action',
      width: 80,
      render: (_: unknown, record: SinkEntry, index: number) => {
        const postgresCount = sinks.filter((s) => s.kind === 'postgres').length
        const isLastPostgres = record.kind === 'postgres' && postgresCount <= 1
        return (
          <Popconfirm
            title={isLastPostgres ? '至少保留一个 postgres Sink，无法删除' : '确定删除？'}
            onConfirm={() => deleteSink(index)}
            okText="确定"
            cancelText="取消"
            disabled={isLastPostgres}
          >
            <Button
              type="text"
              danger
              icon={<DeleteOutlined />}
              disabled={isLastPostgres}
            />
          </Popconfirm>
        )
      },
    },
  ]

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 80 }}>
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div>
      <Title level={3} style={{ marginBottom: 24 }}>全局设置</Title>

      <Card title="追踪设置" style={{ marginBottom: 24 }}>
        <Form form={form} layout="horizontal" labelCol={{ span: 6 }} wrapperCol={{ span: 18 }}>
          <Form.Item name="max_payload_bytes" label="单次最大数据字节">
            <InputNumber min={0} style={{ width: '100%' }} placeholder="超出后外存到MinIO" />
          </Form.Item>
          <Form.Item name="default_retain_days" label="默认保留天数">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Card>

      <Card
        title="Sink 管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={addSink}>
            添加 Sink
          </Button>
        }
        style={{ marginBottom: 24 }}
      >
        <Table
          dataSource={sinks}
          columns={columns}
          rowKey={(_, index) => String(index)}
          pagination={false}
          locale={{ emptyText: '暂无 Sink，点击"添加 Sink"创建' }}
        />
      </Card>

      <Space style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button type="primary" onClick={handleSave} loading={saving}>
          保存设置
        </Button>
      </Space>
    </div>
  )
}
