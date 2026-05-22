import { useState, useEffect, useCallback } from 'react'
import { Typography, Button, Space, Spin, Breadcrumb, Modal, Table, Tag, message } from 'antd'
import { PlusOutlined, ReloadOutlined, ImportOutlined } from '@ant-design/icons'
import { useNavigate, useParams } from 'react-router-dom'
import PipelineCanvas from '../components/pipeline/PipelineCanvas'
import { fetchPipeline, fetchTaskDefinitions, fetchPipelineTasks, assignTaskToPipeline } from '../api/client'
import type { Pipeline, TaskDefinition } from '../api/types'

const { Title, Text } = Typography

export default function PipelineEditor() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [pipeline, setPipeline] = useState<Pipeline | null>(null)
  const [pipelineLoading, setPipelineLoading] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)
  const [importModalOpen, setImportModalOpen] = useState(false)
  const [importableTasks, setImportableTasks] = useState<TaskDefinition[]>([])
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([])
  const [importing, setImporting] = useState(false)

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

  const loadImportableTasks = useCallback(async () => {
    if (!id) return
    try {
      const [allTasks, pipelineTasks] = await Promise.all([
        fetchTaskDefinitions(),
        fetchPipelineTasks(id),
      ])
      const pipelineTaskIds = new Set(pipelineTasks.map(t => t.task_id))
      setImportableTasks(allTasks.filter(t => !pipelineTaskIds.has(t.task_id)))
    } catch {
      // silent
    }
  }, [id])

  const handleImport = async () => {
    if (!id || selectedTaskIds.length === 0) return
    setImporting(true)
    try {
      await Promise.all(selectedTaskIds.map(taskId => assignTaskToPipeline(id, taskId)))
      message.success(`成功导入 ${selectedTaskIds.length} 个任务`)
      setImportModalOpen(false)
      setSelectedTaskIds([])
      await loadImportableTasks()
      setRefreshKey(k => k + 1)
    } catch (err) {
      message.error(err instanceof Error ? err.message : '导入失败')
    } finally {
      setImporting(false)
    }
  }

  useEffect(() => {
    loadImportableTasks()
  }, [loadImportableTasks])

  if (pipelineLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '60vh' }}>
        <Spin />
      </div>
    )
  }

  return (
    <>
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
              <Button icon={<ImportOutlined />} onClick={() => setImportModalOpen(true)}>
                导入任务
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
      <Modal
        title="导入任务到流水线"
        open={importModalOpen}
        onOk={handleImport}
        onCancel={() => { setImportModalOpen(false); setSelectedTaskIds([]) }}
        confirmLoading={importing}
        okText="导入"
        cancelText="取消"
      >
        <Table
          rowSelection={{
            type: 'checkbox',
            selectedRowKeys: selectedTaskIds,
            onChange: (keys) => setSelectedTaskIds(keys as string[]),
          }}
          dataSource={importableTasks}
          rowKey="task_id"
          columns={[
            { title: '任务ID', dataIndex: 'task_id', key: 'task_id', render: (id: string) => <Text code>{id}</Text> },
            { title: '名称', dataIndex: 'name', key: 'name' },
            { title: '类型', dataIndex: 'kind', key: 'kind' },
            { title: '所属管道', dataIndex: 'pipeline_id', key: 'pipeline_id', render: (pid: string | null) => pid ? <Tag>{pid}</Tag> : <Tag color="default">未分配</Tag> },
          ]}
          pagination={{ pageSize: 10 }}
          locale={{ emptyText: '没有可导入的任务' }}
        />
      </Modal>
    </>
  )
}
