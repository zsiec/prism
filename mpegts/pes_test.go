package mpegts

import "testing"

// encodePTS encodes a 33-bit PTS/DTS value into 5 bytes with marker bits.
func encodePTS(marker byte, value int64) []byte {
	bs := make([]byte, 5)
	bs[0] = marker<<4 | byte((value>>29)&0x0E) | 0x01
	bs[1] = byte(value >> 22)
	bs[2] = byte((value>>14)&0xFE) | 0x01
	bs[3] = byte(value >> 7)
	bs[4] = byte((value<<1)&0xFE) | 0x01
	return bs
}

func buildPESPacket(streamID byte, pts, dts int64, hasPTS, hasDTS bool, data []byte) []byte {
	var optHeader []byte
	ptsDTSIndicator := byte(0)
	if hasPTS && hasDTS {
		ptsDTSIndicator = 3
		optHeader = append(optHeader, encodePTS(0x03, pts)...)
		optHeader = append(optHeader, encodePTS(0x01, dts)...)
	} else if hasPTS {
		ptsDTSIndicator = 2
		optHeader = append(optHeader, encodePTS(0x02, pts)...)
	}

	headerDataLen := len(optHeader)
	// PES header: start_code(3) + stream_id(1) + packet_length(2) + flags(2) + header_data_length(1) + optional + data
	totalLen := 3 + headerDataLen + len(data)
	packetLength := totalLen
	if streamID == 0xE0 {
		packetLength = 0 // video: unbounded
	}

	buf := make([]byte, 0, 6+3+headerDataLen+len(data))
	buf = append(buf, 0x00, 0x00, 0x01) // start code
	buf = append(buf, streamID)
	buf = append(buf, byte(packetLength>>8), byte(packetLength))
	buf = append(buf, 0x80)                // marker bits
	buf = append(buf, ptsDTSIndicator<<6)  // PTS_DTS_indicator
	buf = append(buf, byte(headerDataLen)) // PES_header_data_length
	buf = append(buf, optHeader...)
	buf = append(buf, data...)
	return buf
}

func TestParsePES_PTSOnly(t *testing.T) {
	t.Parallel()
	data := []byte{0xAA, 0xBB, 0xCC}
	buf := buildPESPacket(0xC0, 90000, 0, true, false, data) // audio stream, PTS=1s

	pes, err := parsePES(buf)
	if err != nil {
		t.Fatal(err)
	}
	if pes.Header.StreamID != 0xC0 {
		t.Errorf("stream ID = 0x%02X, want 0xC0", pes.Header.StreamID)
	}
	if pes.Header.OptionalHeader == nil {
		t.Fatal("expected optional header")
	}
	if pes.Header.OptionalHeader.PTS == nil {
		t.Fatal("expected PTS")
	}
	if pes.Header.OptionalHeader.PTS.Base != 90000 {
		t.Errorf("PTS = %d, want 90000", pes.Header.OptionalHeader.PTS.Base)
	}
	if pes.Header.OptionalHeader.DTS != nil {
		t.Error("DTS should be nil")
	}
	if len(pes.Data) != 3 {
		t.Errorf("data length = %d, want 3", len(pes.Data))
	}
}

func TestParsePES_PTSAndDTS(t *testing.T) {
	t.Parallel()
	data := []byte{0x01, 0x02}
	buf := buildPESPacket(0xE0, 2790000, 2782492, true, true, data) // video

	pes, err := parsePES(buf)
	if err != nil {
		t.Fatal(err)
	}
	if pes.Header.OptionalHeader.PTS == nil {
		t.Fatal("expected PTS")
	}
	if pes.Header.OptionalHeader.PTS.Base != 2790000 {
		t.Errorf("PTS = %d, want 2790000", pes.Header.OptionalHeader.PTS.Base)
	}
	if pes.Header.OptionalHeader.DTS == nil {
		t.Fatal("expected DTS")
	}
	if pes.Header.OptionalHeader.DTS.Base != 2782492 {
		t.Errorf("DTS = %d, want 2782492", pes.Header.OptionalHeader.DTS.Base)
	}
}

