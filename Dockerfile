# 用于 GitHub Action 的 Dockerfile
# 这个 Dockerfile 假设编译产物已经在外部构建完成

FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata

# 设置时区为上海
ENV TZ=Asia/Shanghai

WORKDIR /app

ARG TARGETARCH

# 从外部编译的产物复制文件
COPY ./bin/uart_sms_forwarder-linux-${TARGETARCH} ./uart_sms_forwarder

# 创建必要的目录
RUN mkdir -p /app/data /app/logs && \
    chmod +x /app/uart_sms_forwarder

# 设置数据卷
VOLUME ["/app/data"]

# 暴露端口
EXPOSE 8080

# 启动服务
ENTRYPOINT ["./uart_sms_forwarder"]
