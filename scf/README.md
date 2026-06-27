# wecom-komari SCF (腾讯云函数版)

基于 Cloudflare Workers 版移植的腾讯云函数（SCF）版本，支持 Telegram Bot 交互和企业微信消息推送。

## 功能

- 🤖 Telegram Bot 交互（查询节点状态、管理面板、内联按钮等）
- 📊 Komari 监控面板 API 集成
- 💬 企业微信消息推送
- 🔒 用户权限控制

## 环境变量

| 变量名 | 说明 | 必填 |
|--------|------|------|
| `SENDKEY` | 消息推送鉴权密钥 | 否 |
| `KOMARI_URL` | Komari 面板地址 | 是 |
| `KOMARI_API_KEY` | Komari API Key | 否 |
| `KOMARI_USERNAME` | Komari 用户名（无API Key时必填） | 否 |
| `KOMARI_PASSWORD` | Komari 密码（无API Key时必填） | 否 |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | 是 |
| `TELEGRAM_WEBHOOK_SECRET` | Telegram Webhook 验证密钥 | 否 |
| `TELEGRAM_ALLOWED_USERS` | 允许使用的 Telegram 用户 ID（逗号分隔） | 否 |
| `TELEGRAM_API_BASE` | Telegram API 自定义域名（可选） | 否 |
| `WECOM_CID` | 企业微信 CorpID | 否 |
| `WECOM_SECRET` | 企业微信 Secret | 否 |
| `WECOM_AID` | 企业微信 AgentID | 否 |
| `WECOM_TOUID` | 企业微信消息接收人（默认 @all） | 否 |

## 部署步骤

### 1. 安装 Serverless Framework

```bash
npm install -g serverless
```

### 2. 配置环境变量

编辑 `serverless.yml` 中的 `environment.variables` 部分，填入实际的环境变量值。

### 3. 部署

```bash
sls deploy
```

部署完成后，会输出 API 网关的访问地址。

### 4. 设置 Telegram Webhook

将 API 网关地址设置为 Telegram Bot 的 Webhook：

```bash
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://<API_GATEWAY_URL>/telegram/webhook", "secret_token": "<YOUR_WEBHOOK_SECRET>"}'
```

### 5. 验证

访问 `https://<API_GATEWAY_URL>/healthz` 应返回 `{"status":"ok"}`。

## API 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/` 或 `/webhook` | GET | 健康检查 |
| `/` 或 `/webhook` | POST | 消息转发（Telegram + 企业微信） |
| `/telegram/push` | POST | 直接推送 Telegram 消息 |
| `/telegram/webhook` | POST | Telegram Bot Webhook 回调 |
| `/wecomchan` | POST | 企业微信消息发送 |
| `/healthz` | GET | 健康检查 |
