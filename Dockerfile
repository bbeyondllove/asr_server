# 使用官方 Golang 镜像作为构建环境
FROM golang:1.24 AS builder

WORKDIR /app

# 拷贝 go.mod 和 go.sum 并下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 拷贝项目源码
COPY . .

# 构建可执行文件
RUN go build -o asr_server main.go

# 使用更小的基础镜像运行
FROM debian:bookworm-slim
WORKDIR /app

# 拷贝编译好的二进制文件和必要的资源
COPY --from=builder /app/asr_server ./
COPY config.json ./
COPY static ./static
COPY data ./data
# 安装解压和下载工具
RUN apt-get update && apt-get install -y unzip wget

# 下载模型文件（如有需要可根据实际情况增减）
RUN mkdir -p models/vad/silero_vad && \
    wget -O models/vad/silero_vad/silero_vad.onnx https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx && \
    mkdir -p models/vad/ten-vad/Linux/x64 && \
    wget -O models/vad/ten-vad/Linux/x64/libten_vad.so https://huggingface.co/TEN-framework/ten-vad/resolve/main/lib/Linux/x64/libten_vad.so && \
    mkdir -p models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17 && \
    wget -O models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/model.int8.onnx https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/model.int8.onnx && \
    wget -O models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/tokens.txt https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/tokens.txt && \
    mkdir -p models/speaker && \
    wget -O models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx https://huggingface.co/csukuangfj/speaker-embedding-models/resolve/main/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx

# 复制 lib 目录
COPY lib ./lib
ENV LD_LIBRARY_PATH=/app/lib:$LD_LIBRARY_PATH

# 暴露端口（假设 8080，实际可根据 config.json 调整）
EXPOSE 8080

# 启动服务
CMD ["./asr_server"] 