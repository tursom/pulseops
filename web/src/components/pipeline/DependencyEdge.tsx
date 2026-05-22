import { memo } from 'react'
import {
  BaseEdge,
  getSmoothStepPath,
  type EdgeProps,
} from '@xyflow/react'
import type { DependencyEdgeType } from './types'

function DependencyEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  style,
  markerEnd,
}: EdgeProps<DependencyEdgeType>) {
  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })

  const condition = (data as Record<string, unknown> | undefined)?.condition as string | undefined

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        style={{
          stroke: '#faad14',
          strokeWidth: 1.5,
          strokeDasharray: '5,5',
          ...style,
        }}
        markerEnd={markerEnd}
      />
      {condition && (
        <foreignObject
          width={200}
          height={24}
          x={labelX - 100}
          y={labelY - 12}
          style={{ overflow: 'visible' }}
          requiredExtensions="http://www.w3.org/1999/xhtml"
        >
          <div
            style={{
              display: 'flex',
              justifyContent: 'center',
              pointerEvents: 'none',
            }}
          >
            <span
              style={{
                background: '#fffbe6',
                border: '1px solid #ffe58f',
                borderRadius: 4,
                padding: '1px 6px',
                fontSize: 10,
                color: '#ad8b00',
                whiteSpace: 'nowrap',
                display: 'inline-block',
              }}
            >
              {condition}
            </span>
          </div>
        </foreignObject>
      )}
    </>
  )
}

export default memo(DependencyEdge)
