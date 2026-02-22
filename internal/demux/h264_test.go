package demux

import (
	"testing"
)

func TestParseAnnexB(t *testing.T) {
	t.Parallel()
	// Build an Annex B byte stream with SPS, PPS, and IDR
	data := []byte{
		// 4-byte start code + SPS (NAL type 7)
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xE0, 0x1E,
		// 4-byte start code + PPS (NAL type 8)
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80,
		// 4-byte start code + IDR (NAL type 5)
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, 0xFF, 0xFE,
	}

	nalus := ParseAnnexB(data)

	if len(nalus) != 3 {
		t.Fatalf("expected 3 NAL units, got %d", len(nalus))
	}

	// SPS
	if nalus[0].Type != NALTypeSPS {
		t.Errorf("expected SPS (7), got %d", nalus[0].Type)
	}
	if !IsSPS(nalus[0].Type) {
		t.Error("IsSPS returned false for SPS")
	}

	// PPS
	if nalus[1].Type != NALTypePPS {
		t.Errorf("expected PPS (8), got %d", nalus[1].Type)
	}
	if !IsPPS(nalus[1].Type) {
		t.Error("IsPPS returned false for PPS")
	}

	// IDR
	if nalus[2].Type != NALTypeIDR {
		t.Errorf("expected IDR (5), got %d", nalus[2].Type)
	}
	if !IsKeyframe(nalus[2].Type) {
		t.Error("IsKeyframe returned false for IDR")
	}
}

func TestParseAnnexB3ByteStartCode(t *testing.T) {
	t.Parallel()
	// 3-byte start code
	data := []byte{
		0x00, 0x00, 0x01, 0x67, 0x42, 0xE0,
		0x00, 0x00, 0x01, 0x65, 0x88, 0x84,
	}

	nalus := ParseAnnexB(data)

	if len(nalus) != 2 {
		t.Fatalf("expected 2 NAL units, got %d", len(nalus))
	}

	if nalus[0].Type != NALTypeSPS {
		t.Errorf("expected SPS, got %d", nalus[0].Type)
	}

	if nalus[1].Type != NALTypeIDR {
		t.Errorf("expected IDR, got %d", nalus[1].Type)
	}
}

func TestParseAnnexBEmpty(t *testing.T) {
	t.Parallel()
	nalus := ParseAnnexB(nil)
	if nalus != nil {
		t.Errorf("expected nil for empty input, got %d units", len(nalus))
	}

	nalus = ParseAnnexB([]byte{0x00, 0x01})
	if nalus != nil {
		t.Errorf("expected nil for too-short input, got %d units", len(nalus))
	}
}

func TestParseAnnexBTrailingZeroAbsorbedByStartCode(t *testing.T) {
	t.Parallel()
	// In H.264 Annex B, zeros preceding a start code are part of the
	// start code prefix, not NALU data. So [... 06 AA BB] [00 00 00 01 41 ...]
	// means the SEI has 3 bytes (06 AA BB) and the start code is 4-byte.
	data := []byte{
		// 4-byte start code + SEI (NAL type 6)
		0x00, 0x00, 0x00, 0x01, 0x06, 0xAA, 0xBB, 0x00,
		// This 0x00 + the next 00 00 01 forms a 4-byte start code
		0x00, 0x00, 0x01, 0x41, 0x9A,
	}

	nalus := ParseAnnexB(data)
	if len(nalus) != 2 {
		t.Fatalf("expected 2 NAL units, got %d", len(nalus))
	}

	// SEI: data is [0x06, 0xAA, 0xBB] — trailing 0x00 absorbed by start code
	if nalus[0].Type != NALTypeSEI {
		t.Errorf("expected SEI (6), got %d", nalus[0].Type)
	}
	if len(nalus[0].Data) != 3 {
		t.Errorf("SEI data length: got %d, want 3", len(nalus[0].Data))
	}

	// Slice
	if nalus[1].Type != NALTypeSlice {
		t.Errorf("expected Slice (1), got %d", nalus[1].Type)
	}
}

