package wire

import (
	"encoding/binary"
	"fmt"
)

const (
	Magic   uint32 = 0x4D354747 // M5GG
	Version uint8  = 1
)

type MsgType uint8

const (
	MsgReq MsgType = iota + 1
	MsgResp
	MsgFlood
)

type Frame struct {
	MsgType   MsgType
	SeqID     uint64
	SendTSNS  int64
	ChannelID uint16
	Payload   []byte
}

func (f Frame) Encode() ([]byte, error) {
	if len(f.Payload) > 0xFFFF {
		return nil, fmt.Errorf("payload too large: %d", len(f.Payload))
	}
	out := make([]byte, 26+len(f.Payload))
	binary.BigEndian.PutUint32(out[0:4], Magic)
	out[4] = Version
	out[5] = byte(f.MsgType)
	binary.BigEndian.PutUint64(out[6:14], f.SeqID)
	binary.BigEndian.PutUint64(out[14:22], uint64(f.SendTSNS))
	binary.BigEndian.PutUint16(out[22:24], f.ChannelID)
	binary.BigEndian.PutUint16(out[24:26], uint16(len(f.Payload)))
	copy(out[26:], f.Payload)
	return out, nil
}

func Decode(input []byte) (Frame, error) {
	if len(input) < 26 {
		return Frame{}, fmt.Errorf("frame too short: %d", len(input))
	}
	magic := binary.BigEndian.Uint32(input[0:4])
	if magic != Magic {
		return Frame{}, fmt.Errorf("bad magic: %x", magic)
	}
	version := input[4]
	if version != Version {
		return Frame{}, fmt.Errorf("bad version: %d", version)
	}
	typ := input[5]
	seqID := binary.BigEndian.Uint64(input[6:14])
	sendTS := int64(binary.BigEndian.Uint64(input[14:22]))
	chID := binary.BigEndian.Uint16(input[22:24])
	plen := binary.BigEndian.Uint16(input[24:26])
	if len(input) < 26+int(plen) {
		return Frame{}, fmt.Errorf("truncated payload")
	}
	return Frame{
		MsgType:   MsgType(typ),
		SeqID:     seqID,
		SendTSNS:  sendTS,
		ChannelID: chID,
		// Zero-copy payload view over input to avoid extra allocation.
		Payload: input[26 : 26+int(plen)],
	}, nil
}
