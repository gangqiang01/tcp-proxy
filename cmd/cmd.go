package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gangqiang01/tcp-proxy/internal/proxy"
	"k8s.io/klog/v2"
)

// Run 启动代理服务器
func Run(cfg *proxy.Config) {
	// 创建并启动服务器
	server := proxy.NewServer(cfg)

	// 启动服务器
	go func() {
		klog.Infof("启动TCP代理服务器，监听地址: %s", cfg.ListenAddr)
		klog.Infof("Repeater地址: %s", cfg.RepeaterAddr)
		klog.Infof("最大连接数: %d", cfg.MaxConnections)

		if err := server.Start(); err != nil {
			klog.Errorf("服务器启动失败: %v", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// 等待信号
	sig := <-sigChan
	klog.Infof("接收到信号: %v，正在关闭服务器...", sig)

	// 优雅关闭
	server.Stop()

	// 等待清理完成
	time.Sleep(2 * time.Second)

	klog.Infof("服务器已完全关闭")
}
