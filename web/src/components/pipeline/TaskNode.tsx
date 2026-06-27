import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { useNavigate } from 'react-router-dom'
import { Dropdown, Modal, Tag, message } from 'antd'
import {
  GlobalOutlined,
  ClusterOutlined,
  CodeOutlined,
  PartitionOutlined,
  MonitorOutlined,
  RobotOutlined,
  SwapOutlined,
  PlayCircleOutlined,
  PoweroffOutlined,
  EditOutlined,
  DeleteOutlined,
  PlusCircleOutlined,
  InfoCircleOutlined,
} from '@ant-design/icons'
import { triggerTaskRun, enableTask, disableTask, deleteTaskDefinition } from '../../api/client'
import type { TaskNodeType } from './types'

const KIND_COLORS: Record<string, string> = {
  http_check: '#1890ff',
  tcp_check: '#52c41a',
  script_exec: '#fa8c16',
  scenario_check: '#722ed1',
  process_check: '#13c2c2',
  ai_analyze: '#f5222d',
  data_process: '#faad14',
}

const KIND_ICONS: Record<string, React.ReactNode> = {
  http_check: <GlobalOutlined />,
  tcp_check: <ClusterOutlined />,
  script_exec: <CodeOutlined />,
  scenario_check: <PartitionOutlined />,
  process_check: <MonitorOutlined />,
  ai_analyze: <RobotOutlined />,
  data_process: <SwapOutlined />,
}

const KIND_LABELS: Record<string, string> = {
  http_check: 'HTTP 检查',
  tcp_check: 'TCP 检查',
  script_exec: '脚本执行',
  scenario_check: '场景检查',
  process_check: '进程检查',
  ai_analyze: 'AI 分析',
  data_process: '数据处理',
}

const DOWNSTREAM_KIND_OPTIONS = ['data_process', 'ai_analyze']

const STATUS_DOT_COLORS: Record<string, string> = {
  running: '#52c41a',
  loaded: '#1890ff',
  disabled: '#bfbfbf',
}

