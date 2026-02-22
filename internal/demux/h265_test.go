package demux

import (
	"testing"
)

func TestHEVCNALType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		firstByte byte
		want      byte
	}{
		{"VPS (32)", 0x40, HEVCNALVPS},
		{"SPS (33)", 0x42, HEVCNALSPS},
		{"PPS (34)", 0x44, HEVCNALPPS},
		{"IDR_W_RADL (19)", 0x26, HEVCNALIDRWRadl},
		{"IDR_N_LP (20)", 0x28, HEVCNALIDRNlp},
		{"CRA (21)", 0x2A, HEVCNALCraNut},
		{"BLA_W_LP (16)", 0x20, HEVCNALBlaWLP},
		{"TRAIL_R (1)", 0x02, 1},
		{"TRAIL_N (0)", 0x00, 0},
		{"SEI_PREFIX (39)", 0x4E, HEVCNALSEIPrefix},
		{"AUD (35)", 0x46, HEVCNALAUD},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HEVCNALType(tt.firstByte)
			if got != tt.want {
				t.Errorf("HEVCNALType(0x%02X) = %d, want %d", tt.firstByte, got, tt.want)
			}
		})
	}
}

func TestIsHEVCKeyframe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		nalType byte
		want    bool
	}{
		{"BLA_W_LP", HEVCNALBlaWLP, true},
		{"IDR_W_RADL", HEVCNALIDRWRadl, true},
		{"IDR_N_LP", HEVCNALIDRNlp, true},
		{"CRA", HEVCNALCraNut, true},
		{"BLA type 17", 17, true},
		{"BLA type 18", 18, true},
		{"TRAIL_N (0)", 0, false},
		{"TRAIL_R (1)", 1, false},
		{"TSA_N (2)", 2, false},
		{"VPS", HEVCNALVPS, false},
		{"SPS", HEVCNALSPS, false},
		{"PPS", HEVCNALPPS, false},
		{"SEI", HEVCNALSEIPrefix, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsHEVCKeyframe(tt.nalType)
			if got != tt.want {
				t.Errorf("IsHEVCKeyframe(%d) = %v, want %v", tt.nalType, got, tt.want)
			}
		})
	}
}

func TestParseAnnexBHEVC(t *testing.T) {
	t.Parallel()
	// Build an HEVC Annex B byte stream: VPS + SPS + PPS + IDR
	// HEVC NAL headers are 2 bytes: forbidden(1) | type(6) | layerID(6) | tid(3)
	data := []byte{
		// 4-byte start code + VPS (type 32: 0x40 0x01)
		0x00, 0x00, 0x00, 0x01, 0x40, 0x01, 0xAA, 0xBB,
		// 4-byte start code + SPS (type 33: 0x42 0x01)
		0x00, 0x00, 0x00, 0x01, 0x42, 0x01, 0xCC, 0xDD,
		// 3-byte start code + PPS (type 34: 0x44 0x01)
		0x00, 0x00, 0x01, 0x44, 0x01, 0xEE,
		// 4-byte start code + IDR_W_RADL (type 19: 0x26 0x01)
		0x00, 0x00, 0x00, 0x01, 0x26, 0x01, 0xFF, 0x00, 0x11,
	}

	nalus := ParseAnnexBHEVC(data)

	if len(nalus) != 4 {
		t.Fatalf("expected 4 NAL units, got %d", len(nalus))
	}

	wantTypes := []byte{HEVCNALVPS, HEVCNALSPS, HEVCNALPPS, HEVCNALIDRWRadl}
	for i, want := range wantTypes {
		if nalus[i].Type != want {
			t.Errorf("NALU[%d]: got type %d, want %d", i, nalus[i].Type, want)
		}
	}

	// VPS should be a keyframe type? No — VPS is not a keyframe.
	if IsHEVCKeyframe(nalus[0].Type) {
		t.Error("VPS should not be keyframe")
	}
	if !IsHEVCKeyframe(nalus[3].Type) {
		t.Error("IDR_W_RADL should be keyframe")
	}
}

