//go:build linux

package sctpkernel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"mock5g/pkg/transport"
)

type listener struct {
	fd  int
	cfg transport.Config
}

type session struct {
	cfg      transport.Config
	base     *fdConn
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
	family, sa, err := resolveSockAddr(pickIP(cfg.LocalIP, "0.0.0.0"), cfg.LocalPort)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(family, unix.SOCK_STREAM, unix.IPPROTO_SCTP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("set nonblock: %w", err)
	}
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	if cfg.ReadBuffer > 0 {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, cfg.ReadBuffer)
	}
	if cfg.WriteBuffer > 0 {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, cfg.WriteBuffer)
	}
	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind: %w", err)
	}
	if err := unix.Listen(fd, 256); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("listen: %w", err)
	}
	return &listener{fd: fd, cfg: cfg}, nil
}

func (l *listener) Accept(ctx context.Context) (transport.Session, error) {
	for {
		if err := pollFD(ctx, l.fd, unix.POLLIN); err != nil {
			return nil, err
		}
		fd, _, err := unix.Accept(l.fd)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				continue
			}
			return nil, fmt.Errorf("accept: %w", err)
		}
		if err := unix.SetNonblock(fd, true); err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("set nonblock: %w", err)
		}
		base := newFDConn(fd)
		s := &session{cfg: l.cfg, base: base, rw: base}
		if s.cfg.TLS {
			if err := s.enableServerTLS(ctx); err != nil {
				_ = base.Close()
				return nil, err
			}
		}
		return s, nil
	}
}

func (l *listener) Close() error {
	return unix.Close(l.fd)
}

func (l *listener) Addr() string {
	sa, err := unix.Getsockname(l.fd)
	if err != nil {
		return ""
	}
	return sockaddrString(sa)
}

func Dial(ctx context.Context, cfg transport.Config) (transport.Session, error) {
	if cfg.RemoteIP == "" || cfg.RemotePort == 0 {
		return nil, fmt.Errorf("remote_ip and remote_port are required")
	}
	family, raddr, err := resolveSockAddr(cfg.RemoteIP, cfg.RemotePort)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(family, unix.SOCK_STREAM, unix.IPPROTO_SCTP)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("set nonblock: %w", err)
	}
	if cfg.ReadBuffer > 0 {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, cfg.ReadBuffer)
	}
	if cfg.WriteBuffer > 0 {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_SNDBUF, cfg.WriteBuffer)
	}
	if cfg.LocalIP != "" || cfg.LocalPort != 0 {
		_, laddr, err := resolveSockAddr(pickIP(cfg.LocalIP, "0.0.0.0"), cfg.LocalPort)
		if err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		if err := unix.Bind(fd, laddr); err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("bind local: %w", err)
		}
	}

	err = unix.Connect(fd, raddr)
	if err != nil && err != unix.EINPROGRESS {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err == unix.EINPROGRESS {
		if err := pollFD(ctx, fd, unix.POLLOUT); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		soErr, gerr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if gerr != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("getsockopt SO_ERROR: %w", gerr)
		}
		if soErr != 0 {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("connect: %w", unix.Errno(soErr))
		}
	}

	base := newFDConn(fd)
	s := &session{cfg: cfg, base: base, rw: base}
	if s.cfg.TLS {
		if err := s.enableClientTLS(ctx); err != nil {
			_ = base.Close()
			return nil, err
		}
	}
	return s, nil
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
	if s.rw == nil || s.rw.RemoteAddr() == nil {
		return ""
	}
	return s.rw.RemoteAddr().String()
}

func (c *channel) ID() uint16 { return c.id }

func (c *channel) Send(ctx context.Context, payload []byte) error {
	c.s.writeMu.Lock()
	defer c.s.writeMu.Unlock()
	if d, ok := ctx.Deadline(); ok {
		_ = c.s.rw.SetWriteDeadline(d)
	} else {
		_ = c.s.rw.SetWriteDeadline(time.Time{})
	}
	for len(payload) > 0 {
		n, err := c.s.rw.Write(payload)
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return context.DeadlineExceeded
		}
		if err != nil {
			return err
		}
		payload = payload[n:]
	}
	return nil
}

func (c *channel) Recv(ctx context.Context) ([]byte, error) {
	c.s.readMu.Lock()
	defer c.s.readMu.Unlock()
	if d, ok := ctx.Deadline(); ok {
		_ = c.s.rw.SetReadDeadline(d)
	} else {
		_ = c.s.rw.SetReadDeadline(time.Time{})
	}
	header := make([]byte, 26)
	if _, err := io.ReadFull(c.s.rw, header); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, err
	}
	payloadLen := int(binary.BigEndian.Uint16(header[24:26]))
	out := make([]byte, 26+payloadLen)
	copy(out, header)
	if payloadLen == 0 {
		return out, nil
	}
	if _, err := io.ReadFull(c.s.rw, out[26:]); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil, context.DeadlineExceeded
		}
		return nil, err
	}
	return out, nil
}

func (c *channel) Close() error { return nil }

