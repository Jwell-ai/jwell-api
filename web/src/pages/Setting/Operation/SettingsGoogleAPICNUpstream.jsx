/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useRef, useState } from 'react';
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
  verifyJSON,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const googleAPICNGroupMappingExample = JSON.stringify(
  {
    default: 'default',
    vip: 'vip',
  },
  null,
  2,
);

const defaultInputs = {
  'google_api_cn.username': '',
  'google_api_cn.password': '',
  'google_api_cn.token_name': 'jwell-api-upstream',
  'google_api_cn.group': 'default',
  'google_api_cn.group_mapping': '',
  'google_api_cn.auto_bootstrap_enabled': true,
  'google_api_cn.auth_base_url': 'https://google-api.cn',
  'google_api_cn.pricing_url': 'https://google-api.cn/api/pricing',
  'google_api_cn.api_base_url': 'https://gemini-api.cn',
  'google_api_cn.channel_name': 'google-api.cn',
  'google_api_cn.channel_tag': 'google-api-cn',
  'google_api_cn.channel_group': 'default',
  'google_api_cn.bootstrap_models': '',
  'google_api_cn.auto_register_model_ratio_enabled': true,
  'google_api_cn.default_model_ratio': 37.5,
  'google_api_cn.bootstrap_timeout_seconds': 60,
  'google_api_cn.debug_auth_token': false,
};

export default function SettingsGoogleAPICNUpstream(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(defaultInputs);
  const [inputsRow, setInputsRow] = useState(defaultInputs);
  const refForm = useRef();

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function normalizePayloadValue(key, value) {
    if (typeof value === 'boolean') {
      return String(value);
    }
    if (
      key === 'google_api_cn.default_model_ratio' ||
      key === 'google_api_cn.bootstrap_timeout_seconds'
    ) {
      return String(value ?? '');
    }
    return value ?? '';
  }

  async function onSubmit() {
    const groupMapping = String(inputs['google_api_cn.group_mapping'] || '').trim();
    if (groupMapping && !verifyJSON(groupMapping)) {
      return showError(t('google-api.cn 分组映射必须是合法 JSON 对象'));
    }
    if (groupMapping) {
      try {
        const parsed = JSON.parse(groupMapping);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          return showError(t('google-api.cn 分组映射必须是合法 JSON 对象'));
        }
      } catch (error) {
        return showError(t('google-api.cn 分组映射必须是合法 JSON 对象'));
      }
    }

    const updateArray = compareObjects(inputs, inputsRow).filter(
      (item) =>
        item.key !== 'google_api_cn.password' ||
        String(inputs['google_api_cn.password'] || '').trim() !== '',
    );
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));

    const requestQueue = updateArray.map((item) =>
      API.put('/api/option/', {
        key: item.key,
        value: normalizePayloadValue(item.key, inputs[item.key]),
      }),
    );
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (res.includes(undefined)) return showError(t('部分保存失败，请重试'));
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = { ...defaultInputs };
    for (let key in props.options) {
      if (Object.keys(defaultInputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    currentInputs['google_api_cn.password'] = '';
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('google-api.cn 上游配置')}>
            <Banner
              type='info'
              description={t(
                '这些配置属于 google-api.cn 上游账号和令牌，不会修改本平台用户分组、计费和 API Key。',
              )}
              className='mb-4'
            />
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field='google_api_cn.auto_bootstrap_enabled'
                  label={t('自动同步上游渠道')}
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange(
                    'google_api_cn.auto_bootstrap_enabled',
                  )}
                  extraText={t('启用后 master 节点启动时会自动创建/同步渠道和模型')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.username'
                  label={t('上游账号')}
                  placeholder={t('google-api.cn 登录用户名')}
                  onChange={handleFieldChange('google_api_cn.username')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.password'
                  label={t('上游密码')}
                  placeholder={t('敏感信息不显示；留空表示不修改')}
                  type='password'
                  onChange={handleFieldChange('google_api_cn.password')}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.auth_base_url'
                  label={t('上游登录/管理地址')}
                  placeholder='https://google-api.cn'
                  onChange={handleFieldChange('google_api_cn.auth_base_url')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.api_base_url'
                  label={t('上游模型 API 地址')}
                  placeholder='https://gemini-api.cn'
                  onChange={handleFieldChange('google_api_cn.api_base_url')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.pricing_url'
                  label={t('上游模型/价格接口')}
                  placeholder='https://google-api.cn/api/pricing'
                  onChange={handleFieldChange('google_api_cn.pricing_url')}
                  showClear
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.token_name'
                  label={t('上游令牌名称')}
                  placeholder='jwell-api-upstream'
                  onChange={handleFieldChange('google_api_cn.token_name')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.group'
                  label={t('默认上游令牌分组')}
                  placeholder='default'
                  onChange={handleFieldChange('google_api_cn.group')}
                  showClear
                  extraText={t('这是 google-api.cn 上游令牌分组，不是本平台分组')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.channel_group'
                  label={t('本平台渠道分组')}
                  placeholder='default'
                  onChange={handleFieldChange('google_api_cn.channel_group')}
                  showClear
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.channel_name'
                  label={t('渠道名称')}
                  placeholder='google-api.cn'
                  onChange={handleFieldChange('google_api_cn.channel_name')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field='google_api_cn.channel_tag'
                  label={t('渠道标签')}
                  placeholder='google-api-cn'
                  onChange={handleFieldChange('google_api_cn.channel_tag')}
                  showClear
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field='google_api_cn.default_model_ratio'
                  label={t('默认模型倍率')}
                  precision={4}
                  min={0}
                  onChange={handleFieldChange(
                    'google_api_cn.default_model_ratio',
                  )}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field='google_api_cn.auto_register_model_ratio_enabled'
                  label={t('自动补模型倍率')}
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange(
                    'google_api_cn.auto_register_model_ratio_enabled',
                  )}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field='google_api_cn.bootstrap_timeout_seconds'
                  label={t('启动同步超时')}
                  min={1}
                  step={1}
                  suffix={t('秒')}
                  onChange={handleFieldChange(
                    'google_api_cn.bootstrap_timeout_seconds',
                  )}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field='google_api_cn.debug_auth_token'
                  label={t('打印上游 token 指纹')}
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('google_api_cn.debug_auth_token')}
                  extraText={t('只打印掩码和 sha256 指纹，不打印明文 token')}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} sm={12}>
                <Form.TextArea
                  field='google_api_cn.group_mapping'
                  label={t('本平台分组 -> 上游令牌分组映射')}
                  placeholder={googleAPICNGroupMappingExample}
                  autosize={{ minRows: 5, maxRows: 10 }}
                  onChange={handleFieldChange('google_api_cn.group_mapping')}
                  extraText={t(
                    'JSON 对象。键为本平台分组，值为 google-api.cn 上游令牌分组；仅影响上游 key 选择。',
                  )}
                />
              </Col>
              <Col xs={24} sm={12}>
                <Form.TextArea
                  field='google_api_cn.bootstrap_models'
                  label={t('模型同步失败兜底列表')}
                  placeholder='gpt-4o,gemini-2.5-flash,claude-sonnet-4-5'
                  autosize={{ minRows: 5, maxRows: 10 }}
                  onChange={handleFieldChange('google_api_cn.bootstrap_models')}
                  extraText={t('逗号分隔。只有上游模型接口失败时才使用。')}
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存 google-api.cn 上游配置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