func TestParseHEVCSPS(t *testing.T) {
	t.Parallel()
	// Hand-constructed HEVC SPS for Main profile, 320x240, Level 3.1
	// NAL type 33 with 2-byte header: 0x42 0x01
	sps := []byte{
		0x42, 0x01, // NAL header (type=33, layer=0, tid=1)
		0x01,                   // vps_id=0(4b), max_sub_layers_minus1=0(3b), temporal_nesting=1(1b)
		0x01,                   // profile_space=0(2b), tier=0(1b), profile_idc=1(5b) [Main]
		0x40, 0x00, 0x00, 0x00, // profile_compatibility_flags (bit 1 set)
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x00, // constraint_indicator_flags
		0x5D,                         // level_idc = 93 (Level 3.1)
		0xA0, 0x0A, 0x08, 0x0F, 0x10, // sps_id=0, chroma=1, width=320, height=240, conf_win=0
	}

	info, err := ParseHEVCSPS(sps)
	if err != nil {
		t.Fatalf("ParseHEVCSPS error: %v", err)
	}

	if info.Width != 320 {
		t.Errorf("Width: got %d, want 320", info.Width)
	}
	if info.Height != 240 {
		t.Errorf("Height: got %d, want 240", info.Height)
	}
	if info.ProfileIDC != 1 {
		t.Errorf("ProfileIDC: got %d, want 1", info.ProfileIDC)
	}
	if info.TierFlag != 0 {
		t.Errorf("TierFlag: got %d, want 0", info.TierFlag)
	}
	if info.LevelIDC != 93 {
		t.Errorf("LevelIDC: got %d, want 93", info.LevelIDC)
	}
}

func TestHEVCSPSCodecString(t *testing.T) {
	t.Parallel()
	info := HEVCSPSInfo{
		ProfileIDC:                1,
		TierFlag:                  0,
		LevelIDC:                  93,
		ProfileCompatibilityFlags: 0x40000000,
		ConstraintIndicatorFlags:  0xB00000000000,
	}

	got := info.CodecString()
	want := "hev1.1.2.L93.B0"
	if got != want {
		t.Errorf("CodecString() = %q, want %q", got, want)
	}
}

func TestHEVCSPSCodecStringHighTier(t *testing.T) {
	t.Parallel()
	info := HEVCSPSInfo{
		ProfileIDC:                2,
		TierFlag:                  1,
		LevelIDC:                  120,
		ProfileCompatibilityFlags: 0x20000000,
		ConstraintIndicatorFlags:  0x000000000000,
	}

	got := info.CodecString()
	// ProfileIDC=2, Reverse32(0x20000000) = 0x00000004 = 4
	// Tier="H", Level=120, no constraint bytes (all zero)
	want := "hev1.2.4.H120"
	if got != want {
		t.Errorf("CodecString() = %q, want %q", got, want)
	}
}

func TestParseHEVCSPSTooShort(t *testing.T) {
	t.Parallel()
	_, err := ParseHEVCSPS([]byte{0x42, 0x01, 0x01})
	if err == nil {
		t.Error("expected error for too-short HEVC SPS")
	}

	_, err = ParseHEVCSPS(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}

func TestIsHEVCVPSSPSPPS(t *testing.T) {
	t.Parallel()
	if !IsHEVCVPS(HEVCNALVPS) {
		t.Error("IsHEVCVPS should return true for VPS")
	}
	if IsHEVCVPS(HEVCNALSPS) {
		t.Error("IsHEVCVPS should return false for SPS")
	}

	if !IsHEVCSPS(HEVCNALSPS) {
		t.Error("IsHEVCSPS should return true for SPS")
	}
	if IsHEVCSPS(HEVCNALPPS) {
		t.Error("IsHEVCSPS should return false for PPS")
	}

	if !IsHEVCPPS(HEVCNALPPS) {
		t.Error("IsHEVCPPS should return true for PPS")
	}
	if IsHEVCPPS(HEVCNALVPS) {
		t.Error("IsHEVCPPS should return false for VPS")
	}
}
