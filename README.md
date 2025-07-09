# 🎤 VAD ASR 语音识别服务器

基于 Sherpa-ONNX 的高性能语音识别服务，支持实时VAD（语音活动检测）、多语言识别和声纹识别。

## ✨ 特性
- 实时多语言语音识别（中/英/日/韩/粤等）
- VAD智能分段，自动过滤静音
- 声纹识别
- WebSocket 实时通信，低延迟
- 健康检查、状态监控、优雅关闭

## 🚀 快速开始

### 方式一：Docker 部署（推荐）

> **推荐：Docker 镜像已自动包含主要模型文件（vad、asr、speaker）和 lib 目录，无需手动挂载 models 或 lib 目录。**

#### 构建镜像
```bash
docker build -t asr_server .
```

#### 运行容器（假设端口 8080）
```bash
docker run -d -p 8080:8080 --name asr_server asr_server
```

#### 端口与访问
- 测试页面: http://localhost:8080/
- 健康检查: http://localhost:8080/health
- WebSocket: ws://localhost:8080/ws

---

### 方式二：源码部署（进阶/开发者）

#### 系统要求
- Go 1.21+
- Linux/macOS/Windows
- 内存建议4GB+

#### 安装依赖
```bash
# 克隆项目
git clone https://github.com/bbeyondllove/asr_server.git
cd asr_server
# 安装Go依赖
go mod tidy
```

#### 依赖库准备

- **主程序依赖的动态库**（如 `onnxruntime.dll`、`sherpa-onnx-c-api.dll`、`libonnxruntime.so`、`libsherpa-onnx-c-api.so`）已放在项目根目录下的 `lib/` 目录。

#### 模型准备

**VAD模型：**
- **silero_vad**：轻量级、跨平台的语音活动检测模型，适合大多数实时场景。
  - 下载链接：[silero_vad.onnx](https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx)
  - 存放路径：`models/vad/silero_vad/silero_vad.onnx`
- **ten-vad**：高性能 VAD 动态库
  - 下载链接：[ten-vad](https://huggingface.co/TEN-framework/ten-vad)
  - 存放路径：`models/vad/ten-vad`

**ASR模型：**
- **SenseVoice多语种模型**：支持中/英/日/韩/粤等多语种识别，适合大多数通用场景。
  - 下载链接：[sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17](https://huggingface.co/csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17)
  - 存放路径：`models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/`

**声纹识别模型：**
- **3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx**：支持多语种声纹识别。
  - 下载链接：[3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx](https://huggingface.co/csukuangfj/speaker-embedding-models/resolve/main/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx)
  - 存放路径：`models/speaker/3dspeaker_speech_campplus_sv_zh_en_16k-common_advanced.onnx`

模型下载后解压到目录 models：
1. VAD 模型: 
    models/vad/silero_vad/
    models/vad/ten-vad/
2. ASR模型: models/asr/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17/
3. 声纹识别模型: models/speaker/


#### 运行服务
```bash
# 默认配置启动
go run main.go
# 或编译后运行
go build -o asr_server
./asr_server
```

#### 访问测试
- 测试页面: http://localhost:8080/
- 健康检查: http://localhost:8080/health
- WebSocket: ws://localhost:8080/ws

---

## ⚙️ 配置
详细配置请参考 `config.json` 文件。

## 🔌 WebSocket API 示例
```javascript
const ws = new WebSocket('ws://localhost:8080/ws');
ws.onopen = () => ws.send(audioBuffer);
ws.onmessage = e => console.log('识别结果:', e.data);
```


## 🏛️ 系统架构

```
┌────────────────────┐    ┌──────────────────────┐    ┌────────────────────┐
│   WebSocket客户端   │    │   VAD语音活动检测池   │    │   ASR识别器模块     │
│                    │    │                      │    │ (动态new stream)   │
│  ┌──────────────┐  │    │  ┌──────────────┐    │    │  ┌──────────────┐  │
│  │  音频流输入   │◄─┼───►│  │   VAD实例    │◄──┼───►│  │ Recognizer   │  │
│  └──────────────┘  │    │  └──────────────┘    │    │  └──────────────┘  │
│  ┌──────────────┐  │    │  ┌──────────────┐    │    │                  │
│  │ 识别结果接收  │  │    │  │  缓冲队列    │    │    │                  │
│  └──────────────┘  │    │  └──────────────┘    │    └────────────────────┘
└────────────────────┘    └──────────────────────┘             │
                                                               ▼
┌────────────────────┐    ┌──────────────────────┐    ┌────────────────────┐
│   会话管理器       │    │   声纹识别模块(可选)  │    │   健康检查/监控    │
│  ┌──────────────┐  │    │  ┌──────────────┐    │    │                    │
│  │ 连接状态管理 │  │    │  │ 说话人注册   │    │    │  监控/状态接口     │
│  └──────────────┘  │    │  └──────────────┘    │    └────────────────────┘
│  ┌──────────────┐  │    │  ┌──────────────┐    │
│  │ 资源分配释放 │  │    │  │ 声纹特征提取 │    │
│  └──────────────┘  │    │  └──────────────┘    │
└────────────────────┘    └──────────────────────┘
```
 
## 🎛️ 关键参数说明
| 参数 | 说明 | 推荐值 |
|------|------|--------|
| `vad.threshold` | VAD检测阈值 | 0.5 |
| `vad.min_silence_duration` | 最小静音时长 | 0.1 |
| `vad.min_speech_duration` | 最小语音时长 | 0.25 |
| `vad.pool_size` | VAD池实例数 | 200 |
| `recognition.num_threads` | ASR线程数 | 8-16 |
| `audio.sample_rate` | 采样率 | 16000 |
| `server.port` | 服务端口 | 8080 |

## 🧪 测试例子
项目自带 test/asr/ 目录下的测试脚本：
- `audiofile_test.py`：单文件识别测试，支持多语种 wav 文件。
- `stress_test.py`：并发压力测试，模拟多连接并发识别。

用法示例：
```bash
python stress_test.py --connections 100 --audio-per-connection 2
```
- `--connections`：并发连接数（如 100 表示同时模拟 100 个客户端）
- `--audio-per-connection`：每个连接要发送的音频文件数（如 2 表示每个连接各自发送 2 个音频文件）

本例将模拟 100 个并发连接，每个连接各自发送 2 个音频文件，总共 200 次识别请求。

## 🤝 贡献
欢迎贡献代码！流程如下：
1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 许可证
本项目采用 MIT 许可证 - 查看 LICENSE 文件了解详情。

## 🙏 致谢
- Sherpa-ONNX - 核心语音识别引擎
- SenseVoice - 多语言语音识别模型
- Silero VAD - 语音活动检测模型

## 📞 支持
如有问题或建议，请：
- 创建 [Issue]
- 发送邮件到: bbeyond.llove@gmail.com