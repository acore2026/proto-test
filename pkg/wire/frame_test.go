package wire

import "testing"

func TestEncodeDecode(t *testing.T) {
	in := Frame{MsgType: MsgReq, SeqID: 42, SendTSNS: 123456, ChannelID: 3, Payload: []byte{0x01, 0x02}}
	buf, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.MsgType != in.MsgType || out.SeqID != in.SeqID || out.SendTSNS != in.SendTSNS || out.ChannelID != in.ChannelID {
		t.Fatalf("header mismatch: %#v vs %#v", out, in)
	}
	if len(out.Payload) != len(in.Payload) || out.Payload[0] != in.Payload[0] || out.Payload[1] != in.Payload[1] {
		t.Fatalf("payload mismatch: %v vs %v", out.Payload, in.Payload)
	}
}
