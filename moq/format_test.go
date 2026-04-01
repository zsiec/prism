package moq

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAnnexBToAVC1Single(t *testing.T) {
	t.Parallel()
	// Single NALU with 4-byte start code
	nalu := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xAA, 0xBB}
	result := AnnexBToAVC1([][]byte{nalu})

	// Should be: 4-byte length (3) + raw NAL data
	if len(result) != 7 {
		t.Fatalf("expected 7 bytes, got %d", len(result))
	}

	length := binary.BigEndian.Uint32(result[0:4])
	if length != 3 {
		t.Errorf("NALU length: got %d, want 3", length)
	}
	if !bytes.Equal(result[4:], []byte{0x65, 0xAA, 0xBB}) {
		t.Errorf("NALU data mismatch: %x", result[4:])
	}
}

func TestAnnexBToAVC1Multiple(t *testing.T) {
	t.Parallel()
	// SPS + PPS + IDR
	sps := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xE0}
	pps := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xCE}
	idr := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x80, 0x40}

	result := AnnexBToAVC1([][]byte{sps, pps, idr})

	// SPS: 4 + 3 = 7, PPS: 4 + 2 = 6, IDR: 4 + 4 = 8 → total 21
	if len(result) != 21 {
		t.Fatalf("expected 21 bytes, got %d", len(result))
	}

	// Check SPS length
	if binary.BigEndian.Uint32(result[0:4]) != 3 {
		t.Errorf("SPS length mismatch")
	}
	// Check PPS length
	if binary.BigEndian.Uint32(result[7:11]) != 2 {
		t.Errorf("PPS length mismatch")
	}
	// Check IDR length
	if binary.BigEndian.Uint32(result[13:17]) != 4 {
		t.Errorf("IDR length mismatch")
	}
}

func TestAnnexBToAVC1Empty(t *testing.T) {
	t.Parallel()
	result := AnnexBToAVC1(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(result))
	}
}

func TestAnnexBToAVC1ThreeByteStartCode(t *testing.T) {
	t.Parallel()
	// 3-byte start code
	nalu := []byte{0x00, 0x00, 0x01, 0x65, 0xAA}
	result := AnnexBToAVC1([][]byte{nalu})

	if len(result) != 6 {
		t.Fatalf("expected 6 bytes, got %d", len(result))
	}

	length := binary.BigEndian.Uint32(result[0:4])
	if length != 2 {
		t.Errorf("NALU length: got %d, want 2", length)
	}
}

func TestAnnexBToAVC1NoStartCode(t *testing.T) {
	t.Parallel()
	// Raw NALU without start code (defensive)
	nalu := []byte{0x65, 0xAA, 0xBB}
	result := AnnexBToAVC1([][]byte{nalu})

	if len(result) != 7 {
		t.Fatalf("expected 7 bytes, got %d", len(result))
	}

	length := binary.BigEndian.Uint32(result[0:4])
	if length != 3 {
		t.Errorf("NALU length: got %d, want 3", length)
	}
}

func TestStripADTS7Byte(t *testing.T) {
	t.Parallel()
	// 7-byte ADTS header (protection absent = 1, no CRC)
	header := []byte{0xFF, 0xF1, 0x50, 0x80, 0x02, 0x00, 0xFC}
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	adts := append(header, payload...)

	result := StripADTS(adts)
	if !bytes.Equal(result, payload) {
		t.Errorf("expected payload only, got %x", result)
	}
}

func TestStripADTS9Byte(t *testing.T) {
	t.Parallel()
	// 9-byte ADTS header (protection absent = 0, CRC present)
	header := []byte{0xFF, 0xF0, 0x50, 0x80, 0x02, 0x00, 0xFC, 0xAA, 0xBB}
	payload := []byte{0xDE, 0xAD}
	adts := append(header, payload...)

	result := StripADTS(adts)
	if !bytes.Equal(result, payload) {
		t.Errorf("expected payload only, got %x", result)
	}
}