func TestParseAnnexBMixed3And4ByteStartCodes(t *testing.T) {
	t.Parallel()
	// Mix of 4-byte and 3-byte start codes
	data := []byte{
		// 4-byte start code + SPS
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42,
		// 3-byte start code + PPS
		0x00, 0x00, 0x01, 0x68, 0xCE,
		// 4-byte start code + SEI
		0x00, 0x00, 0x00, 0x01, 0x06, 0xFF, 0xFE,
		// 3-byte start code + IDR
		0x00, 0x00, 0x01, 0x65, 0x88,
	}

	nalus := ParseAnnexB(data)
	if len(nalus) != 4 {
		t.Fatalf("expected 4 NAL units, got %d", len(nalus))
	}

	wantTypes := []byte{NALTypeSPS, NALTypePPS, NALTypeSEI, NALTypeIDR}
	for i, want := range wantTypes {
		if nalus[i].Type != want {
			t.Errorf("NALU[%d]: got type %d, want %d", i, nalus[i].Type, want)
		}
	}

	// SEI should have 3 bytes: [0x06, 0xFF, 0xFE]
	if len(nalus[2].Data) != 3 {
		t.Errorf("SEI data length: got %d, want 3", len(nalus[2].Data))
	}
}

func TestParseAnnexBSlice(t *testing.T) {
	t.Parallel()
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x41, 0x9A, 0x00, 0x01, 0x02,
	}

	nalus := ParseAnnexB(data)
	if len(nalus) != 1 {
		t.Fatalf("expected 1 NAL unit, got %d", len(nalus))
	}

	if nalus[0].Type != NALTypeSlice {
		t.Errorf("expected Slice (1), got %d", nalus[0].Type)
	}

	if IsKeyframe(nalus[0].Type) {
		t.Error("non-IDR slice should not be keyframe")
	}
}

func TestParseSPS720p(t *testing.T) {
	t.Parallel()
	sps := []byte{
		0x67, 0x64, 0x00, 0x1f, 0xac, 0xd9, 0x40, 0x50,
		0x05, 0xbb, 0xff, 0x00, 0x03, 0x00, 0x04, 0x6a,
		0x02, 0x02, 0x02, 0x80, 0x00, 0x01, 0xf4, 0x80,
		0x00, 0x5d, 0xc0, 0x07, 0x8c, 0x18, 0xcb,
	}

	info, err := ParseSPS(sps)
	if err != nil {
		t.Fatalf("ParseSPS error: %v", err)
	}
	if info.Width != 1280 {
		t.Errorf("width: got %d, want 1280", info.Width)
	}
	if info.Height != 720 {
		t.Errorf("height: got %d, want 720", info.Height)
	}
}

func TestParseSPS256x192(t *testing.T) {
	t.Parallel()
	sps := []byte{
		0x67, 0x4d, 0x40, 0x1f, 0xb9, 0x08, 0x08, 0x0c,
		0xd8, 0x0b, 0x50, 0x10, 0x10, 0x14, 0x00, 0x00,
		0x0f, 0xa4, 0x00, 0x02, 0xee, 0x03, 0x81, 0x80,
		0x04, 0x93, 0xc0, 0x02, 0x49, 0xe8, 0xa0, 0xc0,
		0x3a, 0x8e, 0x18, 0xc9,
	}

	info, err := ParseSPS(sps)
	if err != nil {
		t.Fatalf("ParseSPS error: %v", err)
	}
	if info.Width != 256 {
		t.Errorf("width: got %d, want 256", info.Width)
	}
	if info.Height != 192 {
		t.Errorf("height: got %d, want 192", info.Height)
	}
}

func TestParseSPSTooShort(t *testing.T) {
	t.Parallel()
	_, err := ParseSPS([]byte{0x67, 0x64, 0x00})
	if err == nil {
		t.Error("expected error for too-short SPS")
	}
}

