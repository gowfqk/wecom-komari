# wecom-komari

企业微信 + Telegram + Komari 监控集成

基于 [wecom-nezha](https://github.com/gowfqk/wecom-nezha) 项目架构，适配 [Komari](https://github.com/komari-monitor/komari) 监控面板。

## 功能

- ✅ 企业微信消息推送 & 回调机器人
- ✅ Telegram Bot 交互（内联键盘）
- ✅ 节点状态 / 列表 / 详情 / 离线节点查询
- ✅ Ping 检测（ICMP 延迟 + 丢包率）
- ✅ 性能排行（CPU / 内存 / 上传 / 下载，切换按钮）
- ✅ 负载历史图表（1h / 6h / 24h / 7d / 30d）
- ✅ 分组查看 & 分组节点列表
- ✅ 站点信息 & 系统版本查询
- ✅ 健康评分系统
- ✅ Komari 通知转发脚本（离线/上线/告警 → TG + 企业微信）

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

> *`KOMARI_API_KEY` 与 `KOMARI_USERNAME`/`KOMARI_PASSWORD` 二选一

### Telegram Bot 命令

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

点击节点名称可查看详细信息，支持内联键盘操作。

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

## Komari 通知转发

`komari-notify.js` 可粘贴到 Komari 后台「设置 → 通知」中，将离线/上线/告警等事件转发到 Telegram + 企业微信。

使用前修改脚本顶部 4 个配置项：
- `WECOM_KOMARI_URL` — wecom-komari 服务地址
- `SENDKEY` — 认证密钥
- `TG_CHAT_ID` — Telegram Chat ID
- `WECOM_USER` — 企业微信接收人

支持事件：🔴 离线 / 🟢 上线 / ⚠️ 告警 / ⏰ 续费 / 🚨 到期 / 🧪 测试

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
