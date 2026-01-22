package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"
)

// Server TCP代理服务器
type Server struct {
	Config       *Config
	listener     net.Listener
	sessions     map[string]*Session
	sessionMutex sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	stats        *ServerStats
	isRunning    atomic.Bool
	wg           sync.WaitGroup
	errors       chan error  // 错误通道，用于收集运行时错误
	tlsConfig    *tls.Config // TLS配置
}

// Config 服务器配置
type Config struct {
	ListenAddr     string
	RepeaterAddr   string
	RepeaterTLS    bool
	MaxConnections int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	BufferSize     int
	IdleTimeout    time.Duration // 空闲超时时间
	TLSConfig      *tls.Config   // TLS配置，可选
}

// Session 会话
type Session struct {
	ID           string
	ClientConn   net.Conn
	ClientAddr   string
	RepeaterConn net.Conn
	CreatedAt    time.Time
	LastActive   time.Time
	Closed       bool
	Mu           sync.RWMutex
	StopChan     chan struct{}
}

// ServerStats 服务器统计
type ServerStats struct {
	TotalConnections  atomic.Int64
	ActiveConnections atomic.Int64
	BytesForwarded    atomic.Int64
	FailedConnections atomic.Int64
}

// NewServer 创建新服务器
func NewServer(cfg *Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	// 设置默认值
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1024 * 1024 * 4 // 4MB
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 1000
	}

	// 设置TLS配置
	tlsConfig := cfg.TLSConfig
	if cfg.RepeaterTLS && tlsConfig == nil {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true, // 仅用于测试，生产环境应该验证证书
		}
	}

	return &Server{
		Config:    cfg,
		sessions:  make(map[string]*Session),
		ctx:       ctx,
		cancel:    cancel,
		stats:     &ServerStats{},
		errors:    make(chan error, 100),
		tlsConfig: tlsConfig,
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	var err error
	s.listener, err = net.Listen("tcp", s.Config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	s.isRunning.Store(true)

	klog.Infof("TCP Proxy Server started on %s", s.Config.ListenAddr)
	klog.Infof("Repeater target: %s", s.Config.RepeaterAddr)
	klog.Infof("Repeater TLS: %v", s.Config.RepeaterTLS)
	klog.Infof("Max connections: %d", s.Config.MaxConnections)
	klog.Infof("Read timeout: %v, Write timeout: %v", s.Config.ReadTimeout, s.Config.WriteTimeout)
	klog.Infof("Buffer size: %d bytes", s.Config.BufferSize)
	klog.Infof("Idle timeout: %v", s.Config.IdleTimeout)

	// 启动错误处理协程
	s.wg.Add(1)
	go s.errorHandler()

	// 启动会话清理协程
	s.wg.Add(1)
	go s.sessionCleanupWorker()

	// 启动统计打印协程
	s.wg.Add(1)
	go s.statsWorker()

	// 启动心跳协程
	s.wg.Add(1)
	go s.heartbeatWorker()

	// 接受连接
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptConnections()
	}()

	return nil
}

// connectToRepeater 连接到Repeater服务器
func (s *Server) connectToRepeater() (net.Conn, error) {
	if s.Config.RepeaterTLS {
		klog.V(4).Infof("Connecting to repeater with TLS: %s", s.Config.RepeaterAddr)
		return tls.DialWithDialer(&net.Dialer{
			Timeout: 10 * time.Second,
		}, "tcp", s.Config.RepeaterAddr, s.tlsConfig)
	} else {
		klog.V(4).Infof("Connecting to repeater without TLS: %s", s.Config.RepeaterAddr)
		return net.DialTimeout("tcp", s.Config.RepeaterAddr, 10*time.Second)
	}
}

// errorHandler 错误处理器
func (s *Server) errorHandler() {
	defer s.wg.Done()

	for {
		select {
		case err := <-s.errors:
			if err != nil {
				klog.Errorf("Runtime error: %v", err)
			}
		case <-s.ctx.Done():
			return
		}
	}
}