func (s *session) enableServerTLS(ctx context.Context) error {
	cfg, err := serverTLSConfig(s.cfg)
	if err != nil {
		return err
	}
	tc := tls.Server(s.base, cfg)
	if err := handshakeWithContext(ctx, tc, s.cfg.ConnectTimeout); err != nil {
		return fmt.Errorf("sctp-kernel tls server handshake: %w", err)
	}
	s.rw = tc
	return nil
}

func (s *session) enableClientTLS(ctx context.Context) error {
	cfg, err := clientTLSConfig(s.cfg)
	if err != nil {
		return err
	}
	tc := tls.Client(s.base, cfg)
	if err := handshakeWithContext(ctx, tc, s.cfg.ConnectTimeout); err != nil {
		return fmt.Errorf("sctp-kernel tls client handshake: %w", err)
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

func serverTLSConfig(cfg transport.Config) (*tls.Config, error) {
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
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
	if strings.TrimSpace(alpn) != "" {
		tlsCfg.NextProtos = []string{alpn}
	}
	return tlsCfg, nil
}

func clientTLSConfig(cfg transport.Config) (*tls.Config, error) {
	alpn := defaultALPN(cfg)
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: cfg.RemoteIP}
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
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "mock5g-sctp-kernel"},
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

func pickIP(ip, fallback string) string {
	if strings.TrimSpace(ip) == "" {
		return fallback
	}
	return ip
}

func resolveSockAddr(ipStr string, port int) (int, unix.Sockaddr, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, nil, fmt.Errorf("invalid ip %q", ipStr)
	}
	if ip4 := ip.To4(); ip4 != nil {
		sa := &unix.SockaddrInet4{Port: port}
		copy(sa.Addr[:], ip4)
		return unix.AF_INET, sa, nil
	}
	sa := &unix.SockaddrInet6{Port: port}
	copy(sa.Addr[:], ip.To16())
	return unix.AF_INET6, sa, nil
}

func pollFD(ctx context.Context, fd int, events int16) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: events}}
		n, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		re := fds[0].Revents
		if re&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errors.New("poll error")
		}
		if re&events != 0 {
			return nil
		}
	}
}

func sockaddrString(sa unix.Sockaddr) string {
	switch v := sa.(type) {
	case *unix.SockaddrInet4:
		return fmt.Sprintf("%s:%d", net.IP(v.Addr[:]).String(), v.Port)
	case *unix.SockaddrInet6:
		return fmt.Sprintf("[%s]:%d", net.IP(v.Addr[:]).String(), v.Port)
	default:
		return ""
	}
}

type fdConn struct {
	fd      int
	mu      sync.Mutex
	closed  bool
	readDL  time.Time
	writeDL time.Time
}

func newFDConn(fd int) *fdConn { return &fdConn{fd: fd} }

func (c *fdConn) Read(b []byte) (int, error) {
	for {
		if err := c.waitReadable(); err != nil {
			return 0, err
		}
		n, err := unix.Read(c.fd, b)
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}

func (c *fdConn) Write(b []byte) (int, error) {
	for {
		if err := c.waitWritable(); err != nil {
			return 0, err
		}
		n, err := unix.Write(c.fd, b)
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil {
			return 0, err
		}
		return n, nil
	}
}

func (c *fdConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return unix.Close(c.fd)
}

func (c *fdConn) LocalAddr() net.Addr {
	sa, err := unix.Getsockname(c.fd)
	if err != nil {
		return &net.IPAddr{}
	}
	return sctpNetAddr(sa)
}

func (c *fdConn) RemoteAddr() net.Addr {
	sa, err := unix.Getpeername(c.fd)
	if err != nil {
		return &net.IPAddr{}
	}
	return sctpNetAddr(sa)
}

func (c *fdConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDL = t
	c.writeDL = t
	return nil
}

func (c *fdConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDL = t
	return nil
}

func (c *fdConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDL = t
	return nil
}

func (c *fdConn) waitReadable() error { return c.wait(unix.POLLIN, true) }
func (c *fdConn) waitWritable() error { return c.wait(unix.POLLOUT, false) }

func (c *fdConn) wait(events int16, read bool) error {
	for {
		timeout := c.deadlineTimeout(read)
		fds := []unix.PollFd{{Fd: int32(c.fd), Events: events}}
		n, err := unix.Poll(fds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return timeoutErr{}
		}
		re := fds[0].Revents
		if re&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errors.New("poll error")
		}
		if re&events != 0 {
			return nil
		}
	}
}

func (c *fdConn) deadlineTimeout(read bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	var dl time.Time
	if read {
		dl = c.readDL
	} else {
		dl = c.writeDL
	}
	if dl.IsZero() {
		return -1
	}
	d := time.Until(dl)
	if d <= 0 {
		return 0
	}
	ms := int(d / time.Millisecond)
	if ms <= 0 {
		ms = 1
	}
	return ms
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func sctpNetAddr(sa unix.Sockaddr) net.Addr {
	switch v := sa.(type) {
	case *unix.SockaddrInet4:
		ip := make(net.IP, net.IPv4len)
		copy(ip, v.Addr[:])
		return &net.TCPAddr{IP: ip, Port: v.Port}
	case *unix.SockaddrInet6:
		ip := make(net.IP, net.IPv6len)
		copy(ip, v.Addr[:])
		return &net.TCPAddr{IP: ip, Port: v.Port}
	default:
		return &net.TCPAddr{}
	}
}