func TestStripADTSNotADTS(t *testing.T) {
	t.Parallel()
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	result := StripADTS(data)
	if !bytes.Equal(result, data) {
		t.Error("non-ADTS data should be returned unchanged")
	}
}

func TestStripADTSTooShort(t *testing.T) {
	t.Parallel()
	data := []byte{0xFF, 0xF1}
	result := StripADTS(data)
	if !bytes.Equal(result, data) {
		t.Error("too-short data should be returned unchanged")
	}
}

func TestBuildAVCDecoderConfig(t *testing.T) {
	t.Parallel()
	// SPS with NAL header byte 0x67 (type 7)
	sps := []byte{0x67, 0x42, 0xE0, 0x1E, 0xAB, 0xCD}
	pps := []byte{0x68, 0xCE, 0x38, 0x80}

	config := BuildAVCDecoderConfig(sps, pps)
	if config == nil {
		t.Fatal("expected non-nil config")
	}

	// Verify structure
	if config[0] != 1 {
		t.Errorf("configurationVersion: got %d, want 1", config[0])
	}
	if config[1] != 0x42 {
		t.Errorf("AVCProfileIndication: got 0x%02x, want 0x42", config[1])
	}
	if config[2] != 0xE0 {
		t.Errorf("profile_compatibility: got 0x%02x, want 0xE0", config[2])
	}
	if config[3] != 0x1E {
		t.Errorf("AVCLevelIndication: got 0x%02x, want 0x1E", config[3])
	}
	if config[4] != 0xFF {
		t.Errorf("lengthSizeMinusOne: got 0x%02x, want 0xFF", config[4])
	}
	if config[5] != 0xE1 {
		t.Errorf("numSPS: got 0x%02x, want 0xE1", config[5])
	}

	// SPS length
	spsLen := binary.BigEndian.Uint16(config[6:8])
	if spsLen != uint16(len(sps)) {
		t.Errorf("SPS length: got %d, want %d", spsLen, len(sps))
	}

	// SPS data
	if !bytes.Equal(config[8:8+len(sps)], sps) {
		t.Error("SPS data mismatch")
	}

	// PPS count
	ppsOffset := 8 + len(sps)
	if config[ppsOffset] != 1 {
		t.Errorf("numPPS: got %d, want 1", config[ppsOffset])
	}

	// PPS length
	ppsLen := binary.BigEndian.Uint16(config[ppsOffset+1 : ppsOffset+3])
	if ppsLen != uint16(len(pps)) {
		t.Errorf("PPS length: got %d, want %d", ppsLen, len(pps))
	}

	// PPS data
	if !bytes.Equal(config[ppsOffset+3:ppsOffset+3+len(pps)], pps) {
		t.Error("PPS data mismatch")
	}

	// Total size
	expectedLen := 6 + 2 + len(sps) + 1 + 2 + len(pps)
	if len(config) != expectedLen {
		t.Errorf("total length: got %d, want %d", len(config), expectedLen)
	}
}

func TestBuildAVCDecoderConfigTooShort(t *testing.T) {
	t.Parallel()
	config := BuildAVCDecoderConfig([]byte{0x67, 0x42}, []byte{0x68})
	if config != nil {
		t.Error("expected nil for SPS too short")
	}
}

func TestBuildAVCDecoderConfigNoPPS(t *testing.T) {
	t.Parallel()
	config := BuildAVCDecoderConfig([]byte{0x67, 0x42, 0xE0, 0x1E}, nil)
	if config != nil {
		t.Error("expected nil for empty PPS")
	}
}

