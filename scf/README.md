# wecom-komari SCF (腾讯云函数版)

基于 [Cloudflare Workers 版](../cf-workers/) 移植的腾讯云函数（SCF）版本，支持 Telegram Bot 交互和企业微信消息推送。

## 功能

- 🤖 Telegram Bot 完整交互（查询节点状态、管理面板、内联按钮、编辑节点等）
- 📊 Komari 监控面板 API 集成
- 💬 企业微信消息推送（可选）
- 🔒 用户权限控制

## 前置条件

- [腾讯云账号](https://cloud.tencent.com/register)
- [Node.js 18+](https://nodejs.org/)
- Komari 面板地址及 API Key
- Telegram Bot Token（从 [@BotFather](https://t.me/BotFather) 获取）

## 部署步骤

### 1. 获取腾讯云密钥

1. 登录 [腾讯云控制台](https://console.cloud.tencent.com/cam/capi)
2. 进入 **访问管理** → **API 密钥管理**
3. 创建或获取 **SecretId** 和 **SecretKey**

### 2. 安装 Serverless Framework

```bash
npm install -g serverless
```

### 3. 配置腾讯云凭证

**方式一：环境变量（推荐）**

```bash
export TENCENT_APP_ID=你的AppID
export TENCENT_SECRET_ID=你的SecretId
export TENCENT_SECRET_KEY=你的SecretKey
```

（也可运行 `sls profile add` 交互式配置）

**方式二：交互式登录**

```bash
sls login
```

### 4. 配置环境变量

编辑 `serverless.yml`，填入实际的环境变量值：

```yaml
environment:
  variables:
    KOMARI_URL: "https://km.o0oo.cc"           # Komari 面板地址
    KOMARI_API_KEY: "your-api-key"              # Komari API Key
    TELEGRAM_BOT_TOKEN: "123456:ABC-DEF..."     # Telegram Bot Token
    TELEGRAM_WEBHOOK_SECRET: "your-secret"      # Webhook 验证密钥
    TELEGRAM_ALLOWED_USERS: "691245891"         # 允许的用户 ID（逗号分隔）
    # 以下为可选配置
    # KOMARI_USERNAME: "admin"
    # KOMARI_PASSWORD: "password"
    # WECOM_CID: "your-corp-id"
    # WECOM_SECRET: "your-secret"
    # WECOM_AID: "your-agent-id"
    # WECOM_TOUID: "@all"
```

### 5. 部署

```bash
cd scf/
sls deploy
```

部署完成后，终端会输出 API 网关访问地址，格式类似：
```
https://service-xxxx-xxxx.gz.apigw.tencentcs.com/release/
```

### 6. 设置 Telegram Webhook

将 API 网关地址设置为 Telegram Bot 的 Webhook：

```bash
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://<API_GATEWAY_URL>/telegram/webhook",
    "secret_token": "<YOUR_WEBHOOK_SECRET>"
  }'
```

### 7. 验证

```bash
# 检查健康状态
curl https://<API_GATEWAY_URL>/healthz

# 检查 Webhook 状态
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo"
```

## 常用命令

```bash
# 查看部署信息
sls info

# 查看日志
sls logs -f wecom-komari

# 本地调试
sls invoke local -f wecom-komari --data '{"httpMethod":"GET","path":"/healthz"}'

# 更新部署
sls deploy

# 删除部署
sls remove
```

## Telegram Bot 命令

| 命令 | 说明 |
|------|------|
| `/status` | 查看所有节点状态 |
| `/list` | 节点列表 |
| `/offline` | 离线节点 |
| `/rank` | CPU/内存/流量排行 |
| `/info <节点>` | 节点详情 |
| `/group` | 分组列表 |
| `/ping <节点>` | Ping 测试 |
| `/edit <节点> field=value` | 编辑节点信息 |
| `/admin` | 管理面板入口 |
| `/notify` | 通知设置 |
| `/task` | 任务管理 |
| `/logs` | 查看日志 |

## API 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/` 或 `/webhook` | GET | 健康检查 |
| `/` 或 `/webhook` | POST | 消息转发（Telegram + 企业微信） |
| `/telegram/push` | POST | 直接推送 Telegram 消息 |
| `/telegram/webhook` | POST | Telegram Bot Webhook 回调 |
| `/wecomchan` | POST | 企业微信消息发送 |
| `/healthz` | GET | 健康检查 |

## 注意事项

1. **冷启动**：SCF 默认有冷启动延迟，首次请求可能需要 3-5 秒
2. **超时时间**：默认 60 秒，可在 `serverless.yml` 中调整 `timeout`
3. **内存配置**：默认 128MB，如需处理大量并发可增至 256MB 或更高
4. **地域选择**：默认广州 `ap-guangzhou`，可改为其他地域以降低延迟
5. **费用**：腾讯云函数有免费额度（每月 40 万次调用、100 万 GBs），个人使用通常免费

## 与 CF Workers 版本的区别

| 特性 | CF Workers | SCF |
|------|-----------|-----|
| 运行环境 | Cloudflare Edge | 腾讯云 |
| 冷启动 | 无 | 3-5 秒 |
| 免费额度 | 10 万次/天 | 40 万次/月 |
| 自定义域名 | 支持 | 支持 |
| KV 存储 | 支持 (Workers KV) | 不支持（内存缓存） |

## License

MIT
