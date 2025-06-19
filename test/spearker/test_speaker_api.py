#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
声纹识别API测试脚本
使用方法: python test_speaker_api.py
"""

import requests
import json
import numpy as np
import wave
import time
from typing import Dict, Any, Optional

BASE_URL = "http://localhost:8080"
SPEAKER_API = f"{BASE_URL}/api/v1/speaker"

def print_section(title: str):
    """打印分节标题"""
    print(f"\n{'='*60}")
    print(f" {title}")
    print(f"{'='*60}")

def test_service_health():
    """测试服务健康状态"""
    print_section("1. 测试服务健康状态")
    try:
        response = requests.get(f"{BASE_URL}/health")
        if response.status_code == 200:
            print("✅ 服务运行正常")
            data = response.json()
            print(f"   状态: {data.get('status')}")
            print(f"   时间: {data.get('timestamp')}")
            return True
        else:
            print(f"❌ 服务异常: HTTP {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ 连接失败: {e}")
        return False

def test_speaker_stats():
    """测试获取声纹统计信息"""
    print_section("2. 获取声纹统计信息")
    try:
        response = requests.get(f"{SPEAKER_API}/stats")
        if response.status_code == 200:
            data = response.json()
            print("✅ 统计信息获取成功")
            print(f"   注册说话人: {data.get('total_speakers', 0)}")
            print(f"   总样本数: {data.get('total_samples', 0)}")
            print(f"   特征维度: {data.get('embedding_dim', 0)}")
            print(f"   识别阈值: {data.get('threshold', 0)}")
            print(f"   最后更新: {data.get('updated_at', 'N/A')}")
            return data
        else:
            print(f"❌ 获取统计信息失败: HTTP {response.status_code}")
            return None
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return None

def test_list_speakers():
    """测试获取说话人列表"""
    print_section("3. 获取说话人列表")
    try:
        response = requests.get(f"{SPEAKER_API}/list")
        if response.status_code == 200:
            data = response.json()
            print("✅ 说话人列表获取成功")
            print(f"   总数: {data.get('total', 0)}")
            speakers = data.get('speakers', [])
            if speakers:
                for i, speaker in enumerate(speakers, 1):
                    print(f"   {i}. ID: {speaker.get('id')}, 名称: {speaker.get('name')}, 样本: {speaker.get('sample_count')}")
            else:
                print("   暂无注册的说话人")
            return speakers
        else:
            print(f"❌ 获取说话人列表失败: HTTP {response.status_code}")
            return []
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return []

def generate_test_audio(sample_rate: int = 16000, duration: float = 2.0) -> bytes:
    """生成测试音频数据（正弦波）"""
    t = np.linspace(0, duration, int(sample_rate * duration), False)
    # 生成440Hz的正弦波（A4音符）
    audio = np.sin(2 * np.pi * 440 * t) * 0.3
    # 转换为16位整数
    audio_int16 = (audio * 32767).astype(np.int16)
    
    # 创建WAV格式的字节数据
    import io
    wav_buffer = io.BytesIO()
    with wave.open(wav_buffer, 'wb') as wav_file:
        wav_file.setnchannels(1)  # 单声道
        wav_file.setsampwidth(2)  # 16位
        wav_file.setframerate(sample_rate)
        wav_file.writeframes(audio_int16.tobytes())
    
    wav_buffer.seek(0)
    return wav_buffer.read()

def test_speaker_registration():
    """测试声纹注册"""
    print_section("4. 测试声纹注册")
    
    # 生成测试音频
    print("🎵 生成测试音频...")
    audio_data = generate_test_audio()
    
    # 注册说话人
    speaker_id = "test_speaker_001"
    speaker_name = "测试用户A"
    
    try:
        files = {
            'audio': ('test_audio.wav', audio_data, 'audio/wav')
        }
        data = {
            'speaker_id': speaker_id,
            'speaker_name': speaker_name
        }
        
        print(f"📝 注册说话人 '{speaker_name}' (ID: {speaker_id})...")
        response = requests.post(f"{SPEAKER_API}/register", files=files, data=data)
        
        if response.status_code == 200:
            result = response.json()
            print("✅ 声纹注册成功")
            print(f"   说话人ID: {result.get('speaker_id')}")
            print(f"   说话人名称: {result.get('speaker_name')}")
            print(f"   样本数量: {result.get('sample_count')}")
            return True
        else:
            print(f"❌ 声纹注册失败: HTTP {response.status_code}")
            try:
                error_data = response.json()
                print(f"   错误信息: {error_data.get('error', '未知错误')}")
            except:
                print(f"   响应内容: {response.text}")
            return False
    except Exception as e:
        print(f"❌ 注册失败: {e}")
        return False

def test_speaker_identification():
    """测试声纹识别"""
    print_section("5. 测试声纹识别")
    
    # 生成测试音频
    print("🎵 生成测试音频...")
    audio_data = generate_test_audio()
    
    try:
        files = {
            'audio': ('test_audio.wav', audio_data, 'audio/wav')
        }
        
        print("🔍 进行声纹识别...")
        response = requests.post(f"{SPEAKER_API}/identify", files=files)
        
        if response.status_code == 200:
            result = response.json()
            print("✅ 声纹识别完成")
            if result.get('identified'):
                print(f"   识别结果: 已识别")
                print(f"   说话人ID: {result.get('speaker_id')}")
                print(f"   说话人名称: {result.get('speaker_name')}")
                print(f"   置信度: {result.get('confidence'):.3f}")
            else:
                print(f"   识别结果: 未识别到匹配的说话人")
            print(f"   识别阈值: {result.get('threshold')}")
            return result
        else:
            print(f"❌ 声纹识别失败: HTTP {response.status_code}")
            try:
                error_data = response.json()
                print(f"   错误信息: {error_data.get('error', '未知错误')}")
            except:
                print(f"   响应内容: {response.text}")
            return None
    except Exception as e:
        print(f"❌ 识别失败: {e}")
        return None

def test_speaker_verification():
    """测试声纹验证"""
    print_section("6. 测试声纹验证")
    
    # 生成测试音频
    print("🎵 生成测试音频...")
    audio_data = generate_test_audio()
    
    speaker_id = "test_speaker_001"
    
    try:
        files = {
            'audio': ('test_audio.wav', audio_data, 'audio/wav')
        }
        
        print(f"🔐 验证说话人 {speaker_id}...")
        response = requests.post(f"{SPEAKER_API}/verify/{speaker_id}", files=files)
        
        if response.status_code == 200:
            result = response.json()
            print("✅ 声纹验证完成")
            print(f"   说话人ID: {result.get('speaker_id')}")
            print(f"   说话人名称: {result.get('speaker_name')}")
            print(f"   验证结果: {'通过' if result.get('verified') else '失败'}")
            print(f"   置信度: {result.get('confidence'):.3f}")
            print(f"   验证阈值: {result.get('threshold')}")
            return result
        else:
            print(f"❌ 声纹验证失败: HTTP {response.status_code}")
            try:
                error_data = response.json()
                print(f"   错误信息: {error_data.get('error', '未知错误')}")
            except:
                print(f"   响应内容: {response.text}")
            return None
    except Exception as e:
        print(f"❌ 验证失败: {e}")
        return None

def main():
    """主测试函数"""
    print("🎤 声纹识别API测试工具")
    print("=" * 60)
    
    # 1. 测试服务健康状态
    if not test_service_health():
        print("\n❌ 服务未运行，请先启动ASR服务器")
        print("启动命令: go run main.go")
        return
    
    # 2. 获取统计信息
    test_speaker_stats()
    
    # 3. 获取说话人列表
    speakers = test_list_speakers()
    
    # 4. 测试声纹注册
    if test_speaker_registration():
        # 5. 重新获取统计信息
        print_section("4.1 注册后统计信息")
        test_speaker_stats()
        
        # 6. 重新获取说话人列表
        print_section("4.2 注册后说话人列表")
        test_list_speakers()
    
    # 7. 测试声纹识别
    test_speaker_identification()
    
    # 8. 测试声纹验证
    test_speaker_verification()
    
    print_section("测试完成")
    print("✅ 所有API测试已完成")
    print("\n📝 可用的API端点:")
    print("   - GET  /api/v1/speaker/list       - 获取说话人列表")
    print("   - GET  /api/v1/speaker/stats      - 获取统计信息")
    print("   - POST /api/v1/speaker/register   - 注册声纹")
    print("   - POST /api/v1/speaker/identify   - 识别声纹")
    print("   - POST /api/v1/speaker/verify/:id - 验证声纹")
    print("   - DELETE /api/v1/speaker/:id      - 删除说话人")

if __name__ == "__main__":
    main() 