 #!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
VAD ASR 真实音频文件测试
使用test_wavs目录下的真实wav文件进行测试
"""

import asyncio
import websockets
import wave
import json
import time
import os
import signal
import sys
from pathlib import Path
import glob

class RealAudioTest:
    def __init__(self):
        self.url = "ws://localhost:8080/ws"
        self.test_dir = "test_wavs"
        # 动态读取test_wavs目录下的所有wav文件
        self.audio_files = self.get_wav_files()
        # 不使用SSL
        self.ssl_context = None
    
    def get_wav_files(self):
        """动态获取test_wavs目录下的所有wav文件"""
        wav_files = []
        if not os.path.exists(self.test_dir):
            print(f"❌ 测试目录不存在: {self.test_dir}")
            return []
        
        # 使用glob查找所有wav文件
        wav_pattern = os.path.join(self.test_dir, "*.wav")
        wav_paths = glob.glob(wav_pattern)
        
        # 只返回文件名，不包含路径
        for wav_path in wav_paths:
            filename = os.path.basename(wav_path)
            wav_files.append(filename)
        
        if not wav_files:
            print(f"❌ 在目录 {self.test_dir} 中未找到wav文件")
        else:
            print(f"✅ 找到 {len(wav_files)} 个wav文件: {wav_files}")
        
        return sorted(wav_files)  # 排序以保证测试的一致性
        
    def read_wav_file(self, file_path):
        """读取wav文件并返回音频数据"""
        try:
            with wave.open(file_path, 'rb') as wav_file:
                # 获取音频参数
                frames = wav_file.getnframes()
                sample_rate = wav_file.getframerate()
                channels = wav_file.getnchannels()
                sample_width = wav_file.getsampwidth()
                
                print(f"📊 音频信息: {frames}帧, {sample_rate}Hz, {channels}声道, {sample_width}字节/样本")
                
                # 读取音频数据
                audio_data = wav_file.readframes(frames)
                
                # 如果是立体声，转换为单声道
                if channels == 2:
                    # 简单的立体声转单声道：取左声道
                    import struct
                    if sample_width == 2:  # 16-bit
                        samples = struct.unpack(f'<{len(audio_data)//2}h', audio_data)
                        mono_samples = samples[::2]  # 取偶数索引（左声道）
                        audio_data = struct.pack(f'<{len(mono_samples)}h', *mono_samples)
                    
                return audio_data, sample_rate, len(audio_data)
                
        except Exception as e:
            print(f"❌ 读取音频文件失败 {file_path}: {e}")
            return None, 0, 0
    
    async def test_single_audio_file(self, filename):
        """测试单个音频文件"""
        file_path = os.path.join(self.test_dir, filename)
        
        if not os.path.exists(file_path):
            print(f"❌ 文件不存在: {file_path}")
            return None
            
        print(f"\n🎵 测试音频文件: {filename}")
        print("="*50)
        
        # 读取音频文件
        audio_data, sample_rate, data_size = self.read_wav_file(file_path)
        if audio_data is None:
            return None
            
        duration = data_size / (sample_rate * 2)  # 2 bytes per sample for 16-bit
        print(f"⏱️  音频时长: {duration:.2f}秒")
        
        result = {
            'filename': filename,
            'connected': False,
            'chunks_sent': 0,
            'messages_received': 0,
            'recognition_results': [],
            'total_time': 0,
            'errors': []
        }
        
        try:
            # 连接到服务器
            print("🔗 正在连接到服务器...")
            start_time = time.time()
            websocket = await websockets.connect(self.url)
            result['connected'] = True
            print("✅ 连接成功!")
            
            # 接收消息的任务
            received_messages = []
            
            async def receive_messages():
                try:
                    while True:
                        response = await asyncio.wait_for(websocket.recv(), timeout=3.0)
                        result['messages_received'] += 1
                        try:
                            data = json.loads(response)
                            received_messages.append(data)
                            msg_type = data.get('type', 'unknown')
                            if msg_type == 'final':
                                text = data.get('text', '')
                                if text:
                                    result['recognition_results'].append(text)
                                    print(f"🎯 识别结果: {text}")
                            elif msg_type == 'connection':
                                print(f"📥 连接确认: {data.get('message', '')}")
                            else:
                                print(f"📥 收到消息: {msg_type} - {data}")
                        except json.JSONDecodeError:
                            print("📥 收到非JSON响应")
                except asyncio.TimeoutError:
                    pass
                except Exception as e:
                    print(f"❌ 接收消息错误: {e}")
            
            # 启动接收任务
            receive_task = asyncio.create_task(receive_messages())
            
            # 分块发送音频数据
            chunk_size = 8192  # 8KB chunks
            total_chunks = len(audio_data) // chunk_size + (1 if len(audio_data) % chunk_size else 0)
            
            print(f"📤 开始发送音频数据 ({total_chunks}个块)...")
            
            for i in range(0, len(audio_data), chunk_size):
                chunk = audio_data[i:i + chunk_size]
                await websocket.send(chunk)
                result['chunks_sent'] += 1
                
                # 显示进度
                progress = (i + chunk_size) / len(audio_data) * 100
                if result['chunks_sent'] % 10 == 0 or progress >= 100:
                    print(f"📊 发送进度: {min(progress, 100):.1f}% ({result['chunks_sent']}/{total_chunks})")
                
                # 模拟实时发送（根据音频采样率）
                await asyncio.sleep(0.064)  # 64ms间隔
            
            print("✅ 音频发送完成，等待处理结果...")
            
            # 等待处理完成
            await asyncio.sleep(3.0)
            
            # 取消接收任务
            receive_task.cancel()
            try:
                await receive_task
            except asyncio.CancelledError:
                pass
            
            await websocket.close()
            result['total_time'] = time.time() - start_time
            
            print(f"⏱️  总测试时间: {result['total_time']:.2f}秒")
            print(f"📊 发送块数: {result['chunks_sent']}")
            print(f"📥 接收消息数: {result['messages_received']}")
            print(f"🎯 识别结果数: {len(result['recognition_results'])}")
            
            if result['recognition_results']:
                print("🎉 识别到的文本:")
                for i, text in enumerate(result['recognition_results'], 1):
                    print(f"   {i}. {text}")
            else:
                print("⚠️  没有识别到文本")
            
        except Exception as e:
            result['errors'].append(str(e))
            print(f"❌ 测试失败: {e}")
        
        return result
    
    async def test_all_files(self):
        """测试所有音频文件"""
        print("🚀 开始真实音频文件测试")
        print(f"📁 测试目录: {self.test_dir}")
        print(f"🎵 音频文件数量: {len(self.audio_files)}")
        
        results = []
        
        for filename in self.audio_files:
            result = await self.test_single_audio_file(filename)
            if result:
                results.append(result)
            
            # 文件间间隔
            print("\n" + "⏸️ " * 20 + " 暂停2秒 " + "⏸️ " * 20)
            await asyncio.sleep(2.0)
        
        # 打印总结
        self.print_summary(results)
    
    def print_summary(self, results):
        """打印测试总结"""
        print("\n" + "="*60)
        print("📊 真实音频文件测试总结")
        print("="*60)
        
        successful_tests = sum(1 for r in results if r['connected'])
        total_chunks = sum(r['chunks_sent'] for r in results)
        total_messages = sum(r['messages_received'] for r in results)
        total_recognitions = sum(len(r['recognition_results']) for r in results)
        
        print(f"📁 测试文件数: {len(results)}")
        print(f"✅ 成功连接: {successful_tests}/{len(results)}")
        print(f"📤 总发送块数: {total_chunks}")
        print(f"📥 总接收消息: {total_messages}")
        print(f"🎯 总识别结果: {total_recognitions}")
        
        print(f"\n📋 详细结果:")
        for result in results:
            filename = result['filename']
            status = "✅" if result['connected'] else "❌"
            recognition_count = len(result['recognition_results'])
            print(f"   {status} {filename}: {recognition_count}个识别结果")
            if result['recognition_results']:
                for text in result['recognition_results']:
                    print(f"      └─ \"{text}\"")
        
        # 性能评估
        print(f"\n🏆 性能评估:")
        if total_recognitions > 0:
            print("   🎉 VAD和ASR系统工作正常!")
            print("   ✅ 能够正确识别真实音频")
        else:
            print("   ⚠️  未检测到识别结果")
            print("   💡 可能需要调整VAD阈值或检查ASR模型")
        
        success_rate = (successful_tests / len(results)) * 100 if results else 0
        recognition_rate = (total_recognitions / len(results)) if results else 0
        
        print(f"   📈 连接成功率: {success_rate:.1f}%")
        print("="*60)

def signal_handler(signum, frame):
    print("\n🛑 收到中断信号，正在停止测试...")
    sys.exit(0)

async def main():
    signal.signal(signal.SIGINT, signal_handler)
    
    test = RealAudioTest()
    await test.test_all_files()

if __name__ == "__main__":
    asyncio.run(main()) 