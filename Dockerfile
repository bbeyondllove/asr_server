# 阶段1：构建二进制文件
FROM golang:1.24-bullseye AS builder
WORKDIR /app
RUN sed -i 's|http://deb.debian.org/debian|http://mirrors.aliyun.com/debian|g' /etc/apt/sources.list
# 安装 gcc/g++ 工具链以支持 cgo
RUN apt-get update && apt-get install -y --no-install-recommends \
    libc++1 libc++abi1 \
    build-essential
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && go mod download
COPY . .
RUN go build -ldflags="-s -w" -o asr_server main.go

# 阶段2：最终运行时镜像（量化版本，自动下载模型）
FROM ubuntu:22.04 AS final
WORKDIR /app
RUN echo "deb http://mirrors.aliyun.com/ubuntu/ jammy main restricted universe multiverse" > /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-updates main restricted universe multiverse" >> /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-backports main restricted universe multiverse" >> /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-security main restricted universe multiverse" >> /etc/apt/sources.list
# 安装依赖和下载工具
RUN apt-get update && apt-get install -y --no-install-recommends \
    libc++1 libc++abi1 ca-certificates \
    wget git python3 python3-pip
# 安装 modelscope
RUN pip3 install --no-cache-dir modelscope -i https://mirrors.aliyun.com/pypi/simple/
COPY --from=builder /app/asr_server .
# 创建模型目录
RUN mkdir -p models/vad/silero_vad \
    && mkdir -p models/asr/Fun-ASR-Nano-2512-8bit \
    && mkdir -p models/speaker \
    && mkdir -p logs
# 复制配置和静态文件
COPY config.json ./config.json
COPY static ./static
COPY lib ./lib
# 复制本地已有的 VAD 模型
COPY models ./models
# 复制启动脚本并设置权限
COPY docker-entrypoint.sh ./
RUN chmod +x docker-entrypoint.sh
ENV LD_LIBRARY_PATH=/app/lib:/app/lib/ten-vad/lib/Linux/x64
RUN adduser --disabled-password --gecos "" appuser && chown -R appuser:appuser /app
USER appuser
EXPOSE 6000
ENTRYPOINT ["./docker-entrypoint.sh"]

# 使用说明:
# VAD 模型已打包，ASR/Speaker 模型首次运行时自动下载
# docker build -t asr_server .
# docker run -d -p 6000:6000 --name asr_server asr_server
