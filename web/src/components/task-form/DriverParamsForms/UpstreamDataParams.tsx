import React from 'react';
import { Form, Input, Select, Button, Space } from 'antd';
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';

const sourceOptions = [
  { value: 'payload', label: 'payload' },
  { value: 'summary', label: 'summary' },
  { value: 'artifact:payload', label: 'artifact:payload' },
  { value: 'artifact:stdout', label: 'artifact:stdout' },
  { value: 'artifact:stderr', label: 'artifact:stderr' },
];

const aggModeOptions = [
  { value: '', label: '无' },
  { value: 'sum', label: 'sum' },
  { value: 'avg', label: 'avg' },
  { value: 'count', label: 'count' },
  { value: 'min', label: 'min' },
  { value: 'max', label: 'max' },
];

const UpstreamDataParams: React.FC = () => {
  return (
    <>
      <Form.Item
        name={['params', 'source_task_id']}
        label="源任务ID"
        extra="可选，默认使用 watch_task"
      >
        <Input placeholder="可选，默认使用 watch_task" />
      </Form.Item>

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
                >
                  <Input placeholder="输出字段名" />
                </Form.Item>

                <Form.Item
                  {...restField}
                  name={[name, 'source']}
                  label="数据源"
                  rules={[{ required: true, message: '请选择数据源' }]}
                >
                  <Select style={{ width: 160 }} options={sourceOptions} />
                </Form.Item>

                <Form.Item
                  {...restField}
                  name={[name, 'jq_expr']}
                  label="JQ 表达式"
                  rules={[{ required: true, message: '请输入 jq 表达式' }]}
                >
                  <Input placeholder='例如: .data.items[]' />
                </Form.Item>

                <Form.Item
                  {...restField}
                  name={[name, 'agg_mode']}
                  label="聚合"
                >
                  <Select
                    style={{ width: 100 }}
                    options={aggModeOptions}
                    allowClear
                  />
                </Form.Item>

                <MinusCircleOutlined
                  onClick={() => remove(name)}
                  style={{ marginTop: 8 }}
                />
              </Space>
            ))}

            <Form.Item>
              <Button
                type="dashed"
                onClick={() => add({ source: 'payload' })}
                block
                icon={<PlusOutlined />}
              >
                添加表达式
              </Button>
            </Form.Item>
          </>
        )}
      </Form.List>
    </>
  );
};

export default UpstreamDataParams;