func TestParseSPSEmptyInput(t *testing.T) {
	t.Parallel()
	_, err := ParseSPS(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
	_, err = ParseSPS([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseSPSVUITimingParams(t *testing.T) {
	t.Parallel()
	sps := []byte{
		0x67, 0x64, 0x00, 0x1f, 0xac, 0xd9, 0x40, 0x50,
		0x05, 0xbb, 0x01, 0x6a, 0x04, 0x04, 0x0a, 0x80,
		0x00, 0x00, 0x03, 0x00, 0x80, 0x00, 0x00, 0x1e,
		0x30, 0x20, 0x00, 0x16, 0xe3, 0x60, 0x00, 0x2d,
		0xc6, 0xd2, 0x49, 0x80, 0x7c, 0x60, 0xc6, 0x58,
	}

	info, err := ParseSPS(sps)
	if err != nil {
		t.Fatalf("ParseSPS error: %v", err)
	}
	if info.Width != 1280 {
		t.Errorf("width: got %d, want 1280", info.Width)
	}
	if info.Height != 720 {
		t.Errorf("height: got %d, want 720", info.Height)
	}
	if !info.PicStructPresent {
		t.Error("expected PicStructPresent=true")
	}
	if !info.HRDPresent {
		t.Error("expected HRDPresent=true")
	}
	if info.CpbRemovalDelayLen != 10 {
		t.Errorf("CpbRemovalDelayLen: got %d, want 10", info.CpbRemovalDelayLen)
	}
	if info.DpbOutputDelayLen != 7 {
		t.Errorf("DpbOutputDelayLen: got %d, want 7", info.DpbOutputDelayLen)
	}
	if info.TimeOffsetLen != 0 {
		t.Errorf("TimeOffsetLen: got %d, want 0", info.TimeOffsetLen)
	}
}

func TestParsePicTimingSEI(t *testing.T) {
	t.Parallel()
	sps := SPSInfo{
		PicStructPresent:   true,
		HRDPresent:         true,
		CpbRemovalDelayLen: 10,
		DpbOutputDelayLen:  7,
		TimeOffsetLen:      0,
	}

	tests := []struct {
		name string
		nal  []byte
		want Timecode
		ok   bool
	}{
		{
			name: "TC 01:00:00:00 with emulation prevention",
			nal:  []byte{0x06, 0x01, 0x08, 0x00, 0x02, 0x04, 0x12, 0x00, 0x00, 0x03, 0x00, 0x40, 0x80},
			want: Timecode{Hours: 1, Minutes: 0, Seconds: 0, Frames: 0},
			ok:   true,
		},
		{
			name: "TC 01:00:00:01",
			nal:  []byte{0x06, 0x01, 0x08, 0x00, 0x85, 0x04, 0x12, 0x00, 0x80, 0x00, 0x40, 0x80},
			want: Timecode{Hours: 1, Minutes: 0, Seconds: 0, Frames: 1},
			ok:   true,
		},
		{
			name: "TC 01:00:00:02",
			nal:  []byte{0x06, 0x01, 0x08, 0x01, 0x02, 0x04, 0x12, 0x01, 0x00, 0x00, 0x40, 0x80},
			want: Timecode{Hours: 1, Minutes: 0, Seconds: 0, Frames: 2},
			ok:   true,
		},
		{
			name: "no clock_timestamp",
			nal:  []byte{0x06, 0x01, 0x03, 0x00, 0x02, 0x02, 0x80},
			want: Timecode{},
			ok:   false,
		},
		{
			name: "too short",
			nal:  []byte{0x06},
			want: Timecode{},
			ok:   false,
		},
		{
			name: "no HRD in SPS",
			nal:  []byte{0x06, 0x01, 0x08, 0x00, 0x02, 0x04, 0x12, 0x00, 0x00, 0x03, 0x00, 0x40, 0x80},
			want: Timecode{},
			ok:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := sps
			if tt.name == "no HRD in SPS" {
				s = SPSInfo{PicStructPresent: true, HRDPresent: false}
			}
			got, ok := ParsePicTimingSEI(tt.nal, s)
			if ok != tt.ok {
				t.Fatalf("ok: got %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("timecode: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimecodeString(t *testing.T) {
	t.Parallel()
	tc := Timecode{Hours: 1, Minutes: 2, Seconds: 3, Frames: 4}
	want := "01:02:03:04"
	if tc.String() != want {
		t.Errorf("String(): got %q, want %q", tc.String(), want)
	}
}
