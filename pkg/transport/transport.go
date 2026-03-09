package transport

import (
	"context"
	"errors"
	"time"
)

type Type string

const (
	TypeSCTP Type = "sctp"
	TypeQUIC Type = "quic"
)

type Config struct {
	Type Type `yaml:"type"`

	LocalIP   string `yaml:"local_ip"`
	LocalPort int    `yaml:"local_port"`
	RemoteIP  string `yaml:"remote_ip"`
	RemotePort int   `yaml:"remote_port"`

	ChannelCount int  `yaml:"channel_count"`
	NoDelay      bool `yaml:"nodelay"`
	HeartbeatMS  int  `yaml:"heartbeat_ms"`
	ReadBuffer   int  `yaml:"read_buffer"`
	WriteBuffer  int  `yaml:"write_buffer"`

	ConnectTimeout time.Duration `yaml:"connect_timeout"`

	// Reserved for a future QUIC backend; accepted in v1 and ignored by SCTP.
	ALPN     string `yaml:"alpn"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	CAFile   string `yaml:"ca_file"`
}

type Listener interface {
	Accept(context.Context) (Session, error)
	Close() error
	Addr() string
}

type Session interface {
	OpenChannel(context.Context, uint16) (Channel, error)
	AcceptChannel(context.Context) (Channel, error)
	Close() error
	RemoteAddr() string
}

type Channel interface {
	ID() uint16
	Send(context.Context, []byte) error
	Recv(context.Context) ([]byte, error)
	Close() error
}

var ErrNotImplemented = errors.New("transport backend not implemented")
