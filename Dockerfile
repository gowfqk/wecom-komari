FROM golang:1.21-alpine AS builder

WORKDIR /build

# 安装依赖
RUN apk add --no-cache git

# 复制源代码
COPY . .

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o wecom-komari .

# 运行阶段
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/wecom-komari .

# 暴露端口
EXPOSE 8080

# 运行
CMD ["./wecom-komari"]
