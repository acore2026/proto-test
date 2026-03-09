package backend

import (
	"context"
	"fmt"

	"mock5g/pkg/transport"
	"mock5g/pkg/transport/quic"
	"mock5g/pkg/transport/sctp"
)

func Listen(ctx context.Context, cfg transport.Config) (transport.Listener, error) {
	switch cfg.Type {
	case transport.TypeSCTP:
		return sctp.Listen(ctx, cfg)
	case transport.TypeQUIC:
		return quic.Listen(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown transport type %q", cfg.Type)
	}
}

func Dial(ctx context.Context, cfg transport.Config) (transport.Session, error) {
	switch cfg.Type {
	case transport.TypeSCTP:
		return sctp.Dial(ctx, cfg)
	case transport.TypeQUIC:
		return quic.Dial(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown transport type %q", cfg.Type)
	}
}
