import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Table, Button, Modal, Form, Input, Popconfirm, Space, message, Card, Tag, Typography } from 'antd'
import { PlusOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { fetchPipelines, createPipeline, updatePipeline, deletePipeline, fetchPipelineTasks } from '../api/client'
import type { Pipeline } from '../api/types'
import styles from './PipelineList.module.css'

const { Title, Text } = Typography

function generatePipelineId(): string {
  return `pipe-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

export default function PipelineList() {
  const navigate = useNavigate()
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPipeline, setEditingPipeline] = useState<Pipeline | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [taskCounts, setTaskCounts] = useState<Record<string, number>>({})
  const [form] = Form.useForm()

  const loadPipelines = useCallback(async () => {
    try {
      const data = await fetchPipelines()
      setPipelines(data)
      const counts: Record<string, number> = {}
      await Promise.all(
        data.map(async (p) => {
          try {
            const tasks = await fetchPipelineTasks(p.id)
            counts[p.id] = tasks.length
          } catch {
            counts[p.id] = 0
          }
        }),
      )
      setTaskCounts(counts)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '加载管道失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadPipelines()
  }, [loadPipelines])

  const handleCreate = () => {
    setEditingPipeline(null)
    form.resetFields()
    form.setFieldsValue({ id: generatePipelineId() })
    setModalOpen(true)
  }

  const handleEdit = (pipeline: Pipeline) => {
    setEditingPipeline(pipeline)
    form.setFieldsValue({
      id: pipeline.id,
      name: pipeline.name,
      description: pipeline.description,
    })
    setModalOpen(true)
  }

  const handleDelete = useCallback(
    async (id: string) => {
      try {
        await deletePipeline(id)
        message.success('管道已删除')
        await loadPipelines()
      } catch (err) {
        message.error(err instanceof Error ? err.message : '删除失败')
      }
    },
    [loadPipelines],
  )

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields()
      setSubmitting(true)
      if (editingPipeline) {
        await updatePipeline(editingPipeline.id, {
          name: values.name,
          description: values.description || '',
        })
        message.success('管道已更新')
      } else {
        await createPipeline({
          id: values.id,
          name: values.name,
          description: values.description || '',
        })
        message.success('管道已创建')
      }
      setModalOpen(false)
      form.resetFields()
      await loadPipelines()
    } catch (err) {
      if (err instanceof Error) {
        message.error(err.message)
      }
    } finally {
      setSubmitting(false)
    }
  }

  const columns: ColumnsType<Pipeline> = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 160,
      render: (id: string) => (
        <Text code style={{ fontSize: 12 }}>
          {id}
        </Text>
      ),
    },
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: Pipeline) => (
        <a onClick={() => navigate(`/pipelines/${record.id}`)}>{name}</a>
      ),
    },
    {
      title: '描述',
      dataIndex: 'description',
      key: 'description',
      render: (desc: string) => desc || '\u2014',
    },
    {
      title: '任务数',
      key: 'taskCount',
      width: 80,
      render: (_val: unknown, record: Pipeline) => (
        <Tag>{taskCounts[record.id] ?? '...'}</Tag>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (val: string) => new Date(val).toLocaleString(),
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      render: (_val: unknown, record: Pipeline) => (
        <Space onClick={(e) => e.stopPropagation()}>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>
            编辑
          </Button>
          <Popconfirm
            title={`确定删除管道 ${record.name} 吗？已分配的任务将变为未分配状态。`}
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <Title level={3} style={{ margin: 0 }}>
          管道管理
        </Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={handleCreate}>
          创建管道
        </Button>
      </div>

      <Card>
        <Table<Pipeline>
          columns={columns}
          dataSource={pipelines}
          rowKey="id"
          loading={loading}
          locale={{ emptyText: '暂无管道，点击上方按钮创建' }}
          pagination={{
            pageSize: 20,
            showSizeChanger: true,
            showTotal: (total, range) => `第${range[0]}-${range[1]}条/共${total}条`,
          }}
        />
      </Card>

      <Modal
        title={editingPipeline ? '编辑管道' : '创建管道'}
        open={modalOpen}
        onOk={handleSubmit}
        onCancel={() => {
          setModalOpen(false)
          form.resetFields()
        }}
        confirmLoading={submitting}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="id" label="管道ID">
            <Input disabled />
          </Form.Item>
          <Form.Item
            name="name"
            label="管道名称"
            rules={[{ required: true, message: '请输入管道名称' }]}
          >
            <Input placeholder="例如：生产环境" />
          </Form.Item>
          <Form.Item name="description" label="管道描述">
            <Input.TextArea placeholder="管道用途说明" rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
