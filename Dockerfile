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

# 阶段2：模型下载
FROM ubuntu:22.04 AS model-downloader
WORKDIR /app
RUN echo "deb http://mirrors.aliyun.com/ubuntu/ jammy main restricted universe multiverse" > /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-updates main restricted universe multiverse" >> /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-backports main restricted universe multiverse" >> /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-security main restricted universe multiverse" >> /etc/apt/sources.list
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates
# 创建所有模型子目录
RUN mkdir -p models/vad/silero_vad \
    && mkdir -p models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17 \
    && mkdir -p models/speaker
# 分步下载每个模型，便于排查
RUN cd models && curl -L --retry 5 -o asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/model.int8.onnx https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/model.int8.onnx
RUN cd models && curl -L --retry 5 -o asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/tokens.txt https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/tokens.txt
RUN cd models && curl -L --retry 5 -o speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx https://huggingface.co/csukuangfj/speaker-embedding-models/resolve/main/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx

# 阶段3：最终运行时镜像
FROM ubuntu:22.04 AS final
WORKDIR /app
RUN echo "deb http://mirrors.aliyun.com/ubuntu/ jammy main restricted universe multiverse" > /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-updates main restricted universe multiverse" >> /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-backports main restricted universe multiverse" >> /etc/apt/sources.list \
    && echo "deb http://mirrors.aliyun.com/ubuntu/ jammy-security main restricted universe multiverse" >> /etc/apt/sources.list
RUN apt-get update && apt-get install -y --no-install-recommends libc++1 libc++abi1
COPY --from=builder /app/asr_server .
COPY --from=model-downloader /app/models ./models
# 直接复制本地的silero_vad模型文件
COPY models/vad/silero_vad/silero_vad.onnx ./models/vad/silero_vad/
COPY config.json ./ 
COPY static ./static
COPY lib ./lib
ENV LD_LIBRARY_PATH=/app/lib:/app/lib/ten-vad/lib/Linux/x64
RUN adduser --disabled-password --gecos "" appuser && chown -R appuser:appuser /app
USER appuser
EXPOSE 8080
CMD ["./asr_server"]