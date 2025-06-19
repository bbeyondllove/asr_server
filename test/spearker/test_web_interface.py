#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Web界面功能测试脚本
测试新的语音识别与声纹识别Web界面
"""

import requests
import time
import json

BASE_URL = "http://localhost:8080"

def test_web_page():
    """测试Web页面是否可以访问"""
    print("🌐 测试Web页面访问...")
    try:
        response = requests.get(BASE_URL)
        if response.status_code == 200:
            print("✅ Web页面访问正常")
            print(f"   页面大小: {len(response.content)} 字节")
            # 检查是否包含声纹识别相关内容
            content = response.text
            if "声纹识别系统" in content:
                print("✅ 页面包含声纹识别功能")
            if "语音识别" in content:
                print("✅ 页面包含语音识别功能")
            return True
        else:
            print(f"❌ Web页面访问失败: HTTP {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ Web页面访问异常: {e}")
        return False

def test_speaker_api():
    """测试声纹识别API是否正常"""
    print("\n👤 测试声纹识别API...")
    
    # 测试获取说话人列表
    try:
        response = requests.get(f"{BASE_URL}/api/v1/speaker/list")
        if response.status_code == 200:
            data = response.json()
            print(f"✅ 说话人列表API正常，当前注册人数: {data.get('total', 0)}")
        else:
            print(f"❌ 说话人列表API失败: HTTP {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ 说话人列表API异常: {e}")
        return False
    
    # 测试获取统计信息
    try:
        response = requests.get(f"{BASE_URL}/api/v1/speaker/stats")
        if response.status_code == 200:
            data = response.json()
            print(f"✅ 统计信息API正常，特征维度: {data.get('embedding_dim', 0)}")
        else:
            print(f"❌ 统计信息API失败: HTTP {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ 统计信息API异常: {e}")
        return False
    
    return True

def test_health_check():
    """测试健康检查"""
    print("\n💓 测试服务健康状态...")
    try:
        response = requests.get(f"{BASE_URL}/health")
        if response.status_code == 200:
            data = response.json()
            print(f"✅ 服务健康状态: {data.get('status', '未知')}")
            print(f"   活跃会话: {data.get('sessions', {}).get('active_sessions', 0)}")
            print(f"   VAD池状态: {data.get('vad_pool', {}).get('available_count', 0)}/{data.get('vad_pool', {}).get('pool_size', 0)}")
            return True
        else:
            print(f"❌ 健康检查失败: HTTP {response.status_code}")
            return False
    except Exception as e:
        print(f"❌ 健康检查异常: {e}")
        return False

def show_feature_summary():
    """显示功能总结"""
    print("\n" + "="*60)
    print("🎉 语音识别与声纹识别系统已就绪！")
    print("="*60)
    print("\n📱 Web界面功能:")
    print("   1. 🗣️  实时语音识别 - 支持WebSocket连接进行实时ASR")
    print("   2. 📝  声纹注册 - 输入姓名后录音≥3秒进行注册")
    print("   3. 🔍  声纹识别 - 录音后自动识别说话人身份")
    print("   4. 👥  说话人管理 - 查看和删除已注册的说话人")
    print("   5. 📊  实时状态监控 - 显示连接和录音状态")
    
    print("\n🎯 声纹注册流程:")
    print("   1. 在'说话人姓名'输入框中输入姓名（必填）")
    print("   2. 点击'开始录音注册'按钮开始录音")
    print("   3. 录音至少3秒（界面会显示计时）")
    print("   4. 再次点击按钮停止录音并自动注册")
    print("   5. 显示注册结果，成功后自动刷新说话人列表")
    
    print("\n🔍 声纹识别流程:")
    print("   1. 点击'开始录音识别'按钮开始录音")
    print("   2. 录音完成后点击停止")
    print("   3. 系统自动识别并显示说话人信息和置信度")
    
    print("\n🌐 访问地址:")
    print(f"   Web界面: {BASE_URL}")
    print(f"   健康检查: {BASE_URL}/health")
    print(f"   API文档: {BASE_URL}/api/v1/speaker/")
    
    print("\n💡 使用提示:")
    print("   - 确保浏览器允许麦克风权限")
    print("   - 建议在安静环境中录音以获得更好效果")
    print("   - 注册时请录音3秒，内容可以是简单的自我介绍")
    print("   - 识别时置信度>80%为高可信度，60-80%为中等，<60%为低可信度")

def main():
    """主测试函数"""
    print("🧪 Web界面功能测试")
    print("="*30)
    
    # 测试Web页面
    if not test_web_page():
        print("\n❌ Web页面测试失败，请检查服务器是否正常运行")
        return
    
    # 测试健康检查
    if not test_health_check():
        print("\n❌ 健康检查失败，服务器可能存在问题")
        return
    
    # 测试声纹API
    if not test_speaker_api():
        print("\n❌ 声纹识别API测试失败")
        return
    
    # 显示功能总结
    show_feature_summary()

if __name__ == "__main__":
    main() 