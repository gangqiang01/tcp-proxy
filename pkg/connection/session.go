package connection

import (
	"net"
	"sync"
	"time"

	"github.com/gangqiang01/tcp-proxy/pkg/protocol"
)

// Session 客户端会话
type Session struct {
	ID             string
	ClientConn     net.Conn
	RepeaterConn   net.Conn
	RepeaterAddr   string
	CreatedAt      time.Time
	LastActive     time.Time
	DataToRepeater chan *protocol.Message
	DataToClient   chan *protocol.Message
	Mu             sync.RWMutex
	StopChan       chan struct{}
	Wg             sync.WaitGroup
	Stats          *SessionStats
}

// SessionStats 会话统计
type SessionStats struct {
	BytesSent        int64
	BytesReceived    int64
	MessagesSent     int64
	MessagesReceived int64
	Mu               sync.RWMutex
}

// NewSession 创建新会话
func NewSession(id string, clientConn net.Conn, repeaterAddr string) *Session {
	return &Session{
		ID:             id,
		ClientConn:     clientConn,
		RepeaterAddr:   repeaterAddr,
		CreatedAt:      time.Now(),
		LastActive:     time.Now(),
		DataToRepeater: make(chan *protocol.Message, 100),
		DataToClient:   make(chan *protocol.Message, 100),
		StopChan:       make(chan struct{}),
		Stats:          &SessionStats{},
	}
}

// ConnectToRepeater 连接到Repeater服务器
func (s *Session) ConnectToRepeater() error {
	conn, err := net.DialTimeout("tcp", s.RepeaterAddr, 10*time.Second)
	if err != nil {
		return err
	}
	// tlsConfig := &tls.Config{
	// 	// InsecureSkipVerify controls whether a client verifies the server's
	// 	// certificate chain and host name. If InsecureSkipVerify is true, crypto/tls
	// 	// accepts any certificate presented by the server and any host name in that
	// 	// certificate. In this mode, TLS is susceptible to machine-in-the-middle
	// 	// attacks unless custom verification is used. This should be used only for
	// 	// testing or in combination with VerifyConnection or VerifyPeerCertificate.
	// 	InsecureSkipVerify: true,
	// }
	// conn, err := tls.Dial("tcp",  s.RepeaterAddr, tlsConfig)
	// if err != nil {
	// 	klog.V(4).Infof("Unable to connect to: %s, error: %s\n", s.RepeaterAddr, err.Error())
	// 	return err
	// }

	s.Mu.Lock()
	s.RepeaterConn = conn
	s.Mu.Unlock()

	return nil
}

// Close 关闭会话
func (s *Session) Close() {
	close(s.StopChan)

	s.Mu.Lock()
	if s.ClientConn != nil {
		s.ClientConn.Close()
	}
	if s.RepeaterConn != nil {
		s.RepeaterConn.Close()
	}
	s.Mu.Unlock()

	s.Wg.Wait()
}

// UpdateActivity 更新活动时间
func (s *Session) UpdateActivity() {
	s.Mu.Lock()
	s.LastActive = time.Now()
	s.Mu.Unlock()
}

// GetStats 获取统计信息
func (s *Session) GetStats() SessionStats {
	s.Stats.Mu.RLock()
	defer s.Stats.Mu.RUnlock()
	return *s.Stats
}
