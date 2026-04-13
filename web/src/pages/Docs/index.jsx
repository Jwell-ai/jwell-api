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

import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Tabs, TabPane, Typography, Card, Button, Tooltip } from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { getSystemName, showSuccess } from '../../helpers';

const { Title, Text, Paragraph } = Typography;

// API 端点配置（使用翻译 key）
const getApiEndpoints = (t) => ({
  chat: {
    title: t('对话补全'),
    method: 'POST',
    path: '/v1/chat/completions',
    description: t('创建对话补全请求，支持流式和非流式响应'),
    requestBody: {
      model: 'gpt-4o',
      messages: [
        { role: 'system', content: 'You are a helpful assistant.' },
        { role: 'user', content: 'Hello!' }
      ],
      stream: false,
      temperature: 0.7,
      max_tokens: 2048
    },
    response: {
      id: 'chatcmpl-xxx',
      object: 'chat.completion',
      created: 1699999999,
      model: 'gpt-4o',
      choices: [
        {
          index: 0,
          message: {
            role: 'assistant',
            content: 'Hello! How can I help you today?'
          },
          finish_reason: 'stop'
        }
      ],
      usage: {
        prompt_tokens: 20,
        completion_tokens: 10,
        total_tokens: 30
      }
    }
  },
  completions: {
    title: t('文本补全'),
    method: 'POST',
    path: '/v1/completions',
    description: t('创建文本补全请求'),
    requestBody: {
      model: 'gpt-3.5-turbo-instruct',
      prompt: 'Once upon a time',
      max_tokens: 100,
      temperature: 0.7
    },
    response: {
      id: 'cmpl-xxx',
      object: 'text_completion',
      created: 1699999999,
      model: 'gpt-3.5-turbo-instruct',
      choices: [
        {
          text: ', there was a brave knight who...',
          index: 0,
          finish_reason: 'length'
        }
      ],
      usage: {
        prompt_tokens: 5,
        completion_tokens: 100,
        total_tokens: 105
      }
    }
  },
  embeddings: {
    title: t('文本嵌入'),
    method: 'POST',
    path: '/v1/embeddings',
    description: t('创建文本嵌入向量'),
    requestBody: {
      model: 'text-embedding-3-small',
      input: 'The quick brown fox jumps over the lazy dog'
    },
    response: {
      object: 'list',
      data: [
        {
          object: 'embedding',
          index: 0,
          embedding: [0.0023064255, -0.009327292, '...']
        }
      ],
      model: 'text-embedding-3-small',
      usage: {
        prompt_tokens: 9,
        total_tokens: 9
      }
    }
  },
  models: {
    title: t('模型列表'),
    method: 'GET',
    path: '/v1/models',
    description: t('获取可用的模型列表'),
    requestBody: null,
    response: {
      object: 'list',
      data: [
        {
          id: 'gpt-4o',
          object: 'model',
          created: 1699999999,
          owned_by: 'openai'
        },
        {
          id: 'gpt-4-turbo',
          object: 'model',
          created: 1699999999,
          owned_by: 'openai'
        }
      ]
    }
  },
  images: {
    title: t('图像生成'),
    method: 'POST',
    path: '/v1/images/generations',
    description: t('根据提示生成图像'),
    requestBody: {
      model: 'dall-e-3',
      prompt: 'A beautiful sunset over the ocean',
      n: 1,
      size: '1024x1024'
    },
    response: {
      created: 1699999999,
      data: [
        {
          url: 'https://example.com/image.png',
          revised_prompt: 'A beautiful sunset over the ocean with vibrant colors'
        }
      ]
    }
  }
});