func TestBuildHEVCDecoderConfig(t *testing.T) {
	t.Parallel()
	// Known-good HEVC NALUs from h265_test.go
	vps := []byte{0x40, 0x01, 0x0C, 0x01, 0xFF, 0xFF}
	sps := []byte{
		0x42, 0x01, // NAL header (type=33, layer=0, tid=1)
		0x01,                   // vps_id=0(4b), max_sub_layers_minus1=0(3b), temporal_nesting=1(1b)
		0x01,                   // profile_space=0(2b), tier=0(1b), profile_idc=1(5b)
		0x40, 0x00, 0x00, 0x00, // profile_compatibility_flags (bit 1 set)
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x00, // constraint_indicator_flags
		0x5D,                         // level_idc = 93 (Level 3.1)
		0xA0, 0x0A, 0x08, 0x0F, 0x16, // sps_id=0, chroma=1, width=320, height=240, bdl=0, bdc=0
	}
	pps := []byte{0x44, 0x01, 0xC0, 0xF7}

	config := BuildHEVCDecoderConfig(vps, sps, pps)
	if config == nil {
		t.Fatal("expected non-nil config")
	}

	// Verify header
	if config[0] != 1 {
		t.Errorf("configurationVersion: got %d, want 1", config[0])
	}

	// Verify level_idc at byte 12
	if config[12] != 93 {
		t.Errorf("general_level_idc: got %d, want 93", config[12])
	}

	// Verify chromaFormat at byte 16 (6 reserved bits | 2 chroma bits)
	// chromaFormatIdc=1 (4:2:0): 0xFC | 0x01 = 0xFD
	if config[16] != 0xFD {
		t.Errorf("chromaFormat: got 0x%02x, want 0xFD", config[16])
	}

	// Verify numOfArrays at byte 22
	if config[22] != 3 {
		t.Errorf("numOfArrays: got %d, want 3", config[22])
	}

	// Verify VPS array type
	if config[23] != 0x20 {
		t.Errorf("VPS array type: got 0x%02x, want 0x20", config[23])
	}

	// Verify SPS array follows VPS
	vpsArrayEnd := 23 + 1 + 2 + 2 + len(vps)
	if config[vpsArrayEnd] != 0x21 {
		t.Errorf("SPS array type: got 0x%02x, want 0x21", config[vpsArrayEnd])
	}

	// Verify PPS array follows SPS
	spsArrayEnd := vpsArrayEnd + 1 + 2 + 2 + len(sps)
	if config[spsArrayEnd] != 0x22 {
		t.Errorf("PPS array type: got 0x%02x, want 0x22", config[spsArrayEnd])
	}
}

func TestBuildAV1DecoderConfig(t *testing.T) {
	t.Parallel()

	// 1920x1080, profile 0, level 8, 8-bit, 4:2:0 (from demux/av1_test.go)
	seqHeaderOBU := []byte{
		0x0a, 0x0b, // OBU header (type=1, has_size=1) + LEB128 size=11
		0x00, 0x00, 0x00, 0x42, 0xab, 0xbf, 0xc3, 0x71,
		0x2b, 0xe4, 0x01,
	}

	config := BuildAV1DecoderConfig(seqHeaderOBU)
	if config == nil {
		t.Fatal("expected non-nil config")
	}

	// Total length = 4-byte header + len(seqHeaderOBU)
	expectedLen := 4 + len(seqHeaderOBU)
	if len(config) != expectedLen {
		t.Fatalf("total length: got %d, want %d", len(config), expectedLen)
	}

	// Byte 0: marker(1)=1 | version(7)=1 → 0x81
	if config[0] != 0x81 {
		t.Errorf("byte 0 (marker+version): got 0x%02x, want 0x81", config[0])
	}

	// Byte 1: seq_profile(3)=0 | seq_level_idx_0(5)=8 → 0x08
	if config[1] != 0x08 {
		t.Errorf("byte 1 (profile|level): got 0x%02x, want 0x08", config[1])
	}

	// Verify profile extracted correctly (top 3 bits)
	profile := config[1] >> 5
	if profile != 0 {
		t.Errorf("profile: got %d, want 0", profile)
	}

	// Verify level extracted correctly (bottom 5 bits)
	level := config[1] & 0x1F
	if level != 8 {
		t.Errorf("level: got %d, want 8", level)
	}

	// Byte 2: tier=0, high_bitdepth=0, twelve_bit=0, mono=0, csx=1, csy=1, csp=0 → 0x0C
	if config[2] != 0x0C {
		t.Errorf("byte 2 (tier/bd/chroma): got 0x%02x, want 0x0C", config[2])
	}

	// Byte 3: reserved = 0x00
	if config[3] != 0x00 {
		t.Errorf("byte 3 (reserved): got 0x%02x, want 0x00", config[3])
	}

	// configOBUs (bytes 4+) must match input seqHeaderOBU
	if !bytes.Equal(config[4:], seqHeaderOBU) {
		t.Error("configOBUs do not match input seqHeaderOBU")
	}
}

