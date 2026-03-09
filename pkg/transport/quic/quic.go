package quic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	q "github.com/quic-go/quic-go"

	"mock5g/pkg/transport"
)

const maxQUICFrameSize = 16 * 1024 * 1024

type listener struct {
	ln *q.Listener
}

type session struct {
	conn *q.Conn
	once sync.Once
}

type channel struct {
	id     uint16
	stream *q.Stream
}

func Listen(_ context.Context, cfg transport.Config) (transport.Listener, error) {
	addr := fmt.Sprintf("%s:%d", pickIP(cfg.LocalIP, "0.0.0.0"), cfg.LocalPort)
	tlsCfg, err := serverTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	qcfg := quicConfig(cfg)
	ln, err := q.ListenAddr(addr, tlsCfg, qcfg)
	if err != nil {
		return nil, fmt.Errorf("listen quic: %w", err)
	}
	return &listener{ln: ln}, nil
}

func Dial(ctx context.Context, cfg transport.Config) (transport.Session, error) {
	if cfg.RemoteIP == "" || cfg.RemotePort == 0 {
		return nil, fmt.Errorf("remote_ip and remote_port are required for quic dial")
	}
	addr := fmt.Sprintf("%s:%d", cfg.RemoteIP, cfg.RemotePort)
	tlsCfg, err := clientTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := q.DialAddr(ctx, addr, tlsCfg, quicConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("dial quic: %w", err)
	}
	return &session{conn: conn}, nil
}

func (l *listener) Accept(ctx context.Context) (transport.Session, error) {
	conn, err := l.ln.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return &session{conn: conn}, nil
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

func (s *session) OpenChannel(ctx context.Context, id uint16) (transport.Channel, error) {
	st, err := s.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return &channel{id: id, stream: st}, nil
}

func (s *session) AcceptChannel(ctx context.Context) (transport.Channel, error) {
	st, err := s.conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	chID := uint16(uint64(st.StreamID()) & 0xFFFF)
	return &channel{id: chID, stream: st}, nil
}

func (s *session) Close() error {
	s.once.Do(func() {
		_ = s.conn.CloseWithError(0, "")
	})
	return nil
}

func (s *session) RemoteAddr() string {
	if s.conn.RemoteAddr() == nil {
		return ""
	}
	return s.conn.RemoteAddr().String()
}

func (c *channel) ID() uint16 {
	return c.id
}

func (c *channel) Send(ctx context.Context, payload []byte) error {
	if len(payload) > maxQUICFrameSize {
		return fmt.Errorf("payload too large: %d", len(payload))
	}
	if err := setWriteDeadline(c.stream, ctx); err != nil {
		return err
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writeAll(c.stream, header[:]); err != nil {
		return err
	}
	if _, err := writeAll(c.stream, payload); err != nil {
		return err
	}
	return nil
}

func (c *channel) Recv(ctx context.Context) ([]byte, error) {
	if err := setReadDeadline(c.stream, ctx); err != nil {
		return nil, err
	}
	var header [4]byte
	if _, err := io.ReadFull(c.stream, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > maxQUICFrameSize {
		return nil, fmt.Errorf("incoming payload too large: %d", size)
	}
	buf := make([]byte, int(size))
	if _, err := io.ReadFull(c.stream, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (c *channel) Close() error {
	return c.stream.Close()
}

func quicConfig(cfg transport.Config) *q.Config {
	qcfg := &q.Config{
		EnableDatagrams:                false,
		DisablePathMTUDiscovery:        true,
		HandshakeIdleTimeout:           3 * time.Second,
		InitialStreamReceiveWindow:     64 * 1024,
		MaxStreamReceiveWindow:         256 * 1024,
		InitialConnectionReceiveWindow: 256 * 1024,
		MaxConnectionReceiveWindow:     1024 * 1024,
		MaxIncomingUniStreams:          -1,
	}
	if cfg.HeartbeatMS > 0 {
		qcfg.KeepAlivePeriod = time.Duration(cfg.HeartbeatMS) * time.Millisecond
	}
	if cfg.ChannelCount > 0 {
		qcfg.MaxIncomingStreams = int64(cfg.ChannelCount * 4)
	}
	return qcfg
}

func defaultALPN(cfg transport.Config) string {
	if strings.TrimSpace(cfg.ALPN) != "" {
		return cfg.ALPN
	}
	return "mock5g-v1"
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
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{alpn},
		Certificates: []tls.Certificate{cert},
	}, nil
}

func clientTLSConfig(cfg transport.Config) (*tls.Config, error) {
	alpn := defaultALPN(cfg)
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{alpn},
		ServerName: cfg.RemoteIP,
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
			CommonName: "mock5g-quic",
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

func pickIP(ip, fallback string) string {
	if strings.TrimSpace(ip) == "" {
		return fallback
	}
	return ip
}

func setReadDeadline(st *q.Stream, ctx context.Context) error {
	if d, ok := ctx.Deadline(); ok {
		return st.SetReadDeadline(d)
	}
	return st.SetReadDeadline(time.Time{})
}

func setWriteDeadline(st *q.Stream, ctx context.Context) error {
	if d, ok := ctx.Deadline(); ok {
		return st.SetWriteDeadline(d)
	}
	return st.SetWriteDeadline(time.Time{})
}

func writeAll(w io.Writer, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := w.Write(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
