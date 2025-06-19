# 🎤 VAD ASR 语音识别服务器

一个基于 Sherpa-ONNX 的高性能语音识别服务，支持实时VAD（语音活动检测）、多语言语音识别和声纹识别功能。采用协程池+Stream池架构设计，优化并发处理性能。

## ✨ 特性

### 🚀 核心功能
- **实时语音识别**: 支持中文、英文、日语、韩语、粤语等多语言识别
- **VAD语音活动检测**: 智能检测语音起止，过滤静音段
- **声纹识别**: 支持说话人注册、识别和管理
- **WebSocket实时通信**: 低延迟的实时音频流处理
- **文件批处理**: 支持音频文件上传识别

### 🏗️ 架构优势
- **Stream池复用**: 单Recognizer + Stream池架构，避免频繁创建销毁开销
- **高并发处理**: 支持1000+并发连接，工作池模式处理任务
- **智能负载均衡**: 动态Stream分配，资源使用最优化
- **容错机制**: 连接断开自动清理，Stream临时创建机制
- **性能监控**: 实时统计处理性能和资源使用情况

### 🛡️ 生产就绪
- **速率限制**: 内置请求频率控制和连接数限制
- **日志系统**: 结构化日志记录，支持文件轮转
- **配置热更新**: 支持配置文件动态重载
- **优雅关闭**: 确保资源正确释放
- **HTTPS支持**: 提供SSL证书生成脚本

## 🏛️ 系统架构

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   WebSocket客户端 │    │  VAD语音活动检测池  │    │   Stream资源池   │
│                 │    │                  │    │                 │
│  ┌─────────────┐│    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│  │ 音频流输入   ││◄──►│ │   VAD实例    │ │◄──►│ │ Recognizer  │ │
│  └─────────────┘│    │ └──────────────┘ │    │ │   + Stream  │ │
│                 │    │ ┌──────────────┐ │    │ │    池管理   │ │
│  ┌─────────────┐│    │ │   缓冲队列   │ │    │ └─────────────┘ │
│  │ 识别结果接收 ││    │ └──────────────┘ │    └─────────────────┘
│  └─────────────┘│    └──────────────────┘             │
└─────────────────┘                                     │
                                                         ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   会话管理器     │    │    工作池调度器   │    │   声纹识别模块   │
│                 │    │                  │    │                 │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│ │ 连接状态管理 │ │    │ │ Worker协程   │ │    │ │ 说话人注册  │ │
│ └─────────────┘ │    │ └──────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │ ┌──────────────┐ │    │ ┌─────────────┐ │
│ │ 资源分配释放 │ │    │ │ 任务队列管理 │ │    │ │ 声纹特征提取 │ │
│ └─────────────┘ │    │ └──────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

## 🚀 快速开始

### 📋 系统要求

- **Go版本**: 1.21+
- **操作系统**: Linux/macOS/Windows
- **内存**: 建议4GB+
- **CPU**: 支持多核并行处理

### 📦 安装依赖

```bash
# 克隆项目
git clone https://github.com/bbeyondllove/asr_server.git
cd asr_server

# 安装Go依赖
go mod download
```

### 🎯 模型准备

1. **下载语音识别模型**:
```bash
# 创建模型目录
mkdir -p models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17

# 下载模型文件到对应目录
# - model.int8.onnx
# - tokens.txt
```

2. **下载VAD模型**:
```bash
mkdir -p models/vad
# 下载 silero_vad.onnx 到 models/vad/ 目录
```

3. **下载声纹识别模型**（可选）:
```bash
mkdir -p models/speaker
# 下载 3dspeaker_speech_campplus_sv_zh-cn_16k-common.onnx
```

### ⚡ 运行服务

```bash
# 使用默认配置启动
go run main.go

# 或指定配置文件
go run main.go -config config.json

# 生产环境编译运行
go build -o asr_server
./asr_server
```

服务启动后访问: `http://localhost:8080` 可以进行功能测试

## ⚙️ 配置说明

### 📝 配置文件结构

配置文件 `config.json` 包含以下主要配置：

```json
{
  "server": {
    "port": 8080,
    "host": "0.0.0.0",
    "max_connections": 500
  },
  "recognition": {
    "model_path": "models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/model.int8.onnx",
    "tokens_path": "models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/tokens.txt",
    "language": "auto",
    "num_threads": 2,
    "provider": "cpu"
  },
  "vad": {
    "model_path": "models/vad/silero_vad.onnx",
    "threshold": 0.5,
    "min_silence_duration": 0.1,
    "min_speech_duration": 0.25
  },
  "pool": {
    "worker_count": 500,
    "queue_size": 2000
  },
  "speaker": {
    "enabled": true,
    "model_path": "models/speaker/3dspeaker_speech_campplus_sv_zh-cn_16k-common.onnx",
    "threshold": 0.6
  }
}
```

### 🎛️ 关键参数说明

| 参数 | 说明 | 推荐值 |
|------|------|--------|
| `pool.worker_count` | 工作协程数量 | CPU核心数 × 100 |
| `vad.threshold` | VAD检测阈值 | 0.5 (越高越严格) |
| `server.max_connections` | 最大并发连接数 | 500 |
| `speaker.threshold` | 声纹识别阈值 | 0.6 (越高越准确) |

## 🔌 API 使用

### 🌐 WebSocket API

#### 连接建立
```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = function() {
    console.log('连接已建立');
};

ws.onmessage = function(event) {
    const data = JSON.parse(event.data);
    console.log('识别结果:', data);
};
```

