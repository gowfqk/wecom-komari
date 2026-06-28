# wecom-komari

企业微信 + Telegram + Komari 监控集成 — 多平台部署方案

基于 [wecom-nezha](https://github.com/gowfqk/wecom-nezha) 项目架构，适配 [Komari](https://github.com/komari-monitor/komari) 监控面板。

## 功能

### 监控查询
- ✅ 企业微信消息推送 & 回调机器人
- ✅ Telegram Bot 交互（内联键盘、编辑表单）
- ✅ 节点状态 / 列表 / 详情 / 离线节点查询
- ✅ Ping 检测（ICMP 延迟 + 丢包率）
- ✅ 性能排行（CPU / 内存 / 上传 / 下载，切换按钮）
- ✅ 负载历史图表（1h / 6h / 24h / 7d / 30d）
- ✅ 分组查看 & 分组节点列表
- ✅ 站点信息 & 系统版本查询
- ✅ 健康评分系统

### 管理功能（Telegram Bot）
- ✅ 客户端管理：添加 / 编辑 / 删除 / 列表 / Token 查看
- ✅ `/new <名称>` 快速创建客户端（带一键安装命令）
- ✅ 通知管理：离线通知 / 负载告警 / 流量报告
- ✅ Ping 任务管理：添加 / 编辑 / 删除
- ✅ 远程任务：执行 / 历史 / 详情
- ✅ 审计日志查看
- ✅ 系统设置编辑
- ✅ 记录管理：清空历史记录

### 交互体验
- ✅ 中文参数输入（名称=xxx，自动映射到英文 API 参数）
- ✅ 编辑表单内联键盘（字段按钮，点击填入编辑命令）
- ✅ 原地编辑消息（editMessageText，不刷屏）
- ✅ 取消/退出机制（输入 `取消`/`cancel`/`退出`/`q` 退出当前操作）
- ✅ 命令优先（任何状态下输入 `/help` 等命令立即响应）
- ✅ 多平台安装命令（Linux / Windows / macOS）

### 通知转发
- ✅ Komari 通知脚本 `komari-notify.js`（离线/上线/告警 → TG + 企业微信）

## 部署方式

本仓库提供三种部署方式：

| 方式 | 部署位置 | 成本 | 功能 | 推荐场景 |
|------|---------|------|------|---------|
| Go 服务 | 自有服务器 / Docker / Fly.io | 服务器费用 | 完整（含企业微信回调） | 有服务器的用户 |
| Cloudflare Workers | CF 边缘网络 | 免费额度 10万请求/天 | 完整 Bot 功能 | 不想管服务器的用户 |

---

### 方式一：Go 服务部署（推荐）

#### Docker 部署

```bash
git clone https://github.com/gowfqk/wecom-komari.git
cd wecom-komari
cp .env.example .env
vi .env                    # 配置环境变量
docker-compose up -d
```

#### 直接运行

```bash
export KOMARI_URL=https://your-komari.example.com
export KOMARI_API_KEY=your_api_key
go run .
```

#### 构建二进制

```bash
go build -o wecom-komari .
./wecom-komari
```

#### 环境变量

| 变量名 | 说明 | 必填 |
|--------|------|------|
| `SENDKEY` | API 认证密钥 | 否 |
| `WECOM_CID` | 企业微信公司ID | 否 |
| `WECOM_SECRET` | 企业微信应用Secret | 否 |
| `WECOM_AID` | 企业微信应用ID | 否 |
| `WECOM_TOUID` | 消息接收人 | 否 |
| `KOMARI_URL` | Komari 面板地址 | 是 |
| `KOMARI_USERNAME` | Komari 用户名 | 是* |
| `KOMARI_PASSWORD` | Komari 密码 | 是* |
| `KOMARI_API_KEY` | Komari API Key | 是* |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token | 否 |
| `TELEGRAM_WEBHOOK_SECRET` | Telegram Webhook 密钥 | 否 |
| `TELEGRAM_ALLOWED_USERS` | 允许的用户ID列表 | 否 |
| `GITHUB_PROXY` | GitHub 下载代理（如 `https://ghproxy.com/`） | 否 |

> \* `KOMARI_API_KEY` 与 `KOMARI_USERNAME`/`KOMARI_PASSWORD` 二选一

---

### 方式二：Cloudflare Workers 部署

无服务器版本，部署在 Cloudflare 边缘网络。

```bash
cd cf-workers

# 安装依赖
npm install

# 配置密钥
npx wrangler secret put TELEGRAM_BOT_TOKEN
npx wrangler secret put TELEGRAM_WEBHOOK_SECRET
npx wrangler secret put SENDKEY
npx wrangler secret put KOMARI_API_KEY

# 部署
npm run deploy
```

设置 Telegram Webhook：

```bash
curl -X POST "https://api.telegram.org/bot<YOUR_TOKEN>/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-worker.workers.dev/telegram/webhook",
    "secret_token": "your_webhook_secret",
    "allowed_updates": ["message","callback_query"]
  }'
```

完整环境变量见 [`cf-workers/README.md`](cf-workers/README.md)。

---

## Telegram Bot 命令

### 查询命令

| 命令 | 说明 |
|------|------|
| `/status` | 服务器状态概览（在线/离线/总数） |
| `/list` | 所有节点列表 |
| `/offline` | 离线节点列表 |
| `/ping` | Ping 所有节点（延迟 + 丢包率） |
| `/rank` | 性能排行（CPU/内存/网络切换） |
| `/info` | 站点信息 & Komari 版本 |
| `/group` | 分组列表 & 节点分布 |
| `/history <uuid>` | 节点负载历史图表 |
| `/node <uuid>` | 节点详细信息 |
| `/help` | 帮助信息 |

### 管理命令

| 命令 | 说明 |
|------|------|
| `/admin` | 管理员面板 |
| `/new <名称>` | 快速创建客户端 |
| `/notify` | 通知管理 |
| `/ping_admin` | Ping 任务管理 |
| `/task` | 远程任务 |
| `/logs` | 审计日志 |
| `/settings` | 系统设置 |

### 快捷操作

- 直接输入节点名称 → 查看节点详情
- 点击节点名 → 查看节点详情（含编辑 / Token / 历史 / 刷新按钮）
- 点击编辑按钮 → 原地编辑表单（6个字段：名称/分组/区域/权重/公开备注/私有备注）
- 输入 `取消` / `cancel` / `退出` / `q` → 退出当前操作
- 任何状态下输入 `/help` 等命令立即响应

### 企业微信命令

| 命令 | 说明 |
|------|------|
| `状态` / `/status` | 服务器状态概览 |
| `列表` / `/list` | 所有节点列表 |
| `离线` / `/offline` | 离线节点 |
| `排名` / `/rank` | 性能排行 |
| `分组` / `/group` | 分组信息 |
| `/admin` / `管理` | 管理面板 |
| 直接输入节点名称 | 查看节点详情 |

## 通知转发

### Webhook 端点

通用消息转发接口，支持任意程序调用。

```bash
curl -X POST "http://localhost:8080/webhook?sendkey=your_key" \
  -H "Content-Type: application/json" \
  -d '{"text": "服务器宕机了！"}'
```

**请求方式：**
- `GET` — 连通性检查，返回 `{"status":"ok"}`
- `POST` — 发送消息

**消息字段（任选其一）：** `text` / `msg` / `content`

**认证（任选其一）：** JSON body 或 Query 参数：`sendkey` / `token`

**转发目标（环境变量配置）：**
- Telegram：`TELEGRAM_BOT_TOKEN` + `TELEGRAM_ALLOWED_USERS`
- 企业微信：`WECOM_CID` + `WECOM_SECRET` + `WECOM_TOUID`

### Komari 集成

在 Komari 后台「设置 → 通知」中配置 Webhook URL：

```
http://your-server:8080/webhook?sendkey=your_key
```

或使用 `komari-notify.js` 脚本。

### 支持的事件类型

`Offline` / `Online` / `Alert` / `Renew` / `Expire` / `Test`

## API 接口

### 企业微信推送

```bash
curl -X POST http://localhost:8080/wecomchan \
  -H "Content-Type: application/json" \
  -d '{"sendkey": "your_key", "msg": "Hello World"}'
```

### Telegram 推送

```bash
curl -X POST http://localhost:8080/telegram/push \
  -H "Content-Type: application/json" \
  -d '{"sendkey": "your_key", "chat_id": 123456, "text": "Hello World"}'
```

### 健康检查

```bash
curl http://localhost:8080/healthz
```

## 项目结构

```
wecom-komari/
├── main.go                 # Go 服务入口
├── handlers.go             # Telegram Bot 回调处理
├── komari.go               # Komari API 交互
├── types.go                # 数据结构
├── webhook.go              # Webhook 处理
├── komari-notify.js        # Komari 通知脚本
├── Dockerfile              # Docker 构建
├── docker-compose.yml      # Docker Compose
├── fly.toml                # Fly.io 配置
├── cf-workers/             # Cloudflare Workers 版本
│   ├── src/index.js        # Worker 代码
│   ├── wrangler.toml       # Wrangler 配置
│   └── package.json
```

## 各版本区别

| 特性 | Go 版本 | CF Workers 版本 | SCF 版本 |
|------|---------|-----------------|----------|
| 部署 | 自有服务器 / Docker | CF 边缘网络 | 腾讯云 |
| 冷启动 | 无 | ~5ms | ~100ms |
| 状态存储 | 内存 (sync.Map) | KV 存储 | 无（无状态） |
| 成本 | 服务器费用 | 免费额度 | 免费额度 |
| 扩展性 | 手动 | 自动 | 自动 |
| 企业微信回调 | ✅ | ❌ | ❌ |
| 本地开发 | `go run .` | `wrangler dev` | `sls invoke local` |

## 与 wecom-nezha 的区别

| 功能 | wecom-nezha | wecom-komari |
|------|-------------|--------------|
| 监控面板 | 哪吒监控 | Komari |
| 认证方式 | JWT Token | Session/API Key |
| 数据结构 | 扁平化 | 嵌套(实时)/扁平(历史) |
| 节点标识 | ID | UUID |
| API 路径 | `/api/v1/` | `/api/` |
| 通知管理 | REST API | JS 脚本（后台配置） |

## 致谢

- [wecom-nezha](https://github.com/gowfqk/wecom-nezha) — 原始项目架构
- [Komari](https://github.com/komari-monitor/komari) — 监控面板

## 许可证

MIT License