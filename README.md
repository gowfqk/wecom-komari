# wecom-komari

企业微信 + Telegram + Komari 监控集成

基于 [wecom-nezha](https://github.com/gowfqk/wecom-nezha) 项目架构，适配 [Komari](https://github.com/komari-monitor/komari) 监控面板。

## 功能

- ✅ 企业微信消息推送
- ✅ 企业微信回调机器人
- ✅ Telegram Bot 交互
- ✅ 节点状态查询
- ✅ 节点列表展示
- ✅ 实时数据获取
- ✅ 历史记录查询
- ✅ 健康评分系统
- ✅ 内联键盘操作

## 快速开始

### Docker 部署 (推荐)

```bash
# 克隆项目
git clone https://github.com/gowfqk/wecom-komari.git
cd wecom-komari

# 创建环境变量文件
cp .env.example .env

# 编辑配置
vi .env

# 启动服务
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

- `/status` - 服务器状态概览
- `/list` - 所有节点列表
- `/help` - 帮助信息

### 企业微信命令

- `状态` 或 `/status` - 服务器状态概览
- `列表` 或 `/list` - 所有节点列表
- `帮助` 或 `/help` - 帮助信息
- 直接输入节点名称 - 查看节点详情

## API 接口

### 消息推送

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
# 安装依赖
go mod tidy

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

## 致谢

- [wecom-nezha](https://github.com/gowfqk/wecom-nezha) - 原始项目架构
- [Komari](https://github.com/komari-monitor/komari) - 监控面板
- [企业微信](https://work.weixin.qq.com/) - 企业通讯平台
- [Telegram](https://telegram.org/) - 即时通讯平台

## 许可证

MIT License