function TaskNode({ data }: NodeProps<TaskNodeType>) {
  const navigate = useNavigate()
  const kind = data.kind as string
  const name = data.name as string
  const enabled = data.enabled as boolean
  const taskId = data.taskId as string
  const pipelineId = data.pipelineId as string | undefined
  const status = data.status as string | undefined
  const lastRunStatus = data.lastRunStatus as string | undefined
  const onRefresh = data.onRefresh as (() => void) | undefined

  const editUrl = `/task-defs/${taskId}/edit` + (pipelineId ? `?from=/pipelines/${encodeURIComponent(pipelineId)}` : '')
  const detailUrl = `/tasks/${taskId}` + (pipelineId ? `?from=/pipelines/${encodeURIComponent(pipelineId)}` : '')

  const borderColor = KIND_COLORS[kind] || '#d9d9d9'
  const dotColor = STATUS_DOT_COLORS[status || ''] || '#d9d9d9'

  const handleMenuClick = async ({ key }: { key: string }) => {
    switch (key) {
      case 'run':
        try {
          await triggerTaskRun(taskId)
          message.success('任务已触发')
          onRefresh?.()
        } catch (err) {
          message.error(err instanceof Error ? err.message : '触发任务失败')
        }
        break
      case 'toggle':
        try {
          if (enabled) {
            await disableTask(taskId)
            message.success('任务已禁用')
          } else {
            await enableTask(taskId)
            message.success('任务已启用')
          }
          onRefresh?.()
        } catch (err) {
            message.error(err instanceof Error ? err.message : '切换启用状态失败')
        }
        break
      case 'detail':
        navigate(detailUrl)
        break
      case 'edit':
        navigate(editUrl)
        break
      case 'delete':
        Modal.confirm({
          title: '删除任务',
          content: `确定要删除 "${name}" 吗？此操作不可撤销。`,
          okText: '删除',
          okButtonProps: { danger: true },
          onOk: async () => {
            try {
              await deleteTaskDefinition(taskId)
              message.success('任务已删除')
              onRefresh?.()
            } catch (err) {
              message.error(err instanceof Error ? err.message : '删除任务失败')
            }
          },
        })
        break
      default:
        if (key.startsWith('add-downstream:')) {
          const downstreamKind = key.split(':')[1]
          const params = new URLSearchParams({
            kind: downstreamKind,
            upstream_task_id: taskId,
            upstream_name: name,
          })
          if (pipelineId) {
            params.set('pipeline', pipelineId)
            params.set('from', `/pipelines/${pipelineId}`)
          }
          navigate(`/task-defs/new?${params.toString()}`)
        }
        break
    }
  }

  const menuItems = [
    { key: 'run', label: '立即执行', icon: <PlayCircleOutlined /> },
    { key: 'toggle', label: enabled ? '禁用' : '启用', icon: <PoweroffOutlined /> },
    { key: 'detail', label: '查看详情', icon: <InfoCircleOutlined /> },
    { key: 'edit', label: '编辑', icon: <EditOutlined /> },
    {
      key: 'add-downstream',
      label: '创建下游任务',
      icon: <PlusCircleOutlined />,
      children: DOWNSTREAM_KIND_OPTIONS.map((kindValue) => ({
        key: `add-downstream:${kindValue}`,
        label: `创建${KIND_LABELS[kindValue]}`,
      })),
    },
    { type: 'divider' as const },
    { key: 'delete', label: '删除', danger: true, icon: <DeleteOutlined /> },
  ]

  return (
    <Dropdown menu={{ items: menuItems, onClick: handleMenuClick }} trigger={['contextMenu']}>
      <div
        style={{
          width: 240,
          padding: '12px 14px',
          borderRadius: 8,
          border: '1px solid #e8e8e8',
          borderLeft: `4px solid ${borderColor}`,
          backgroundColor: enabled ? '#fff' : '#fafafa',
          cursor: 'pointer',
          boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
          transition: 'box-shadow 0.2s, transform 0.1s',
          position: 'relative',
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.boxShadow = '0 3px 8px rgba(0,0,0,0.15)'
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.boxShadow = '0 1px 3px rgba(0,0,0,0.08)'
        }}
      >
        <Handle
          type="target"
          position={Position.Left}
          isConnectable={true}
          style={{
            background: '#1890ff',
            width: 10,
            height: 10,
            border: '2px solid #fff',
            borderRadius: '50%',
          }}
        />
        <Handle
          type="source"
          position={Position.Right}
          isConnectable={true}
          style={{
            background: '#faad14',
            width: 10,
            height: 10,
            border: '2px solid #fff',
            borderRadius: '50%',
          }}
        />

        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <Tag
            color={KIND_COLORS[kind] ? undefined : 'default'}
            style={{
              margin: 0,
              backgroundColor: borderColor,
              color: '#fff',
              border: 'none',
              fontSize: 11,
              lineHeight: '20px',
            }}
            icon={KIND_ICONS[kind]}
          >
            {KIND_LABELS[kind] || kind}
          </Tag>
        </div>

        <div
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: enabled ? '#262626' : '#bfbfbf',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            marginBottom: 8,
          }}
          title={name}
        >
          {name}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <span
              style={{
                display: 'inline-block',
                width: 8,
                height: 8,
                borderRadius: '50%',
                backgroundColor: dotColor,
                flexShrink: 0,
              }}
            />
            <span style={{ color: '#8c8c8c' }}>
              {status || (enabled ? 'unknown' : 'disabled')}
            </span>
          </div>

          {lastRunStatus && (
            <Tag
              style={{
                margin: 0,
                fontSize: 10,
                lineHeight: '18px',
                padding: '0 6px',
              }}
              color={lastRunStatus === 'success' ? 'success' : lastRunStatus === 'failed' ? 'error' : 'default'}
            >
              {lastRunStatus}
            </Tag>
          )}

          {!enabled && (
            <Tag style={{ margin: 0, fontSize: 10, lineHeight: '18px', padding: '0 6px' }} color="default">
              disabled
            </Tag>
          )}
        </div>
      </div>
    </Dropdown>
  )
}

export default memo(TaskNode)
