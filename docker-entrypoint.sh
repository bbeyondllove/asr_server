#!/bin/bash
set -e

echo "Checking required model files..."

REQUIRED_FILES=(
    "models/vad/silero_vad/silero_vad.onnx"
    "models/asr/Fun-ASR-Nano-2512-8bit/model.onnx"
    "models/asr/Fun-ASR-Nano-2512-8bit/tokens.txt"
    "models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx"
)

MISSING_FILES=()

for file in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "/app/$file" ]; then
        MISSING_FILES+=("$file")
    fi
done

if [ ${#MISSING_FILES[@]} -ne 0 ]; then
    echo "WARNING: Missing required model files:"
    for file in "${MISSING_FILES[@]}"; do
        echo "  - $file"
    done
    echo ""
    echo "Attempting to download models automatically..."

    # 下载 ASR 模型
    if [[ ! -f "/app/models/asr/Fun-ASR-Nano-2512-8bit/model.onnx" ]]; then
        echo "Downloading Fun-ASR-Nano-2512-8bit model from ModelScope..."
        python3 -c "
from modelscope import snapshot_download
model_dir = snapshot_download('fengge2024/Fun-ASR-Nano-2512-8bit', cache_dir='/app/models_cache')
print(f'Model downloaded to: {model_dir}')
" || {
            echo "ERROR: Failed to download ASR model"
            exit 1
        }
        mkdir -p /app/models/asr/Fun-ASR-Nano-2512-8bit
        find /app/models_cache -name "model*.onnx" -exec cp {} /app/models/asr/Fun-ASR-Nano-2512-8bit/model.onnx \;
        find /app/models_cache -name "tokens.txt" -exec cp {} /app/models/asr/Fun-ASR-Nano-2512-8bit/ \;
        echo "ASR model files installed successfully."
    fi

    # 下载 VAD 模型
    if [[ ! -f "/app/models/vad/silero_vad/silero_vad.onnx" ]]; then
        echo "Downloading Silero VAD model..."
        mkdir -p /app/models/vad/silero_vad
        wget -O /app/models/vad/silero_vad/silero_vad.onnx \
            https://github.com/snakers4/silero-vad/raw/master/files/silero_vad.onnx || {
            echo "ERROR: Failed to download VAD model"
            exit 1
        }
        echo "VAD model downloaded successfully."
    fi

    # 下载 Speaker 模型
    if [[ ! -f "/app/models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx" ]]; then
        echo "Downloading 3DSpeaker model..."
        python3 -c "
from modelscope import snapshot_download
model_dir = snapshot_download('fengge2024/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx', cache_dir='/app/models_cache')
print(f'Speaker model downloaded to: {model_dir}')
" || {
            echo "ERROR: Failed to download Speaker model"
            exit 1
        }
        mkdir -p /app/models/speaker
        find /app/models_cache -name "3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx" -exec cp {} /app/models/speaker/ \; 2>/dev/null || \
        find /app/models_cache -name "*.onnx" -path "*/3dspeaker*" -exec cp {} /app/models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx \;
        echo "Speaker model downloaded successfully."
    fi

    # 重新检查文件
    MISSING_FILES=()
    for file in "${REQUIRED_FILES[@]}"; do
        if [ ! -f "/app/$file" ]; then
            MISSING_FILES+=("$file")
        fi
    done

    if [ ${#MISSING_FILES[@]} -ne 0 ]; then
        echo "ERROR: Some files are still missing after download attempt:"
        for file in "${MISSING_FILES[@]}"; do
            echo "  - $file"
        done
        exit 1
    fi

    echo "All model files downloaded and installed successfully."
else
    echo "All required model files found."
fi

echo "Starting ASR server..."

exec ./asr_server