func TestParsePES_NoTimestamps(t *testing.T) {
	t.Parallel()
	data := []byte{0x01}
	buf := buildPESPacket(0xC0, 0, 0, false, false, data)

	pes, err := parsePES(buf)
	if err != nil {
		t.Fatal(err)
	}
	if pes.Header.OptionalHeader == nil {
		t.Fatal("expected optional header")
	}
	if pes.Header.OptionalHeader.PTS != nil {
		t.Error("PTS should be nil")
	}
}

func TestParsePES_VideoUnboundedLength(t *testing.T) {
	t.Parallel()
	data := make([]byte, 500)
	for i := range data {
		data[i] = byte(i)
	}
	buf := buildPESPacket(0xE0, 90000, 0, true, false, data)

	pes, err := parsePES(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(pes.Data) != 500 {
		t.Errorf("data length = %d, want 500", len(pes.Data))
	}
}

func TestParsePES_PaddingStream(t *testing.T) {
	t.Parallel()
	buf := []byte{0x00, 0x00, 0x01, 0xBE, 0x00, 0x04, 0xFF, 0xFF, 0xFF, 0xFF}
	pes, err := parsePES(buf)
	if err != nil {
		t.Fatal(err)
	}
	if pes.Header.StreamID != 0xBE {
		t.Errorf("stream ID = 0x%02X, want 0xBE", pes.Header.StreamID)
	}
	if pes.Header.OptionalHeader != nil {
		t.Error("padding stream should not have optional header")
	}
	if len(pes.Data) != 4 {
		t.Errorf("data length = %d, want 4", len(pes.Data))
	}
}

func TestParsePES_KnownPTSValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pts  int64
	}{
		{"zero", 0},
		{"one_second", 90000},
		{"one_minute", 5400000},
		{"golden_first_video", 2790000},
		{"large", 8589934591}, // max 33-bit value
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := buildPESPacket(0xC0, tc.pts, 0, true, false, []byte{0x00})
			pes, err := parsePES(buf)
			if err != nil {
				t.Fatal(err)
			}
			if pes.Header.OptionalHeader.PTS.Base != tc.pts {
				t.Errorf("PTS = %d, want %d", pes.Header.OptionalHeader.PTS.Base, tc.pts)
			}
		})
	}
}

func TestIsPESPayload(t *testing.T) {
	t.Parallel()
	if !isPESPayload([]byte{0x00, 0x00, 0x01, 0xE0}) {
		t.Error("should detect PES start code")
	}
	if isPESPayload([]byte{0x00, 0x00, 0x00}) {
		t.Error("should not detect non-PES data")
	}
	if isPESPayload([]byte{0x00, 0x00}) {
		t.Error("should not detect short data")
	}
}

func TestParsePTSOrDTS_Roundtrip(t *testing.T) {
	t.Parallel()
	values := []int64{0, 1, 90000, 2790000, 8589934591}
	for _, v := range values {
		encoded := encodePTS(0x02, v)
		cr := parsePTSOrDTS(encoded)
		if cr == nil {
			t.Fatalf("parsePTSOrDTS returned nil for %d", v)
		}
		if cr.Base != v {
			t.Errorf("round-trip: got %d, want %d", cr.Base, v)
		}
	}
}

func TestParsePES_InvalidStartCode(t *testing.T) {
	t.Parallel()
	buf := []byte{0x00, 0x00, 0x00, 0xE0, 0x00, 0x00}
	_, err := parsePES(buf)
	if err == nil {
		t.Error("expected error for invalid start code")
	}
}

func TestParsePES_TooShort(t *testing.T) {
	t.Parallel()
	_, err := parsePES([]byte{0x00, 0x00, 0x01})
	if err == nil {
		t.Error("expected error for short packet")
	}
}
