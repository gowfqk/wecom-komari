# wecom-komari

企业微信 + Telegram + Komari 监控集成

基于 [wecom-nezha](https://github.com/gowfqk/wecom-nezha) 项目架构，适配 [Komari](https://github.com/komari-monitor/komari) 监控面板。

## 功能

### 监控查询
- ✅ 企业微信消息推送 & 回调机器人
- ✅ Telegram Bot 交互（内联键盘）
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
- ✅ 中文参数输入（名称=xxx 区域=xxx，自动映射到英文 API 参数）
- ✅ 取消/退出机制（输入 `取消`/`cancel`/`退出`/`q` 退出当前操作）
- ✅ 命令优先（任何状态下输入 `/help` 等命令立即响应）
- ✅ 多平台安装命令（Linux / Windows / macOS）

### 通知转发
- ✅ Komari 通知脚本 `komari-notify.js`（离线/上线/告警 → TG + 企业微信）

## 快速开始

### Docker 部署 (推荐)

```bash
git clone https://github.com/gowfqk/wecom-komari.git
cd wecom-komari
cp .env.example .env
vi .env
docker-compose up -d
```

### 环境变量

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

> *`KOMARI_API_KEY` 与 `KOMARI_USERNAME`/`KOMARI_PASSWORD` 二选一

### Telegram Bot 命令

#### 查询命令

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

#### 管理命令

| 命令 | 说明 |
|------|------|
| `/admin` | 管理员面板 |
| `/new <名称>` | 快速创建客户端 |
| `/notify` | 通知管理 |
| `/ping_admin` | Ping 任务管理 |
| `/task` | 远程任务 |
| `/logs` | 审计日志 |
| `/settings` | 系统设置 |

#### 快捷操作

- 直接输入节点名称 → 查看节点详情
- 输入 `取消` / `cancel` / `退出` / `q` → 退出当前操作
- 任何状态下输入 `/help` 等命令立即响应

### 企业微信命令

- `状态` / `/status` - 服务器状态概览
- `列表` / `/list` - 所有节点列表
- `离线` / `/offline` - 离线节点
- `排名` / `/rank` - 性能排行
- `分组` / `/group` - 分组信息
- `/admin` / `管理` - 管理面板
- `/admin_clients` / `客户端管理` - 客户端管理
- `/admin_notify` / `通知管理` - 通知管理
- `/admin_ping` / `Ping管理` - Ping任务
- `/admin_tasks` / `远程任务` - 远程任务
- `/admin_logs` / `日志` - 审计日志
- `/admin_sessions` / `会话` - 会话管理
- `/admin_settings` / `设置` - 系统设置
- `/admin_clear` / `清空记录` - 清空记录
- 直接输入节点名称 - 查看节点详情

## 通知转发

### Webhook 端点

通用消息转发接口，支持任意程序调用。

```bash
curl -X POST "http://localhost:8080/webhook?sendkey=your_key" \
  -H "Content-Type: application/json" \
  -d '{"text": "服务器宕机了！"}'
```

**请求方式：**
- `GET` - 连通性检查，返回 `{"status":"ok"}`
- `POST` - 发送消息

**消息字段（任选其一）：**
- `text` - 推荐
- `msg` - 兼容
- `content` - 兼容

**认证（任选其一）：**
- JSON body: `{"sendkey": "xxx"}` 或 `{"token": "xxx"}`
- Query 参数: `?sendkey=xxx` 或 `?token=xxx`

**响应：**
```json
{"status": "ok", "sent": true}
```

**转发目标（环境变量配置）：**
- Telegram：`TELEGRAM_BOT_TOKEN` + `TELEGRAM_ALLOWED_USERS`
- 企业微信：`WECOM_CID` + `WECOM_SECRET` + `WECOM_TOUID`

### Komari 集成

在 Komari 后台「设置 → 通知」中配置 Webhook URL：

```
http://your-server:8080/webhook?sendkey=your_key
```

或使用 `komari-notify.js` 脚本（见仓库）。

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

### Komari Webhook

接收 Komari 事件通知并转发到 Telegram + 企业微信。

```bash
curl -X POST "http://localhost:8080/webhook?sendkey=your_key" \
  -H "Content-Type: application/json" \
  -d '{
    "event": "Offline",
    "nodes": [{"name": "Server-1", "ip": "1.2.3.4", "region": "CN"}],
    "time": "2024-01-01 12:00:00",
    "message": "节点离线"
  }'
```

**参数说明：**
- `sendkey` / `token` - 认证密钥（JSON body 或 query 参数）

**支持的事件类型：**
- `Offline` / `Online` / `Alert` / `Renew` / `Expire` / `Test`

**JSON 字段兼容：**
- 事件类型：`event` / `type` / `name`
- 节点列表：`nodes` / `clients`
- 时间：`time` / `timestamp`
- 消息：`message` / `msg`

**转发目标（环境变量配置）：**
- Telegram：`TELEGRAM_BOT_TOKEN` + `TELEGRAM_ALLOWED_USERS`
- 企业微信：`WECOM_CID` + `WECOM_SECRET` + `WECOM_TOUID`

### 健康检查

```bash
curl http://localhost:8080/healthz
```

## 开发

```bash
# 本地运行
export KOMARI_URL=https://your-komari.example.com
export KOMARI_API_KEY=your_api_key
go run .

# 构建
go build -o wecom-komari .
```

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

- [wecom-nezha](https://github.com/gowfqk/wecom-nezha) - 原始项目架构
- [Komari](https://github.com/komari-monitor/komari) - 监控面板

## 许可证

MIT License