// acceptConnections 接受客户端连接
func (s *Server) acceptConnections() {
	defer func() {
		if r := recover(); r != nil {
			s.errors <- fmt.Errorf("panic in acceptConnections: %v", r)
		}
	}()

	for s.isRunning.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if !s.isRunning.Load() {
				return
			}

			// 检查是否是临时错误
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				klog.Warningf("Temporary accept error: %v", err)
				time.Sleep(5 * time.Millisecond)
				continue
			}

			// 严重错误，发送到错误通道
			s.errors <- fmt.Errorf("failed to accept connection: %w", err)
			continue
		}

		// 检查当前连接数
		if int(s.stats.ActiveConnections.Load()) >= s.Config.MaxConnections {
			klog.Warningf("Maximum connections (%d) reached, rejecting new connection from %s",
				s.Config.MaxConnections, conn.RemoteAddr())
			safeCloseConn(conn)
			continue
		}

		// 处理连接
		s.wg.Add(1)
		go func(c net.Conn) {
			defer func() {
				s.wg.Done()
				if r := recover(); r != nil {
					s.errors <- fmt.Errorf("panic in handleConnection: %v", r)
				}
			}()
			s.handleConnection(c)
		}(conn)
	}
}

// handleConnection 处理客户端连接
func (s *Server) handleConnection(clientConn net.Conn) {
	// 生成会话ID
	sessionID := generateSessionID()
	clientAddr := clientConn.RemoteAddr().String()

	klog.Infof("Session %s: new connection from %s", sessionID, clientAddr)

	// 创建会话
	session := &Session{
		ID:         sessionID,
		ClientConn: clientConn,
		ClientAddr: clientAddr,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Closed:     false,
		StopChan:   make(chan struct{}, 1), // 使用缓冲通道避免阻塞
	}

	// 注册会话
	s.registerSession(session)
	defer func() {
		s.unregisterSession(session.ID)
		if r := recover(); r != nil {
			s.errors <- fmt.Errorf("panic in session cleanup for %s: %v", sessionID, r)
		}
	}()

	// 连接到Repeater
	repeaterConn, err := s.connectToRepeater()
	if err != nil {
		klog.Errorf("Session %s: failed to connect to repeater: %v", session.ID, err)
		s.stats.FailedConnections.Add(1)
		return
	}
	session.RepeaterConn = repeaterConn

	klog.Infof("Session %s: connection established, client %s <-> repeater %s (TLS: %v)",
		session.ID, clientAddr, s.Config.RepeaterAddr, s.Config.RepeaterTLS)

	// 启动双向数据转发
	var wg sync.WaitGroup
	wg.Add(2)

	// 从客户端转发到repeater
	go func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				s.errors <- fmt.Errorf("panic in client->repeater forward for %s: %v", sessionID, r)
			}
		}()
		s.forwardData(session, session.ClientConn, session.RepeaterConn, "client->repeater")
	}()

	// 从repeater转发到客户端
	go func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				s.errors <- fmt.Errorf("panic in repeater->client forward for %s: %v", sessionID, r)
			}
		}()
		s.forwardData(session, session.RepeaterConn, session.ClientConn, "repeater->client")
	}()

	// 等待转发完成
	wg.Wait()

	klog.Infof("Session %s: connection closed", session.ID)
}

