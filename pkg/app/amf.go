package app

import (
	"context"
	"fmt"
	"sync"

	"mock5g/pkg/transport"
	"mock5g/pkg/transport/backend"
	"mock5g/pkg/wire"
)

type AMFServer struct {
	TransportCfg transport.Config
}

func (s AMFServer) Run(ctx context.Context) error {
	ln, err := backend.Listen(ctx, s.TransportCfg)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Printf("amf listening on %s transport=%s\n", ln.Addr(), s.TransportCfg.Type)

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		default:
		}

		sess, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleSession(ctx, sess)
		}()
	}
}

func handleSession(ctx context.Context, sess transport.Session) {
	defer sess.Close()
	ch, err := sess.AcceptChannel(ctx)
	if err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		raw, err := ch.Recv(ctx)
		if err != nil {
			return
		}
		frm, err := wire.Decode(raw)
		if err != nil {
			continue
		}
		switch frm.MsgType {
		case wire.MsgReq:
			resp := wire.Frame{MsgType: wire.MsgResp, SeqID: frm.SeqID, SendTSNS: frm.SendTSNS, ChannelID: frm.ChannelID, Payload: frm.Payload}
			buf, _ := resp.Encode()
			_ = ch.Send(ctx, buf)
		case wire.MsgFlood:
			// Flood mode is one-way by default.
		default:
		}
	}
}
