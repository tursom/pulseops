import React, { useState, useEffect } from 'react';
import { Form, Input, Select, Button, Space, Tooltip, Typography, Spin } from 'antd';
import type { FormInstance } from 'antd';
import { MinusCircleOutlined, PlusOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import JsonFieldPicker from '../../JsonFieldPicker';
import { fetchTaskDefinitions } from '../../../api/client';
import type { TaskDefinition } from '../../../api/types';

const { Text } = Typography;

interface SourceOption {
  value: string;
  label: string;
  description: string;
}

const sourceOptions: SourceOption[] = [
  { value: 'payload', label: 'Payload — 上游任务返回的 JSON 数据体', description: '上游任务执行后存储的原始 JSON 数据（如 HTTP 响应体）' },
  { value: 'summary', label: 'Summary — 上游任务的摘要字段', description: '上游任务自己定义的摘要信息（如 status_code、response_time 等）' },
  { value: 'record', label: 'Record — 上游任务的运行记录', description: '上游任务的运行元数据：duration_ms, check_status, run_id, task_id, trigger_type 等' },
  { value: 'artifact:payload', label: 'Artifact:Payload — 上游产物（payload）', description: '上游任务存储的 payload 产物文件内容' },
  { value: 'artifact:stdout', label: 'Artifact:Stdout — 上游标准输出', description: '上游任务的标准输出产物' },
  { value: 'artifact:stderr', label: 'Artifact:Stderr — 上游标准错误', description: '上游任务的标准错误输出产物' },
];

const aggModeOptions = [
  { value: '', label: '无（直接透传）' },
  { value: 'sum', label: '求和 (sum)' },
  { value: 'avg', label: '平均值 (avg)' },
  { value: 'count', label: '计数 (count)' },
  { value: 'min', label: '最小值 (min)' },
  { value: 'max', label: '最大值 (max)' },
];

const UpstreamDataParams: React.FC<{ form?: FormInstance }> = ({ form }) => {
  const sourceTaskId = form ? Form.useWatch(['params', 'source_task_id'], form) as string : undefined;
  const trigger = form ? Form.useWatch('trigger', form) as string : undefined;
  const watchTaskId = form ? Form.useWatch('watch_task_id', form) as string : undefined;
  const resolvedTaskId = sourceTaskId || (trigger === 'on_run' ? watchTaskId : null) || null;

  const [taskDefs, setTaskDefs] = useState<TaskDefinition[]>([]);
  const [taskDefsLoading, setTaskDefsLoading] = useState(false);

  useEffect(() => {
    setTaskDefsLoading(true);
    fetchTaskDefinitions()
      .then((defs) => setTaskDefs(defs.filter((d) => d.enabled)))
      .catch(() => {})
      .finally(() => setTaskDefsLoading(false));
  }, []);

  return (
    <>
      <Form.Item
        name={['params', 'source_task_id']}
        label="源任务"
        extra="留空则使用依赖触发（watch_task）的上游任务作为数据源"
      >
        <Select
          allowClear
          loading={taskDefsLoading}
          notFoundContent={taskDefsLoading ? <Spin size="small" /> : '没有启用的任务'}
          placeholder="留空默认使用 watch_task"
          options={taskDefs.map((d) => ({
            value: d.task_id,
            label: `${d.name} (${d.task_id})`,
          }))}
          showSearch
          optionFilterProp="label"
        />
      </Form.Item>

      <div style={{ marginBottom: 12 }}>
        <Text strong>提取表达式</Text>
        <Tooltip title="定义从上游数据中提取哪些字段。每个表达式指定：输出字段名、数据来源、jq 提取规则、可选的聚合方式。">
          <QuestionCircleOutlined style={{ marginLeft: 6, color: '#999', cursor: 'help' }} />
        </Tooltip>
      </div>

      <Form.List name={['params', 'extract_exprs']}>
        {(fields, { add, remove }) => (
          <>
            {fields.map(({ key, name, ...restField }) => (
              <Space
                key={key}
                style={{ display: 'flex', marginBottom: 8, flexWrap: 'wrap' }}
                align="start"
              >
                <Form.Item
                  {...restField}
                  name={[name, 'field']}
                  label="字段名"
                  rules={[{ required: true, message: '请输入输出字段名' }]}
                  tooltip="提取结果在 Summary 中的字段名，如 cost_ms、status_code"
                >
                  <Input placeholder="如 cost_ms" style={{ width: 120 }} />
                </Form.Item>

                <Form.Item
                  {...restField}
                  name={[name, 'source']}
                  label="数据源"
                  rules={[{ required: true, message: '请选择数据源' }]}
                  tooltip="从上游任务的哪个数据源提取"
                >
                  <Select
                    style={{ width: 280 }}
                    options={sourceOptions.map((opt) => ({
                      value: opt.value,
                      label: (
                        <Tooltip title={opt.description} placement="right">
                          {opt.label}
                        </Tooltip>
                      ),
                    }))}
                    optionFilterProp="label"
                    showSearch
                  />
                </Form.Item>

                <Form.Item shouldUpdate noStyle>
                  {(fm) => {
                    const src = fm.getFieldValue(['params', 'extract_exprs', name, 'source']) as string | undefined;
                    const isVisualSource = src && !src.startsWith('artifact:');
                    const fieldLabel = isVisualSource ? '字段选择' : 'JQ 表达式';
                    return (
                      <Form.Item
                        {...restField}
                        name={[name, 'jq_expr']}
                        label={fieldLabel}
                        rules={[{ required: true, message: '请选择或输入 jq 表达式' }]}
                        tooltip={isVisualSource ? '点击树节点自动生成 jq 表达式' : '手写 jq 表达式提取数据'}
                      >
                        {isVisualSource ? (
                          <JsonFieldPicker
                            sourceTaskId={resolvedTaskId}
                            source={src!}
                          />
                        ) : (
                          <Input placeholder="如 .duration_ms" style={{ width: 180 }} />
                        )}
                      </Form.Item>
                    );
                  }}
                </Form.Item>

                <Form.Item
                  {...restField}
                  name={[name, 'agg_mode']}
                  label="聚合"
                  tooltip="对 jq 提取结果（数组）做聚合运算，数值有效。非数组或非数值则透传原值。"
                >
                  <Select
                    style={{ width: 140 }}
                    options={aggModeOptions}
                    allowClear
                  />
                </Form.Item>

                <MinusCircleOutlined
                  onClick={() => remove(name)}
                  style={{ marginTop: 8, color: '#ff4d4f' }}
                />
              </Space>
            ))}

            <Form.Item>
              <Button
                type="dashed"
                onClick={() => add({ source: 'record' })}
                block
                icon={<PlusOutlined />}
              >
                添加提取表达式
              </Button>
            </Form.Item>
          </>
        )}
      </Form.List>

      <div style={{ background: '#f6f8fa', padding: 12, borderRadius: 6, marginTop: 8 }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          💡 <strong>字段选择器说明：</strong>
          <br />
          · 选择 <strong>payload / summary / record</strong> 数据源后，展开树点击字段即可自动生成 jq 表达式
          <br />
          · 选择 <strong>artifact:*</strong> 数据源时需手写 jq（产物结构不固定）
          <br />
          · 点击「手写」切换到原始 jq 输入模式
          <br />
          · 聚合仅对 jq 提取的数值数组有效
        </Text>
      </div>
    </>
  );
};

export default UpstreamDataParams;
