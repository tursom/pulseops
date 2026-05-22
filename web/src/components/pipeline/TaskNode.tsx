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
  PlayCircleOutlined,
  PoweroffOutlined,
  EditOutlined,
  DeleteOutlined,
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
}

const KIND_ICONS: Record<string, React.ReactNode> = {
  http_check: <GlobalOutlined />,
  tcp_check: <ClusterOutlined />,
  script_exec: <CodeOutlined />,
  scenario_check: <PartitionOutlined />,
  process_check: <MonitorOutlined />,
  ai_analyze: <RobotOutlined />,
}

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
  const status = data.status as string | undefined
  const lastRunStatus = data.lastRunStatus as string | undefined
  const onRefresh = data.onRefresh as (() => void) | undefined

  const borderColor = KIND_COLORS[kind] || '#d9d9d9'
  const dotColor = STATUS_DOT_COLORS[status || ''] || '#d9d9d9'

  const handleClick = () => {
    navigate(`/task-defs/${taskId}/edit`)
  }

  const handleMenuClick = async ({ key }: { key: string }) => {
    switch (key) {
      case 'run':
        try {
          await triggerTaskRun(taskId)
          message.success('Task triggered')
          onRefresh?.()
        } catch (err) {
          message.error(err instanceof Error ? err.message : 'Failed to trigger task')
        }
        break
      case 'toggle':
        try {
          if (enabled) {
            await disableTask(taskId)
            message.success('Task disabled')
          } else {
            await enableTask(taskId)
            message.success('Task enabled')
          }
          onRefresh?.()
        } catch (err) {
          message.error(err instanceof Error ? err.message : 'Failed to toggle task')
        }
        break
      case 'edit':
        navigate(`/task-defs/${taskId}/edit`)
        break
      case 'delete':
        Modal.confirm({
          title: 'Delete Task',
          content: `Are you sure you want to delete "${name}"? This action cannot be undone.`,
          okText: 'Delete',
          okButtonProps: { danger: true },
          onOk: async () => {
            try {
              await deleteTaskDefinition(taskId)
              message.success('Task deleted')
              window.location.reload()
            } catch (err) {
              message.error(err instanceof Error ? err.message : 'Failed to delete task')
            }
          },
        })
        break
    }
  }

  const menuItems = [
    { key: 'run', label: 'Run Now', icon: <PlayCircleOutlined /> },
    { key: 'toggle', label: enabled ? 'Disable' : 'Enable', icon: <PoweroffOutlined /> },
    { key: 'edit', label: 'Edit', icon: <EditOutlined /> },
    { type: 'divider' as const },
    { key: 'delete', label: 'Delete', danger: true, icon: <DeleteOutlined /> },
  ]

  return (
    <Dropdown menu={{ items: menuItems, onClick: handleMenuClick }} trigger={['contextMenu']}>
      <div
        onClick={handleClick}
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
            {kind}
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
