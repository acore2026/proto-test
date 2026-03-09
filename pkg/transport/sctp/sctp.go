package sctp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	isctp "github.com/ishidawataru/sctp"

	"mock5g/pkg/transport"
)

type listener struct {
	ln *isctp.SCTPListener
}

type session struct {
	cfg      transport.Config
	conn     *isctp.SCTPConn
	readMu   sync.Mutex
	writeMu  sync.Mutex
	closedMu sync.Mutex
	closed   bool
}

type channel struct {
	id uint16
	s  *session
}

func Listen(_ context.Context, cfg transport.Config) (transport.Listener, error) {
	addr, err := isctp.ResolveSCTPAddr("sctp", fmt.Sprintf("%s:%d", cfg.LocalIP, cfg.LocalPort))
	if err != nil {
		return nil, fmt.Errorf("resolve sctp listen addr: %w", err)
	}
	ln, err := isctp.ListenSCTP("sctp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen sctp: %w", err)
	}
	return &listener{ln: ln}, nil
}

func (l *listener) Accept(ctx context.Context) (transport.Session, error) {
	type result struct {
		conn *isctp.SCTPConn
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		c, err := l.ln.AcceptSCTP()
		resCh <- result{conn: c, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, res.err
		}
		return &session{conn: res.conn, cfg: transport.Config{}}, nil
	}
}

func (l *listener) Close() error {
	return l.ln.Close()
}

func (l *listener) Addr() string {
	if l.ln.Addr() == nil {
		return ""
	}
	return l.ln.Addr().String()
}

func Dial(ctx context.Context, cfg transport.Config) (transport.Session, error) {
	laddr, err := resolveOptional(cfg.LocalIP, cfg.LocalPort)
	if err != nil {
		return nil, err
	}
	raddr, err := isctp.ResolveSCTPAddr("sctp", fmt.Sprintf("%s:%d", cfg.RemoteIP, cfg.RemotePort))
	if err != nil {
		return nil, fmt.Errorf("resolve sctp remote addr: %w", err)
	}

	type result struct {
		conn *isctp.SCTPConn
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		c, e := isctp.DialSCTP("sctp", laddr, raddr)
		resCh <- result{conn: c, err: e}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return nil, fmt.Errorf("dial sctp: %w", res.err)
		}
		s := &session{cfg: cfg, conn: res.conn}
		s.applyTunables()
		return s, nil
	}
}

func resolveOptional(ip string, port int) (*isctp.SCTPAddr, error) {
	if ip == "" && port == 0 {
		return nil, nil
	}
	return isctp.ResolveSCTPAddr("sctp", fmt.Sprintf("%s:%d", ip, port))
}

func (s *session) applyTunables() {
	if s.cfg.ReadBuffer > 0 {
		_ = s.conn.SetReadBuffer(s.cfg.ReadBuffer)
	}
	if s.cfg.WriteBuffer > 0 {
		_ = s.conn.SetWriteBuffer(s.cfg.WriteBuffer)
	}
	// Heartbeat and nodelay are kept in config for API compatibility and can be
	// wired to SCTP socket options where available.
}

func (s *session) OpenChannel(_ context.Context, id uint16) (transport.Channel, error) {
	return &channel{id: id, s: s}, nil
}

func (s *session) AcceptChannel(_ context.Context) (transport.Channel, error) {
	return &channel{id: 0, s: s}, nil
}

func (s *session) Close() error {
	s.closedMu.Lock()
	defer s.closedMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close()
}

func (s *session) RemoteAddr() string {
	ra := s.conn.RemoteAddr()
	if ra == nil {
		return ""
	}
	return ra.String()
}

func (c *channel) ID() uint16 { return c.id }

func (c *channel) Send(ctx context.Context, payload []byte) error {
	c.s.writeMu.Lock()
	defer c.s.writeMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.s.conn.SetWriteDeadline(deadline)
	} else {
		_ = c.s.conn.SetWriteDeadline(time.Time{})
	}
	_, err := c.s.conn.Write(payload)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return context.DeadlineExceeded
	}
	return err
}

func (c *channel) Recv(ctx context.Context) ([]byte, error) {
	c.s.readMu.Lock()
	defer c.s.readMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.s.conn.SetReadDeadline(deadline)
	} else {
		_ = c.s.conn.SetReadDeadline(time.Time{})
	}
	buf := make([]byte, 64*1024)
	n, err := c.s.conn.Read(buf)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return nil, context.DeadlineExceeded
	}
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out, nil
}

func (c *channel) Close() error {
	return nil
}
