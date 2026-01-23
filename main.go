package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gangqiang01/tcp-proxy/cmd"
	"github.com/gangqiang01/tcp-proxy/internal/proxy"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
)

func main() {
	execPath, _ := os.Getwd()

	// 创建独立的 flag 集，避免与 klog 的全局 flag 冲突
	myFlagSet := flag.NewFlagSet("tcp-proxy", flag.ExitOnError)

	// 定义程序自己的参数
	listenAddr := myFlagSet.String("listen", ":8080", "监听地址")
	repeaterAddr := myFlagSet.String("repeater", "localhost:9090", "Repeater服务器地址")
	repeaterTLS := myFlagSet.Bool("repeater-tls", false, "是否启用与Repeater服务器的TLS连接")
	maxConnections := myFlagSet.Int("max-conn", 1000, "最大连接数")
	readTimeout := myFlagSet.Duration("read-timeout", 10*time.Second, "读取超时")
	writeTimeout := myFlagSet.Duration("write-timeout", 10*time.Second, "写入超时")
	bufferSize := myFlagSet.Int("buffer", 1024*1024*4, "缓冲区大小")
	logFile := myFlagSet.Bool("log-file", true, "是否将日志输出到文件")

	// 解析参数，但只解析我们自己的参数
	// 这样 klog 的参数仍然可以通过全局 flag 访问
	myFlagSet.Parse(os.Args[1:])

	// 检查是否请求帮助
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "-help" || os.Args[1] == "--help") {
		myFlagSet.Usage()
		os.Exit(0)
	}

	// 现在处理日志相关的参数
	// 因为 klog 的 flag 在全局 flag 集中，我们需要再次解析
	flag.Parse()

	// 设置日志输出
	if *logFile {
		logFileName := os.Args[0] + ".log"
		flag.Set("log_file", logFileName)
		flag.Set("log_file_max_size", "50") // in MB
		flag.Set("logtostderr", "false")
		flag.Set("alsologtostderr", "false")
	} else {
		flag.Set("logtostderr", "true")
		flag.Set("alsologtostderr", "false")
	}

	// 初始化日志
	logs.InitLogs()
	defer logs.FlushLogs()

	programName := filepath.Base(os.Args[0])
	programName = strings.TrimSuffix(programName, filepath.Ext(programName))

	klog.Infof("TCP-PROXY 程序: %s", programName)
	klog.Infof("TCP-PROXY 参数: %v", os.Args[1:])
	klog.Infof("TCP-PROXY 工作目录: %s", execPath)

	cfg := &proxy.Config{
		ListenAddr:     *listenAddr,
		RepeaterAddr:   *repeaterAddr,
		RepeaterTLS:    *repeaterTLS,
		MaxConnections: *maxConnections,
		ReadTimeout:    *readTimeout,
		WriteTimeout:   *writeTimeout,
		BufferSize:     *bufferSize,
	}

	cmd.Run(cfg)
}
