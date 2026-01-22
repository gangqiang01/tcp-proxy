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

	listenAddr := flag.String("listen", ":8080", "监听地址")
	repeaterAddr := flag.String("repeater", "localhost:9090", "Repeater服务器地址")
	repeaterTLS := flag.Bool("repeater-tls", false, "是否启用与Repeater服务器的TLS连接")
	maxConnections := flag.Int("max-conn", 1000, "最大连接数")
	readTimeout := flag.Duration("read-timeout", 10*time.Second, "读取超时")
	writeTimeout := flag.Duration("write-timeout", 10*time.Second, "写入超时")
	bufferSize := flag.Int("buffer", 1024*1024*4, "缓冲区大小")
	logFile := flag.Bool("log-file", true, "是否将日志输出到文件")
	flag.Parse()
	if *logFile {

		logFile := os.Args[0] + ".log"
		flag.Set("log_file", logFile)
		flag.Set("log_file_max_size", "50") //in MB, default as 1800MB
		flag.Set("logtostderr", "false")
		flag.Set("alsologtostderr", "false")
	} else {
		flag.Set("logtostderr", "true")
		flag.Set("alsologtostderr", "false")
	}

	// flag.Set("v", "2")
	logs.InitLogs()
	defer logs.FlushLogs()

	programName := filepath.Base(os.Args[0])
	programName = strings.TrimSuffix(programName, filepath.Ext(programName))

	klog.Infof("TCP-PROXY 程序: %s", programName)
	klog.Infof("TCP-PROXY 参数: %v", flag.Args())
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