func TestBuildAV1DecoderConfig10Bit(t *testing.T) {
	t.Parallel()

	// 640x480, profile 0, level 4, 10-bit, 4:2:0 (from demux/av1_test.go)
	seqHeaderOBU := []byte{
		0x0a, 0x0b, // OBU header (type=1, has_size=1) + LEB128 size=11
		0x00, 0x00, 0x00, 0x24, 0xc4, 0xff, 0xdf, 0x12,
		0xbe, 0x50, 0x10,
	}

	config := BuildAV1DecoderConfig(seqHeaderOBU)
	if config == nil {
		t.Fatal("expected non-nil config")
	}

	// Total length = 4-byte header + len(seqHeaderOBU)
	expectedLen := 4 + len(seqHeaderOBU)
	if len(config) != expectedLen {
		t.Fatalf("total length: got %d, want %d", len(config), expectedLen)
	}

	// Byte 0: marker + version
	if config[0] != 0x81 {
		t.Errorf("byte 0: got 0x%02x, want 0x81", config[0])
	}

	// Byte 1: profile=0 | level=4 → 0x04
	if config[1] != 0x04 {
		t.Errorf("byte 1 (profile|level): got 0x%02x, want 0x04", config[1])
	}

	// Byte 2: tier=0, high_bitdepth=1, twelve_bit=0, mono=0, csx=1, csy=1, csp=0
	// = 0<<7 | 1<<6 | 0<<5 | 0<<4 | 1<<3 | 1<<2 | 0 = 0x4C
	if config[2] != 0x4C {
		t.Errorf("byte 2 (tier/bd/chroma): got 0x%02x, want 0x4C", config[2])
	}

	// configOBUs (bytes 4+) must match input
	if !bytes.Equal(config[4:], seqHeaderOBU) {
		t.Error("configOBUs do not match input seqHeaderOBU")
	}
}

func TestBuildAV1DecoderConfigNil(t *testing.T) {
	t.Parallel()

	if BuildAV1DecoderConfig(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if BuildAV1DecoderConfig([]byte{}) != nil {
		t.Error("expected nil for empty input")
	}
	if BuildAV1DecoderConfig([]byte{0x0a}) != nil {
		t.Error("expected nil for truncated input")
	}

	// Wrong OBU type (OBU_FRAME instead of SEQUENCE_HEADER)
	if BuildAV1DecoderConfig([]byte{0x32, 0x01, 0x00}) != nil {
		t.Error("expected nil for wrong OBU type")
	}
}

func TestBuildHEVCDecoderConfigNil(t *testing.T) {
	t.Parallel()
	if BuildHEVCDecoderConfig(nil, []byte{0x42, 0x01, 0x01, 0x01}, []byte{0x44}) != nil {
		t.Error("expected nil for nil VPS")
	}
	if BuildHEVCDecoderConfig([]byte{0x40}, nil, []byte{0x44}) != nil {
		t.Error("expected nil for nil SPS")
	}
	if BuildHEVCDecoderConfig([]byte{0x40}, []byte{0x42, 0x01, 0x01, 0x01}, nil) != nil {
		t.Error("expected nil for nil PPS")
	}
}
