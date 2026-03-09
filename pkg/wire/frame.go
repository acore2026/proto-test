package wire

import (
	"bytes"
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
	buf := bytes.NewBuffer(make([]byte, 0, 4+1+1+8+8+2+2+len(f.Payload)))
	_ = binary.Write(buf, binary.BigEndian, Magic)
	_ = binary.Write(buf, binary.BigEndian, Version)
	_ = binary.Write(buf, binary.BigEndian, uint8(f.MsgType))
	_ = binary.Write(buf, binary.BigEndian, f.SeqID)
	_ = binary.Write(buf, binary.BigEndian, f.SendTSNS)
	_ = binary.Write(buf, binary.BigEndian, f.ChannelID)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(f.Payload)))
	_, _ = buf.Write(f.Payload)
	return buf.Bytes(), nil
}

func Decode(input []byte) (Frame, error) {
	if len(input) < 26 {
		return Frame{}, fmt.Errorf("frame too short: %d", len(input))
	}
	var f Frame
	r := bytes.NewReader(input)
	var magic uint32
	var version uint8
	var typ uint8
	var plen uint16
	_ = binary.Read(r, binary.BigEndian, &magic)
	_ = binary.Read(r, binary.BigEndian, &version)
	_ = binary.Read(r, binary.BigEndian, &typ)
	_ = binary.Read(r, binary.BigEndian, &f.SeqID)
	_ = binary.Read(r, binary.BigEndian, &f.SendTSNS)
	_ = binary.Read(r, binary.BigEndian, &f.ChannelID)
	_ = binary.Read(r, binary.BigEndian, &plen)

	if magic != Magic {
		return Frame{}, fmt.Errorf("bad magic: %x", magic)
	}
	if version != Version {
		return Frame{}, fmt.Errorf("bad version: %d", version)
	}
	if len(input) < 26+int(plen) {
		return Frame{}, fmt.Errorf("truncated payload")
	}
	f.MsgType = MsgType(typ)
	f.Payload = make([]byte, int(plen))
	copy(f.Payload, input[26:26+int(plen)])
	return f, nil
}