// 代码示例模板
const CODE_TEMPLATES = {
  curl: (endpoint, baseUrl) => `curl -X ${endpoint.method} \\
  ${baseUrl}${endpoint.path} \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
${endpoint.requestBody ? `  -d '${JSON.stringify(endpoint.requestBody, null, 2)}'` : ''}`,

  python: (endpoint, baseUrl) => `import requests

url = "${baseUrl}${endpoint.path}"
headers = {
    "Content-Type": "application/json",
    "Authorization": "Bearer YOUR_API_KEY"
}
${endpoint.requestBody ? `data = ${JSON.stringify(endpoint.requestBody, null, 4)}

response = requests.${endpoint.method.toLowerCase()}(url, headers=headers, json=data)` : `response = requests.${endpoint.method.toLowerCase()}(url, headers=headers)`}
print(response.json())`,

  javascript: (endpoint, baseUrl) => `const url = '${baseUrl}${endpoint.path}';
const headers = {
    'Content-Type': 'application/json',
    'Authorization': 'Bearer YOUR_API_KEY'
};
${endpoint.requestBody ? `const data = ${JSON.stringify(endpoint.requestBody, null, 4)};

fetch(url, {
    method: '${endpoint.method}',
    headers: headers,
    body: JSON.stringify(data)
})` : `fetch(url, {
    method: '${endpoint.method}',
    headers: headers
})`}
.then(response => response.json())
.then(data => console.log(data))
.catch(error => console.error('Error:', error));`,

  go: (endpoint, baseUrl) => `package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

func main() {
    url := "${baseUrl}${endpoint.path}"
    ${endpoint.requestBody ? `data := map[string]interface{}{}
    jsonData, _ := json.Marshal(data)
    req, _ := http.NewRequest("${endpoint.method}", url, bytes.NewBuffer(jsonData))` : `req, _ := http.NewRequest("${endpoint.method}", url, nil)`}
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer YOUR_API_KEY")
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    
    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    fmt.Printf("%+v\\n", result)
}`
};

