# 阶段1：构建二进制文件
FROM golang:1.24 AS builder
WORKDIR /app
# 安装 gcc/g++ 工具链以支持 cgo
RUN apt-get update && apt-get install -y --no-install-recommends build-essential
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && go mod download
COPY . .
RUN go build -ldflags="-s -w" -o asr_server main.go

# 阶段2：模型下载
FROM ubuntu:22.04 AS model-downloader
WORKDIR /app
RUN echo "deb http://mirrors.aliyun.com/ubuntu/ jammy main restricted universe multiverse\ndeb http://mirrors.aliyun.com/ubuntu/ jammy-updates main restricted universe multiverse\ndeb http://mirrors.aliyun.com/ubuntu/ jammy-backports main restricted universe multiverse\ndeb http://mirrors.aliyun.com/ubuntu/ jammy-security main restricted universe multiverse" > /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends ca-certificates wget unzip tzdata \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
# 创建所有模型子目录
RUN mkdir -p models/vad/silero_vad \
    && mkdir -p models/vad/ten-vad/Linux/x64 \
    && mkdir -p models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17 \
    && mkdir -p models/speaker
# 分步下载每个模型，便于排查
RUN cd models && wget -nv --show-progress -O vad/silero_vad/silero_vad.onnx  https://huggingface.co/csukuangfj/vad/resolve/main/silero_vad.onnx
RUN cd models && wget -nv --show-progress -O asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/model.int8.onnx https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/model.int8.onnx
RUN cd models && wget -nv --show-progress -O asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/tokens.txt https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/resolve/main/tokens.txt
RUN cd models && wget -nv --show-progress -O speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx https://huggingface.co/csukuangfj/speaker-embedding-models/resolve/main/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx

# 阶段3：最终运行时镜像
FROM ubuntu:22.04
WORKDIR /app
RUN echo "deb http://mirrors.aliyun.com/ubuntu/ jammy main restricted universe multiverse\ndeb http://mirrors.aliyun.com/ubuntu/ jammy-updates main restricted universe multiverse\ndeb http://mirrors.aliyun.com/ubuntu/ jammy-backports main restricted universe multiverse\ndeb http://mirrors.aliyun.com/ubuntu/ jammy-security main restricted universe multiverse" > /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/asr_server .
COPY --from=model-downloader /app/models ./models
COPY config.json ./ 
COPY static ./static
COPY lib ./lib
RUN adduser --disabled-password --gecos "" appuser && chown -R appuser:appuser /app
USER appuser
ENV LD_LIBRARY_PATH=/app/lib
EXPOSE 8080
CMD ["./asr_server"]