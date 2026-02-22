package mpegts

import (
	"testing"
)

func makePacket(pid uint16, cc uint8, pusi bool, payload []byte) []byte {
	buf := make([]byte, packetSize)
	buf[0] = syncByte
	buf[1] = byte(pid>>8) & 0x1F
	buf[2] = byte(pid)
	buf[3] = 0x10 | (cc & 0x0F) // payload only
	if pusi {
		buf[1] |= 0x40
	}
	copy(buf[4:], payload)
	return buf
}

func makePacketWithAF(pid uint16, cc uint8, afLen int, payload []byte) []byte {
	buf := make([]byte, packetSize)
	buf[0] = syncByte
	buf[1] = byte(pid>>8) & 0x1F
	buf[2] = byte(pid)
	if len(payload) > 0 {
		buf[3] = 0x30 | (cc & 0x0F) // adaptation + payload
	} else {
		buf[3] = 0x20 | (cc & 0x0F) // adaptation only
	}
	buf[4] = byte(afLen)
	// AF body is zeros (no flags set)
	offset := 5 + afLen
	if offset < packetSize {
		copy(buf[offset:], payload)
	}
	return buf
}

func TestParsePacket_Normal(t *testing.T) {
	t.Parallel()
	payload := []byte{0x01, 0x02, 0x03}
	buf := makePacket(0x100, 5, false, payload)

	p, err := parsePacket(buf)
	if err != nil {
		t.Fatal(err)
	}

	if p.Header.PID != 0x100 {
		t.Errorf("PID = %d, want %d", p.Header.PID, 0x100)
	}
	if p.Header.ContinuityCounter != 5 {
		t.Errorf("CC = %d, want 5", p.Header.ContinuityCounter)
	}
	if p.Header.PayloadUnitStartIndicator {
		t.Error("PUSI should be false")
	}
	if !p.Header.HasPayload {
		t.Error("HasPayload should be true")
	}
	if p.Header.HasAdaptationField {
		t.Error("HasAdaptationField should be false")
	}
	if len(p.Payload) != 184 {
		t.Errorf("payload length = %d, want 184", len(p.Payload))
	}
	if p.Payload[0] != 0x01 || p.Payload[1] != 0x02 || p.Payload[2] != 0x03 {
		t.Error("payload content mismatch")
	}
}

func TestParsePacket_PUSI(t *testing.T) {
	t.Parallel()
	buf := makePacket(0x1E1, 0, true, nil)
	p, err := parsePacket(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Header.PayloadUnitStartIndicator {
		t.Error("PUSI should be true")
	}
	if p.Header.PID != 0x1E1 {
		t.Errorf("PID = 0x%X, want 0x1E1", p.Header.PID)
	}
}

func TestParsePacket_TEI(t *testing.T) {
	t.Parallel()
	buf := makePacket(0x100, 0, false, nil)
	buf[1] |= 0x80 // set TEI
	p, err := parsePacket(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Header.TransportErrorIndicator {
		t.Error("TEI should be true")
	}
}

func TestParsePacket_AdaptationField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		afLen       int
		payloadData []byte
		wantPayLen  int
	}{
		{"af_1_byte", 1, []byte{0xAA}, 188 - 6},
		{"af_10_bytes", 10, []byte{0xBB}, 188 - 15},
		{"af_183_bytes_no_payload", 183, nil, 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := makePacketWithAF(0x100, 0, tc.afLen, tc.payloadData)
			p, err := parsePacket(buf)
			if err != nil {
				t.Fatal(err)
			}
			if !p.Header.HasAdaptationField {
				t.Error("HasAdaptationField should be true")
			}
			if tc.payloadData != nil {
				if !p.Header.HasPayload {
					t.Error("HasPayload should be true")
				}
				if len(p.Payload) != tc.wantPayLen {
					t.Errorf("payload length = %d, want %d", len(p.Payload), tc.wantPayLen)
				}
			} else {
				if p.Header.HasPayload {
					// adaptation-only
				}
			}
		})
	}
}

func TestParsePacket_BadSyncByte(t *testing.T) {
	t.Parallel()
	buf := make([]byte, packetSize)
	buf[0] = 0x00
	_, err := parsePacket(buf)
	if err == nil {
		t.Error("expected error for bad sync byte")
	}
}

func TestParsePacket_WrongSize(t *testing.T) {
	t.Parallel()
	_, err := parsePacket([]byte{0x47, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for wrong packet size")
	}
}

func TestParsePacket_MaxPID(t *testing.T) {
	t.Parallel()
	buf := makePacket(0x1FFF, 0, false, nil)
	p, err := parsePacket(buf)
	if err != nil {
		t.Fatal(err)
	}
	if p.Header.PID != 0x1FFF {
		t.Errorf("PID = 0x%X, want 0x1FFF", p.Header.PID)
	}
}