// forwardData 转发数据
func (s *Server) forwardData(session *Session, src net.Conn, dst net.Conn, direction string) {
	defer func() {
		if r := recover(); r != nil {
			s.errors <- fmt.Errorf("panic in forwardData for session %s: %v", session.ID, r)
		}
		// 通知另一个方向的转发停止
		select {
		case session.StopChan <- struct{}{}:
			klog.V(4).Infof("Session %s: sent stop signal for %s", session.ID, direction)
		default:
			// 通道已满或已关闭，忽略
		}
	}()

	buffer := make([]byte, s.Config.BufferSize)
	bytesRead := 0

	for {
		select {
		case <-session.StopChan:
			klog.V(4).Infof("Session %s: %s received stop signal", session.ID, direction)
			return
		case <-s.ctx.Done():
			klog.V(4).Infof("Session %s: %s context done", session.ID, direction)
			return
		default:
			// 设置读取超时
			if err := src.SetReadDeadline(time.Now().Add(s.Config.ReadTimeout)); err != nil {
				klog.Warningf("Session %s: failed to set read deadline: %v", session.ID, err)
			}

			// 从源读取数据
			n, err := src.Read(buffer)
			if err != nil {
				// 检查是否是预期中的错误
				if isEOF(err) {
					klog.Infof("Session %s: %s connection closed by peer", session.ID, direction)
				} else if isTimeout(err) {
					// 超时，检查是否需要继续
					klog.V(4).Infof("Session %s: %s read timeout", session.ID, direction)
					session.Mu.Lock()
					session.LastActive = time.Now()
					session.Mu.Unlock()
					continue
				} else if isClosedError(err) {
					klog.Infof("Session %s: %s connection closed", session.ID, direction)
				} else {
					klog.Errorf("Session %s: error reading from %s: %v", session.ID, direction, err)
				}
				return
			}

			if n > 0 {
				bytesRead += n

				// 更新活动时间
				session.Mu.Lock()
				session.LastActive = time.Now()
				session.Mu.Unlock()

				// 设置写入超时
				if err := dst.SetWriteDeadline(time.Now().Add(s.Config.WriteTimeout)); err != nil {
					klog.Warningf("Session %s: failed to set write deadline: %v", session.ID, err)
				}

				// 写入数据到目标
				written, err := dst.Write(buffer[:n])
				if err != nil {
					klog.Errorf("Session %s: error writing to %s: %v", session.ID, direction, err)
					return
				}

				// 更新统计
				s.stats.BytesForwarded.Add(int64(written))

				klog.V(5).Infof("Session %s: %s forwarded %d bytes (total: %d)",
					session.ID, direction, written, bytesRead)
			}
		}
	}
}

// 辅助函数：检查错误类型
func isEOF(err error) bool {
	return err.Error() == "EOF"
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

func isClosedError(err error) bool {
	return err.Error() == "use of closed network connection"
}

// registerSession 注册会话
func (s *Server) registerSession(session *Session) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	s.sessions[session.ID] = session
	s.stats.ActiveConnections.Add(1)
	s.stats.TotalConnections.Add(1)
}

// unregisterSession 注销会话
func (s *Server) unregisterSession(sessionID string) {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		// 标记为已关闭
		session.Mu.Lock()
		session.Closed = true
		session.Mu.Unlock()

		// 安全关闭连接
		safeCloseConn(session.RepeaterConn)
		safeCloseConn(session.ClientConn)

		// 关闭停止通道
		close(session.StopChan)

		delete(s.sessions, sessionID)
		s.stats.ActiveConnections.Add(-1)

		klog.V(4).Infof("Session %s: unregistered", sessionID)
	}
}

// safeCloseConn 安全关闭连接
func safeCloseConn(conn net.Conn) {
	if conn != nil {
		if err := conn.Close(); err != nil && !isClosedError(err) {
			klog.Warningf("Error closing connection: %v", err)
		}
	}
}

// sessionCleanupWorker 会话清理协程
func (s *Server) sessionCleanupWorker() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupInactiveSessions()
		case <-s.ctx.Done():
			return
		}
	}
}

// cleanupInactiveSessions 清理不活跃的会话
func (s *Server) cleanupInactiveSessions() {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		session.Mu.RLock()
		inactiveDuration := now.Sub(session.LastActive)
		closed := session.Closed
		session.Mu.RUnlock()

		// 检查是否超时或已关闭
		if inactiveDuration > s.Config.IdleTimeout && !closed {
			klog.Infof("Session %s: cleaning up due to inactivity (%v)", id, inactiveDuration)

			// 标记为已关闭
			session.Mu.Lock()
			session.Closed = true
			session.Mu.Unlock()

			// 安全关闭连接
			safeCloseConn(session.RepeaterConn)
			safeCloseConn(session.ClientConn)

			// 关闭停止通道
			close(session.StopChan)

			delete(s.sessions, id)
			s.stats.ActiveConnections.Add(-1)
		}
	}
}

// statsWorker 统计工作协程
func (s *Server) statsWorker() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.printStats()
		case <-s.ctx.Done():
			return
		}
	}
}