const ApiDocs = () => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [activeTab, setActiveTab] = useState('chat');
  const [copied, setCopied] = useState(false);
  const [codeLang, setCodeLang] = useState('curl');

  // 获取服务器地址
  const baseUrl = window.location.origin;
  const systemName = getSystemName();

  // 获取 API 端点配置（带翻译）
  const API_ENDPOINTS = useMemo(() => getApiEndpoints(t), [t]);

  const handleCopy = (text) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    showSuccess(t('已复制'));
    setTimeout(() => setCopied(false), 2000);
  };

  const renderEndpointDoc = (key, endpoint) => {
    const codeExample = CODE_TEMPLATES[codeLang](endpoint, baseUrl);

    return (
      <div key={key} className="space-y-6">
        {/* API 基本信息 */}
        <Card className="bg-semi-color-bg-1">
          <div className="flex items-center gap-3 mb-4">
            <span className={`px-3 py-1 rounded text-sm font-medium ${
              endpoint.method === 'GET' ? 'bg-green-100 text-green-700' :
              endpoint.method === 'POST' ? 'bg-blue-100 text-blue-700' :
              'bg-gray-100 text-gray-700'
            }`}>
              {endpoint.method}
            </span>
            <code className="text-sm bg-semi-color-bg-2 px-3 py-1 rounded">
              {endpoint.path}
            </code>
          </div>
          <Paragraph className="text-semi-color-text-1">
            {endpoint.description}
          </Paragraph>
        </Card>

        {/* 请求示例代码 */}
        <div>
          <div className="flex items-center justify-between mb-3">
            <Title heading={5}>{t('请求示例')}</Title>
            <div className="flex gap-2">
              {Object.keys(CODE_TEMPLATES).map((lang) => (
                <Button
                  key={lang}
                  size="small"
                  type={codeLang === lang ? 'primary' : 'tertiary'}
                  onClick={() => setCodeLang(lang)}
                >
                  {lang.toUpperCase()}
                </Button>
              ))}
            </div>
          </div>
          <Card className="bg-semi-color-bg-2 relative">
            <Tooltip content={copied ? t('已复制') : t('复制')}>
              <Button
                icon={copied ? <span>✓</span> : <IconCopy />}
                size="small"
                className="absolute top-2 right-2"
                onClick={() => handleCopy(codeExample)}
              />
            </Tooltip>
            <pre className="text-sm overflow-x-auto p-2">
              <code>{codeExample}</code>
            </pre>
          </Card>
        </div>

        {/* 请求参数 */}
        {endpoint.requestBody && (
          <div>
            <Title heading={5} className="mb-3">{t('请求参数')}</Title>
            <Card className="bg-semi-color-bg-1">
              <pre className="text-sm overflow-x-auto">
                <code>{JSON.stringify(endpoint.requestBody, null, 2)}</code>
              </pre>
            </Card>
          </div>
        )}

        {/* 返回响应 */}
        <div>
          <Title heading={5} className="mb-3">{t('返回响应')}</Title>
          <Card className="bg-semi-color-bg-1">
            <pre className="text-sm overflow-x-auto">
              <code>{JSON.stringify(endpoint.response, null, 2)}</code>
            </pre>
          </Card>
        </div>
      </div>
    );
  };

  return (
    <div className="min-h-screen bg-semi-color-bg-0">
      {/* 头部 */}
      <div className="bg-semi-color-bg-1 border-b border-semi-color-border">
        <div className="max-w-6xl mx-auto px-4 py-8">
          <Title heading={2} className="mb-4">{t('API 文档')}</Title>
          <Paragraph className="text-semi-color-text-1 text-lg">
            {t('欢迎使用 {{systemName}} API，本文档将帮助您快速接入和使用我们的服务。', { systemName })}
          </Paragraph>
          
          {/* 基础信息 */}
          <Card className="mt-6 bg-semi-color-bg-2">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <Text className="text-semi-color-text-2">{t('基础 URL')}</Text>
                <div className="flex items-center gap-2 mt-1">
                  <code className="bg-semi-color-bg-1 px-3 py-1 rounded text-sm">
                    {baseUrl}/v1
                  </code>
                  <Tooltip content={copied ? t('已复制') : t('复制')}>
                    <Button
                      icon={copied ? <span>✓</span> : <IconCopy />}
                      size="small"
                      type="tertiary"
                      onClick={() => handleCopy(`${baseUrl}/v1`)}
                    />
                  </Tooltip>
                </div>
              </div>
              <div>
                <Text className="text-semi-color-text-2">{t('认证方式')}</Text>
                <div className="mt-1">
                  <code className="bg-semi-color-bg-1 px-3 py-1 rounded text-sm">
                    Authorization: Bearer {'{YOUR_API_KEY}'}
                  </code>
                </div>
              </div>
            </div>
          </Card>
        </div>
      </div>

      {/* 文档内容 */}
      <div className="max-w-6xl mx-auto px-4 py-8">
        <Tabs
          type="line"
          activeKey={activeTab}
          onChange={setActiveTab}
          tabPosition={isMobile ? 'top' : 'left'}
          className="api-docs-tabs"
          lazyRender
        >
          {Object.entries(API_ENDPOINTS).map(([key, endpoint]) => (
            <TabPane
              key={key}
              itemKey={key}
              tab={endpoint.title}
            >
              {renderEndpointDoc(key, endpoint)}
            </TabPane>
          ))}
        </Tabs>
      </div>

      {/* 底部说明 */}
      <div className="max-w-6xl mx-auto px-4 pb-8">
        <Card className="bg-semi-color-bg-1">
          <Title heading={5} className="mb-3">{t('注意事项')}</Title>
          <ul className="list-disc list-inside space-y-2 text-semi-color-text-1">
            <li>{t('所有 API 请求都需要在 Header 中携带 Authorization: Bearer {API_KEY}')}</li>
            <li>{t('API Key 可以在控制台 - API 令牌页面创建')}</li>
            <li>{t('流式响应需要设置 stream: true 并正确处理 SSE 格式')}</li>
            <li>{t('请求频率限制请参考控制台中的速率限制设置')}</li>
          </ul>
        </Card>
      </div>
    </div>
  );
};

export default ApiDocs;
