package sctp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	isctp "github.com/ishidawataru/sctp"

	"mock5g/pkg/transport"
)

type listener struct {
	ln  *isctp.SCTPListener
	cfg transport.Config
}

type session struct {
	cfg      transport.Config
	conn     *isctp.SCTPConn
	rw       net.Conn
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
	return &listener{ln: ln, cfg: cfg}, nil
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
		s := &session{conn: res.conn, rw: res.conn, cfg: l.cfg}
		s.applyTunables()
		if s.cfg.TLS {
			if err := s.enableServerTLS(ctx); err != nil {
				_ = s.conn.Close()
				return nil, err
			}
		}
		return s, nil
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
		s := &session{cfg: cfg, conn: res.conn, rw: res.conn}
		s.applyTunables()
		if s.cfg.TLS {
			if err := s.enableClientTLS(ctx); err != nil {
				_ = s.conn.Close()
				return nil, err
			}
		}
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
	return s.rw.Close()
}

func (s *session) RemoteAddr() string {
	ra := s.rw.RemoteAddr()
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
		_ = c.s.rw.SetWriteDeadline(deadline)
	} else {
		_ = c.s.rw.SetWriteDeadline(time.Time{})
	}
	_, err := c.s.rw.Write(payload)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return context.DeadlineExceeded
	}
	return err
}

func (c *channel) Recv(ctx context.Context) ([]byte, error) {
	c.s.readMu.Lock()
	defer c.s.readMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.s.rw.SetReadDeadline(deadline)
	} else {
		_ = c.s.rw.SetReadDeadline(time.Time{})
	}
	buf := make([]byte, 64*1024)
	n, err := c.s.rw.Read(buf)
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

func (s *session) enableServerTLS(ctx context.Context) error {
	cfg, err := sctpServerTLSConfig(s.cfg)
	if err != nil {
		return err
	}
	tc := tls.Server(s.conn, cfg)
	if err := handshakeWithContext(ctx, tc, s.cfg.ConnectTimeout); err != nil {
		return fmt.Errorf("sctp tls server handshake: %w", err)
	}
	s.rw = tc
	return nil
}

func (s *session) enableClientTLS(ctx context.Context) error {
	cfg, err := sctpClientTLSConfig(s.cfg)
	if err != nil {
		return err
	}
	tc := tls.Client(s.conn, cfg)
	if err := handshakeWithContext(ctx, tc, s.cfg.ConnectTimeout); err != nil {
		return fmt.Errorf("sctp tls client handshake: %w", err)
	}
	s.rw = tc
	return nil
}

func handshakeWithContext(ctx context.Context, tc *tls.Conn, timeout time.Duration) error {
	hctx := ctx
	cancel := func() {}
	if timeout > 0 {
		hctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	return tc.HandshakeContext(hctx)
}

func sctpServerTLSConfig(cfg transport.Config) (*tls.Config, error) {
	alpn := defaultALPN(cfg)
	var cert tls.Certificate
	var err error
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, fmt.Errorf("both cert_file and key_file are required")
		}
		cert, err = tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load x509 key pair: %w", err)
		}
	} else {
		cert, err = generateSelfSignedCert(pickIP(cfg.LocalIP, "127.0.0.1"))
		if err != nil {
			return nil, err
		}
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}
	if strings.TrimSpace(alpn) != "" {
		tlsCfg.NextProtos = []string{alpn}
	}
	return tlsCfg, nil
}

func sctpClientTLSConfig(cfg transport.Config) (*tls.Config, error) {
	alpn := defaultALPN(cfg)
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: cfg.RemoteIP,
	}
	if strings.TrimSpace(alpn) != "" {
		tlsCfg.NextProtos = []string{alpn}
	}
	if cfg.CAFile == "" {
		tlsCfg.InsecureSkipVerify = true
		return tlsCfg, nil
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read ca file: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("append CA cert failed")
	}
	tlsCfg.RootCAs = roots
	return tlsCfg, nil
}

func defaultALPN(cfg transport.Config) string {
	if strings.TrimSpace(cfg.ALPN) != "" {
		return cfg.ALPN
	}
	return "mock5g-v1"
}

func pickIP(ip, fallback string) string {
	if strings.TrimSpace(ip) == "" {
		return fallback
	}
	return ip
}

func generateSelfSignedCert(host string) (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate rsa key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "mock5g-sctp-tls",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else if host != "" {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("x509 key pair: %w", err)
	}
	return cert, nil
}