// printStats 打印统计信息
func (s *Server) printStats() {
	s.sessionMutex.RLock()
	sessionCount := len(s.sessions)
	s.sessionMutex.RUnlock()

	klog.Infof("Stats - Sessions: %d, Active: %d, Total: %d, Failed: %d, Bytes: %d, TLS: %v",
		sessionCount,
		s.stats.ActiveConnections.Load(),
		s.stats.TotalConnections.Load(),
		s.stats.FailedConnections.Load(),
		s.stats.BytesForwarded.Load(),
		s.Config.RepeaterTLS)
}

// heartbeatWorker 心跳协程，保持连接活跃
func (s *Server) heartbeatWorker() {
	defer s.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkConnections()
		case <-s.ctx.Done():
			return
		}
	}
}

// checkConnections 检查连接健康状况
func (s *Server) checkConnections() {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()

	for _, session := range s.sessions {
		session.Mu.RLock()
		lastActive := session.LastActive
		closed := session.Closed
		session.Mu.RUnlock()

		if !closed && time.Since(lastActive) > 2*s.Config.ReadTimeout {
			klog.Warningf("Session %s: connection appears to be stale, last active %v ago",
				session.ID, time.Since(lastActive))
		}
	}
}

// Stop 停止服务器
func (s *Server) Stop() {
	if s.isRunning.CompareAndSwap(true, false) {
		klog.Info("Stopping TCP proxy server...")

		// 取消上下文
		s.cancel()

		// 停止监听
		if s.listener != nil {
			if err := s.listener.Close(); err != nil && !isClosedError(err) {
				klog.Errorf("Error closing listener: %v", err)
			}
		}

		// 关闭所有会话
		s.sessionMutex.Lock()
		for id, session := range s.sessions {
			klog.Infof("Closing session: %s", id)

			session.Mu.Lock()
			session.Closed = true
			session.Mu.Unlock()

			// 安全关闭连接
			safeCloseConn(session.RepeaterConn)
			safeCloseConn(session.ClientConn)

			// 关闭停止通道
			close(session.StopChan)
		}
		s.sessions = make(map[string]*Session)
		s.sessionMutex.Unlock()

		// 等待所有goroutine完成
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			klog.Info("All goroutines stopped")
		case <-time.After(10 * time.Second):
			klog.Warning("Timeout waiting for goroutines to stop")
			// 尝试获取剩余的goroutine数量（通过debug信息）
			klog.Infof("Server may not have stopped cleanly")
		}

		// 关闭错误通道
		close(s.errors)

		klog.Info("TCP proxy server stopped")
	}
}

// GetStats 获取当前统计信息（用于监控）
func (s *Server) GetStats() map[string]interface{} {
	s.sessionMutex.RLock()
	sessionCount := len(s.sessions)
	s.sessionMutex.RUnlock()

	return map[string]interface{}{
		"sessions":          sessionCount,
		"activeConnections": s.stats.ActiveConnections.Load(),
		"totalConnections":  s.stats.TotalConnections.Load(),
		"failedConnections": s.stats.FailedConnections.Load(),
		"bytesForwarded":    s.stats.BytesForwarded.Load(),
		"isRunning":         s.isRunning.Load(),
		"repeaterTLS":       s.Config.RepeaterTLS,
		"repeaterAddr":      s.Config.RepeaterAddr,
	}
}

// generateSessionID 生成会话ID
func generateSessionID() string {
	return fmt.Sprintf("sess-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&sequence, 1))
}

var sequence uint64 = 0

// GetActiveSessions 获取活跃会话列表（用于调试和监控）
func (s *Server) GetActiveSessions() []string {
	s.sessionMutex.RLock()
	defer s.sessionMutex.RUnlock()

	sessions := make([]string, 0, len(s.sessions))
	for id, session := range s.sessions {
		session.Mu.RLock()
		info := fmt.Sprintf("%s (from %s, active: %v ago)",
			id, session.ClientAddr, time.Since(session.LastActive))
		session.Mu.RUnlock()
		sessions = append(sessions, info)
	}
	return sessions
}
