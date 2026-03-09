//go:build !linux

package sctpkernel

import (
	"context"
	"fmt"

	"mock5g/pkg/transport"
)

func Listen(context.Context, transport.Config) (transport.Listener, error) {
	return nil, fmt.Errorf("sctp-kernel transport is only supported on linux")
}

func Dial(context.Context, transport.Config) (transport.Session, error) {
	return nil, fmt.Errorf("sctp-kernel transport is only supported on linux")
}
