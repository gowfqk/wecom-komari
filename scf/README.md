# wecom-komari (腾讯云函数 SCF 版本)

基于腾讯云函数 (SCF) 的无服务器版本，支持消息转发到 Telegram + 企业微信。

## 功能

- ✅ 通用 Webhook 端点 - 接收消息转发
- ✅ Telegram Bot - 命令处理 + 消息转发
- ✅ 企业微信消息推送
- ✅ Token 内存缓存 (函数实例内)
- ✅ API Gateway 触发器

## 部署

### 1. 安装 Serverless CLI

```bash
npm install -g serverless
```

### 2. 创建 serverless.yml

```yaml
component: scf
name: wecom-komari
app: wecom-komari-app

inputs:
  name: wecom-komari
  region: ap-guangzhou
  runtime: Go1
  handler: main
  src:
    src: ./scf
    exclude:
      - .git
      - .env
  environment:
    variables:
      SENDKEY: "your_sendkey"
      WECOM_CID: ""
      WECOM_SECRET: ""
      WECOM_AID: ""
      WECOM_TOUID: "@all"
      TELEGRAM_BOT_TOKEN: ""
      TELEGRAM_ALLOWED_USERS: ""
  events:
    - apigw:
        parameters:
          protocols:
            - https
          endpoints:
            - path: /webhook
              method: ANY
            - path: /telegram/webhook
              method: ANY
            - path: /healthz
              method: GET
```

### 3. 部署

```bash
cd scf
serverless deploy
```

或使用腾讯云控制台：

1. 登录 [腾讯云函数控制台](https://console.cloud.tencent.com/scf)
2. 创建函数 → 选择 "Go" 运行时
3. 上传 `scf/main.go` 编译后的二进制
4. 配置环境变量
5. 创建 API Gateway 触发器

### 4. 编译

```bash
cd scf
GOOS=linux GOARCH=amd64 go build -o main main.go
zip main.zip main
```

## API

### Webhook 端点

```bash
# 健康检查
curl https://xxx.ap-guangzhou.tencentcloudapi.com/webhook

# 发送消息
curl -X POST "https://xxx.ap-guangzhou.tencentcloudapi.com/webhook?sendkey=your_key" \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello from SCF!"}'
```

### Telegram Bot Webhook

设置 Telegram Webhook：

```bash
curl -X POST "https://api.telegram.org/bot<YOUR_TOKEN>/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://xxx.ap-guangzhou.tencentcloudapi.com/telegram/webhook"}'
```

## 本地测试

```bash
cd scf
go run main.go
# 使用 curl 或 Postman 发送 API Gateway 格式的请求
```

## 与原版区别

| 特性 | Go 版本 | SCF 版本 |
|------|---------|----------|
| 部署 | 服务器/Docker | 腾讯云函数 |
| 冷启动 | 无 | ~100-300ms |
| 状态 | 内存 (sync.Map) | 实例内存 (会丢失) |
| 成本 | 服务器费用 | 按调用次数计费 |
| 扩展 | 手动 | 自动 |
| 并发 | 单进程 | 多实例并行 |

## 注意事项

1. **Token 缓存**：函数实例内存中的 token 缓存在冷启动后会丢失，每次冷启动会重新获取
2. **无状态**：SCF 是无状态的，不要依赖内存存储持久数据
3. **超时**：默认超时 3 秒，建议设置为 30 秒以处理企业微信 API 调用
4. **并发**：每个请求可能在不同实例处理，token 缓存不共享