#### 发送音频数据
```javascript
// 发送音频流数据（16kHz, 16bit, 单声道）
ws.send(audioBuffer);

// 发送结束标志
ws.send(JSON.stringify({type: "end"}));
```

#### 响应格式
```json
{
  "type": "result",
  "text": "识别到的文字内容",
  "is_final": true,
  "confidence": 0.95,
  "speaker_id": "speaker_001",
  "session_id": "session_123456"
}
```

### 🎤 声纹识别API

#### 注册说话人
```bash
curl -X POST http://localhost:8080/api/v1/speaker/register \
  -F "audio=@voice_sample.wav" \
  -F "speaker_id=speaker_001" \
  -F "speaker_name=张三"
```

#### 识别说话人
```bash
curl -X POST http://localhost:8080/api/v1/speaker/identify \
  -F "audio=@unknown_voice.wav"
```

#### 管理说话人
```bash
# 获取说话人列表
curl http://localhost:8080/api/v1/speaker/list

# 删除说话人
curl -X DELETE http://localhost:8080/api/v1/speaker/speaker_001
```

### 📊 系统监控API

```bash
# 健康检查
curl http://localhost:8080/health

# 系统状态
curl http://localhost:8080/stats

# 性能指标
curl http://localhost:8080/metrics
```

## 🧪 测试

### 🏃‍♂️ 运行压力测试

```bash
cd test/asr

# 基础测试（10个并发，每个发送5个文件）
python stress_test.py --connections 10 --files-per-connection 5

# 高强度测试
python stress_test.py --connections 100 --files-per-connection 10 --duration 300

# 自定义测试参数
python stress_test.py \
  --host localhost \
  --port 8080 \
  --connections 50 \
  --files-per-connection 3 \
  --audio-dir test_wavs \
  --report-interval 5
```

### 📈 测试报告

测试完成后会生成详细报告：
- 连接成功率
- 平均响应时间
- 系统资源使用情况
- 识别准确率统计
- 错误日志分析

### 🎯 准备测试音频

```bash
# 测试音频要求：
# - 格式：WAV
# - 采样率：16kHz
# - 位深：16bit
# - 声道：单声道
# - 时长：建议1-30秒

# 转换音频格式示例（使用ffmpeg）
ffmpeg -i input.mp3 -ar 16000 -ac 1 -sample_fmt s16 output.wav
```

## 🚀 部署

### 🐳 Docker部署

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o asr_server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/asr_server .
COPY --from=builder /app/config.json .
COPY --from=builder /app/models ./models
CMD ["./asr_server"]
```

### 🌐 Nginx反向代理

```bash
# 生成SSL证书
go run scripts/generate_ssl_certs.go

# 使用提供的nginx配置
cp scripts/nginx.conf /etc/nginx/sites-available/asr_server
ln -s /etc/nginx/sites-available/asr_server /etc/nginx/sites-enabled/

# 重启nginx
systemctl restart nginx
```

### 🔧 系统服务

```ini
# /etc/systemd/system/asr_server.service
[Unit]
Description=VAD ASR Server
After=network.target

[Service]
Type=simple
User=asr
WorkingDirectory=/opt/asr_server
ExecStart=/opt/asr_server/asr_server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# 启用服务
systemctl enable asr_server
systemctl start asr_server
```

## 📊 性能优化

### 🎯 关键性能参数

| 参数 | 影响 | 调优建议 |
|------|------|----------|
| `pool.worker_count` | 并发处理能力 | 根据CPU核心数和内存调整 |
| `vad_pool.pool_size` | VAD处理效率 | 设为worker_count的1-2倍 |
| `rate_limit.requests_per_second` | 流量控制 | 根据服务器性能设置 |
| `audio.queue_size` | 音频缓冲 | 网络不稳定时增大 |

### 💡 优化建议

1. **硬件配置**:
   - CPU：推荐8核以上
   - 内存：推荐16GB以上
   - 存储：使用SSD提升模型加载速度

2. **系统调优**:
   - 增加文件描述符限制: `ulimit -n 65536`
   - 优化TCP连接: 调整`net.core.somaxconn`
   - 设置合适的交换内存策略

3. **应用调优**:
   - 根据实际负载调整worker数量
   - 监控内存使用，及时调整池大小
   - 使用CPU provider提升识别速度

## 🔍 故障排除

### ❗ 常见问题

**Q: 连接建立失败**
```bash
# 检查端口占用
netstat -tlnp | grep 8080

# 检查防火墙设置
firewall-cmd --list-ports
```

**Q: 识别结果为空**
```bash
# 检查音频格式
ffprobe audio_file.wav

# 确认模型文件完整
ls -la models/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/
```

**Q: 内存使用过高**
```bash
# 调整池大小配置
{
  "vad_pool": {
    "pool_size": 50,
    "max_idle": 25
  }
}
```

### 📋 日志分析

```bash
# 查看实时日志
tail -f logs/app.log

# 错误日志过滤
grep "ERROR" logs/app.log | tail -20

# 性能统计
grep "stats" logs/app.log | jq '.'
```

## 🤝 贡献

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🙏 致谢

- [Sherpa-ONNX](https://github.com/k2-fsa/sherpa-onnx) - 核心语音识别引擎
- [SenseVoice](https://github.com/FunAudioLLM/SenseVoice) - 多语言语音识别模型
- [Silero VAD](https://github.com/snakers4/silero-vad) - 语音活动检测模型

## 📞 支持

如有问题或建议，请：
- 创建 [Issue]
- 发送邮件到: bbeyond.llove@gmail.com

---

**享受语音识别的乐趣！** 🎉 
