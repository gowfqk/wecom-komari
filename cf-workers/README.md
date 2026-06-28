# wecom-komari (Cloudflare Workers 版本)

基于 Cloudflare Workers 的无服务器版本，支持消息转发到 Telegram + 企业微信。

## 功能

- ✅ 通用 Webhook 端点 - 接收消息转发
- ✅ Telegram Push API - 直接推送到指定 Chat
- ✅ WeCom 消息发送 API
- ✅ Telegram Bot - 命令处理 + 内联键盘
- ✅ Webhook Secret 验证
- ✅ 企业微信消息推送
- ✅ Token 自动缓存 (KV)
- ✅ CORS 支持

## 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/webhook` | GET | 健康检查 |
| `/webhook` | POST | 消息转发到 TG + WeCom |
| `/telegram/push` | POST | 直接推送 Telegram |
| `/telegram/webhook` | POST | Telegram Bot Webhook |
| `/wecomchan` | POST | 企业微信消息发送 |
| `/healthz` | GET | 健康检查 |

## 部署

### 1. 安装 Wrangler CLI

```bash
npm install -g wrangler
wrangler login
```

### 2. 配置环境变量

编辑 `wrangler.toml` 或使用 `wrangler secret`：

```bash
wrangler secret put SENDKEY
wrangler secret put WECOM_SECRET
wrangler secret put TELEGRAM_BOT_TOKEN
wrangler secret put TELEGRAM_WEBHOOK_SECRET
# ... 其他密钥
```

### 3. 创建 KV 命名空间 (可选，用于 Token 缓存)

```bash
wrangler kv namespace create KV
# 输出: { binding = "KV", id = "xxxxxxxx" }
```

将 ID 填入 `wrangler.toml`：

```toml
[[kv_namespaces]]
binding = "KV"
id = "your_kv_namespace_id"
```

### 4. 部署

```bash
cd cf-workers
npm run deploy
```

## API

### Webhook 端点

```bash
# 健康检查
curl https://your-worker.workers.dev/webhook

# 发送消息
curl -X POST "https://your-worker.workers.dev/webhook?sendkey=your_key" \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello from CF Workers!"}'
```

### Telegram Push

```bash
curl -X POST "https://your-worker.workers.dev/telegram/push" \
  -H "Content-Type: application/json" \
  -d '{"sendkey": "your_key", "chat_id": 123456, "text": "Hello!"}'
```

### WeCom Channel

```bash
curl -X POST "https://your-worker.workers.dev/wecomchan" \
  -H "Content-Type: application/json" \
  -d '{"sendkey": "your_key", "msg": "Hello!"}'
```

### Telegram Bot Webhook

设置 Telegram Webhook：

```bash
curl -X POST "https://api.telegram.org/bot<YOUR_TOKEN>/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://your-worker.workers.dev/telegram/webhook", "secret_token": "your_webhook_secret"}'
```

## 本地开发

```bash
cd cf-workers
npm run dev
# 或
wrangler dev
```

## 环境变量

| 变量名 | 说明 | 必填 |
|--------|------|------|
| `SENDKEY` | API 认证密钥 | 是 |
| `WECOM_CID` | 企业微信公司ID | 否 |
| `WECOM_SECRET` | 企业微信应用Secret | 否 |
| `WECOM_AID` | 企业微信应用ID | 否 |
| `WECOM_TOUID` | 消息接收人 | 否 |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | 否 |
| `TELEGRAM_WEBHOOK_SECRET` | Telegram Webhook 密钥 | 否 |
| `TELEGRAM_ALLOWED_USERS` | 允许的用户ID列表 | 否 |
| `TELEGRAM_API_BASE` | Telegram API 地址 | 否 |

## 与原版区别

| 特性 | Go 版本 | CF Workers 版本 |
|------|---------|-----------------|
| 部署 | 服务器/Docker | Cloudflare 边缘网络 |
| 冷启动 | 无 | ~5ms |
| 状态 | 内存 (sync.Map) | KV 存储 |
| 成本 | 服务器费用 | 免费额度 10万请求/天 |
| 扩展 | 手动 | 自动 |
| Komari 集成 | ✅ 完整 | ❌ 无 (纯消息转发) |
| 企业微信回调 | ✅ | ❌ |
