package demux

import (
	"testing"
)

// Test data generated with ffmpeg using libsvtav1.
// 1920x1080, profile 0, level 8, 8-bit, 4:2:0.
var testSeqHdrOBU1080p = []byte{
	0x0a, 0x0b, // OBU header (type=1, has_size=1) + LEB128 size=11
	0x00, 0x00, 0x00, 0x42, 0xab, 0xbf, 0xc3, 0x71,
	0x2b, 0xe4, 0x01,
}

// 640x480, profile 0, level 4, 10-bit, 4:2:0.
var testSeqHdrOBU480p10 = []byte{
	0x0a, 0x0b, // OBU header (type=1, has_size=1) + LEB128 size=11
	0x00, 0x00, 0x00, 0x24, 0xc4, 0xff, 0xdf, 0x12,
	0xbe, 0x50, 0x10,
}

// Temporal unit containing TD + seq header + key frame (1920x1080 8-bit).
var testTemporalUnitKeyframe = []byte{
	0x12, 0x00, // OBU_TEMPORAL_DELIMITER, has_size=1, size=0
	0x0a, 0x0b, // OBU_SEQUENCE_HEADER, has_size=1, size=11
	0x00, 0x00, 0x00, 0x42, 0xab, 0xbf, 0xc3, 0x71,
	0x2b, 0xe4, 0x01,
	0x32, 0x2d, // OBU_FRAME, has_size=1, size=45
	0x10, 0x00, 0x8b, 0x00, 0x81, 0x45, 0x08, 0x20,
	0x40, 0x00, 0x00, 0x03, 0x24, 0xfe, 0x6a, 0x75,
	0xf5, 0xb5, 0x7d, 0xaa, 0x9a, 0x90, 0x9b, 0xe8,
	0x41, 0xd4, 0x1a, 0x98, 0x69, 0x56, 0xfa, 0xf0,
	0xa8, 0xe6, 0xda, 0x81, 0x0c, 0x1c, 0xe9, 0xc2,
	0xf3, 0x84, 0xbd, 0x86, 0xab,
}

// Temporal unit containing TD + inter frame (no sequence header).
var testTemporalUnitInterFrame = []byte{
	0x12, 0x00, // OBU_TEMPORAL_DELIMITER, has_size=1, size=0
	0x32, 0x18, // OBU_FRAME, has_size=1, size=24
	0x30, 0x02, 0x04, 0x09, 0x24, 0x92, 0x22, 0x46,
	0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0xbc,
	0x32, 0xc1, 0x64, 0xdc, 0x91, 0xe8, 0x51, 0xe8,
}

func TestParseAV1SequenceHeader1080p(t *testing.T) {
	t.Parallel()

	hdr, err := ParseAV1SequenceHeader(testSeqHdrOBU1080p)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader error: %v", err)
	}

	if hdr.SeqProfile != 0 {
		t.Errorf("SeqProfile: got %d, want 0", hdr.SeqProfile)
	}
	if hdr.SeqLevelIdx != 8 {
		t.Errorf("SeqLevelIdx: got %d, want 8", hdr.SeqLevelIdx)
	}
	if hdr.SeqTier != 0 {
		t.Errorf("SeqTier: got %d, want 0", hdr.SeqTier)
	}
	if hdr.BitDepth != 8 {
		t.Errorf("BitDepth: got %d, want 8", hdr.BitDepth)
	}
	if hdr.Width != 1920 {
		t.Errorf("Width: got %d, want 1920", hdr.Width)
	}
	if hdr.Height != 1080 {
		t.Errorf("Height: got %d, want 1080", hdr.Height)
	}
	if hdr.ChromaSubsamplingX != 1 {
		t.Errorf("ChromaSubsamplingX: got %d, want 1", hdr.ChromaSubsamplingX)
	}
	if hdr.ChromaSubsamplingY != 1 {
		t.Errorf("ChromaSubsamplingY: got %d, want 1", hdr.ChromaSubsamplingY)
	}
	if hdr.Monochrome {
		t.Error("Monochrome: got true, want false")
	}
}

func TestParseAV1SequenceHeader480p10Bit(t *testing.T) {
	t.Parallel()

	hdr, err := ParseAV1SequenceHeader(testSeqHdrOBU480p10)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader error: %v", err)
	}

	if hdr.SeqProfile != 0 {
		t.Errorf("SeqProfile: got %d, want 0", hdr.SeqProfile)
	}
	if hdr.SeqLevelIdx != 4 {
		t.Errorf("SeqLevelIdx: got %d, want 4", hdr.SeqLevelIdx)
	}
	if hdr.SeqTier != 0 {
		t.Errorf("SeqTier: got %d, want 0", hdr.SeqTier)
	}
	if hdr.BitDepth != 10 {
		t.Errorf("BitDepth: got %d, want 10", hdr.BitDepth)
	}
	if hdr.Width != 640 {
		t.Errorf("Width: got %d, want 640", hdr.Width)
	}
	if hdr.Height != 480 {
		t.Errorf("Height: got %d, want 480", hdr.Height)
	}
	if hdr.ChromaSubsamplingX != 1 {
		t.Errorf("ChromaSubsamplingX: got %d, want 1", hdr.ChromaSubsamplingX)
	}
	if hdr.ChromaSubsamplingY != 1 {
		t.Errorf("ChromaSubsamplingY: got %d, want 1", hdr.ChromaSubsamplingY)
	}
	if hdr.Monochrome {
		t.Error("Monochrome: got true, want false")
	}
}

func TestParseAV1SequenceHeaderNil(t *testing.T) {
	t.Parallel()

	_, err := ParseAV1SequenceHeader(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}

func TestParseAV1SequenceHeaderEmpty(t *testing.T) {
	t.Parallel()

	_, err := ParseAV1SequenceHeader([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseAV1SequenceHeaderTruncated(t *testing.T) {
	t.Parallel()

	// Just the OBU header, no payload
	_, err := ParseAV1SequenceHeader([]byte{0x0a, 0x0b})
	if err == nil {
		t.Error("expected error for truncated input")
	}

	// Partial payload
	_, err = ParseAV1SequenceHeader([]byte{0x0a, 0x0b, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for truncated payload")
	}
}

func TestParseAV1SequenceHeaderWrongOBUType(t *testing.T) {
	t.Parallel()

	// OBU_FRAME (type 6) instead of SEQUENCE_HEADER
	_, err := ParseAV1SequenceHeader([]byte{0x32, 0x01, 0x00})
	if err == nil {
		t.Error("expected error for wrong OBU type")
	}
}

func TestAV1CodecString1080p(t *testing.T) {
	t.Parallel()

	hdr, err := ParseAV1SequenceHeader(testSeqHdrOBU1080p)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader error: %v", err)
	}

	want := "av01.0.08M.08"
	got := hdr.CodecString()
	if got != want {
		t.Errorf("CodecString: got %q, want %q", got, want)
	}
}

func TestAV1CodecString480p10Bit(t *testing.T) {
	t.Parallel()

	hdr, err := ParseAV1SequenceHeader(testSeqHdrOBU480p10)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader error: %v", err)
	}

	want := "av01.0.04M.10"
	got := hdr.CodecString()
	if got != want {
		t.Errorf("CodecString: got %q, want %q", got, want)
	}
}

func TestAV1CodecStringHighTier(t *testing.T) {
	t.Parallel()

	hdr := &AV1SequenceHeader{
		SeqProfile:  1,
		SeqLevelIdx: 13,
		SeqTier:     1,
		BitDepth:    10,
	}

	want := "av01.1.13H.10"
	got := hdr.CodecString()
	if got != want {
		t.Errorf("CodecString: got %q, want %q", got, want)
	}
}

func TestAV1CodecStringZeroPadding(t *testing.T) {
	t.Parallel()

	hdr := &AV1SequenceHeader{
		SeqProfile:  0,
		SeqLevelIdx: 1,
		SeqTier:     0,
		BitDepth:    8,
	}

	want := "av01.0.01M.08"
	got := hdr.CodecString()
	if got != want {
		t.Errorf("CodecString: got %q, want %q", got, want)
	}
}

func TestIsAV1KeyframeTrue(t *testing.T) {
	t.Parallel()

	if !IsAV1Keyframe(testTemporalUnitKeyframe) {
		t.Error("expected true for keyframe temporal unit")
	}
}

func TestIsAV1KeyframeFalseInterFrame(t *testing.T) {
	t.Parallel()

	if IsAV1Keyframe(testTemporalUnitInterFrame) {
		t.Error("expected false for inter frame temporal unit")
	}
}

func TestIsAV1KeyframeFalseNil(t *testing.T) {
	t.Parallel()

	if IsAV1Keyframe(nil) {
		t.Error("expected false for nil input")
	}
}

func TestIsAV1KeyframeFalseEmpty(t *testing.T) {
	t.Parallel()

	if IsAV1Keyframe([]byte{}) {
		t.Error("expected false for empty input")
	}
}

func TestIsAV1KeyframeFalseTruncated(t *testing.T) {
	t.Parallel()

	// Just a temporal delimiter, no frame OBU
	if IsAV1Keyframe([]byte{0x12, 0x00}) {
		t.Error("expected false for temporal delimiter only")
	}
}

func TestFindSequenceHeaderOBUPresent(t *testing.T) {
	t.Parallel()

	obu := FindSequenceHeaderOBU(testTemporalUnitKeyframe)
	if obu == nil {
		t.Fatal("expected non-nil OBU")
	}

	// Verify the found OBU is a valid sequence header
	hdr, err := ParseAV1SequenceHeader(obu)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader on found OBU: %v", err)
	}
	if hdr.Width != 1920 || hdr.Height != 1080 {
		t.Errorf("resolution: got %dx%d, want 1920x1080", hdr.Width, hdr.Height)
	}
}

func TestFindSequenceHeaderOBUAbsent(t *testing.T) {
	t.Parallel()

	obu := FindSequenceHeaderOBU(testTemporalUnitInterFrame)
	if obu != nil {
		t.Errorf("expected nil for inter frame, got %d bytes", len(obu))
	}
}

func TestFindSequenceHeaderOBUNil(t *testing.T) {
	t.Parallel()

	obu := FindSequenceHeaderOBU(nil)
	if obu != nil {
		t.Error("expected nil for nil input")
	}
}

func TestFindSequenceHeaderOBUEmpty(t *testing.T) {
	t.Parallel()

	obu := FindSequenceHeaderOBU([]byte{})
	if obu != nil {
		t.Error("expected nil for empty input")
	}
}

func TestReadLEB128(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		want    int
		wantN   int
		wantErr bool
	}{
		{
			name:  "zero",
			data:  []byte{0x00},
			want:  0,
			wantN: 1,
		},
		{
			name:  "one byte value 11",
			data:  []byte{0x0b},
			want:  11,
			wantN: 1,
		},
		{
			name:  "one byte value 127",
			data:  []byte{0x7f},
			want:  127,
			wantN: 1,
		},
		{
			name:  "two byte value 128",
			data:  []byte{0x80, 0x01},
			want:  128,
			wantN: 2,
		},
		{
			name:  "two byte value 300",
			data:  []byte{0xac, 0x02},
			want:  300,
			wantN: 2,
		},
		{
			name:    "empty",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "unterminated",
			data:    []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			val, n, err := readLEB128(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if val != tt.want {
				t.Errorf("value: got %d, want %d", val, tt.want)
			}
			if n != tt.wantN {
				t.Errorf("bytes consumed: got %d, want %d", n, tt.wantN)
			}
		})
	}
}

func TestParseOBUHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        []byte
		wantType    int
		wantHasSize bool
		wantHasExt  bool
		wantErr     bool
	}{
		{
			name:        "sequence header with size",
			data:        []byte{0x0a, 0x0b, 0x00, 0x00},
			wantType:    OBUSequenceHeader,
			wantHasSize: true,
		},
		{
			name:        "temporal delimiter with size",
			data:        []byte{0x12, 0x00},
			wantType:    OBUTemporalDelimiter,
			wantHasSize: true,
		},
		{
			name:        "frame with size",
			data:        []byte{0x32, 0x2d, 0x10},
			wantType:    OBUFrame,
			wantHasSize: true,
		},
		{
			name:    "forbidden bit set",
			data:    []byte{0x8a},
			wantErr: true,
		},
		{
			name:    "empty",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:        "extension flag",
			data:        []byte{0x0e, 0x40, 0x0b, 0x00, 0x00},
			wantType:    OBUSequenceHeader,
			wantHasExt:  true,
			wantHasSize: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := parseOBUHeader(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.obuType != tt.wantType {
				t.Errorf("obuType: got %d, want %d", h.obuType, tt.wantType)
			}
			if h.hasSizeField != tt.wantHasSize {
				t.Errorf("hasSizeField: got %v, want %v", h.hasSizeField, tt.wantHasSize)
			}
			if h.hasExtension != tt.wantHasExt {
				t.Errorf("hasExtension: got %v, want %v", h.hasExtension, tt.wantHasExt)
			}
		})
	}
}

func TestAV1BitReaderOverflow(t *testing.T) {
	t.Parallel()

	br := newAV1BitReader([]byte{0xFF})
	_ = br.readBits(8) // should succeed
	_ = br.readBits(1) // should set error

	if br.err == nil {
		t.Error("expected error after reading past end")
	}

	// Subsequent reads should return 0 without panicking
	val := br.readBits(8)
	if val != 0 {
		t.Errorf("expected 0 after error, got %d", val)
	}
}

func TestAV1BitReaderReadBits(t *testing.T) {
	t.Parallel()

	// 0xA5 = 10100101
	br := newAV1BitReader([]byte{0xA5})

	got := br.readBits(3)
	if got != 5 { // 101
		t.Errorf("first 3 bits: got %d, want 5", got)
	}

	got = br.readBits(5)
	if got != 5 { // 00101
		t.Errorf("next 5 bits: got %d, want 5", got)
	}

	if br.err != nil {
		t.Errorf("unexpected error: %v", br.err)
	}
}

func TestAV1BitReaderUvlc(t *testing.T) {
	t.Parallel()

	// UVLC encoding of 0: just a single 1-bit → value 0
	br := newAV1BitReader([]byte{0x80}) // 10000000
	got := br.readUvlc()
	if got != 0 {
		t.Errorf("uvlc(0): got %d, want 0", got)
	}

	// UVLC encoding of 1: 0,1,0 → leadingZeros=1, val=0, result = 0 + (1<<1) - 1 = 1
	br = newAV1BitReader([]byte{0x40}) // 01000000
	got = br.readUvlc()
	if got != 1 {
		t.Errorf("uvlc(1): got %d, want 1", got)
	}

	// UVLC encoding of 2: 0,1,1 → leadingZeros=1, val=1, result = 1 + (1<<1) - 1 = 2
	br = newAV1BitReader([]byte{0x60}) // 01100000
	got = br.readUvlc()
	if got != 2 {
		t.Errorf("uvlc(2): got %d, want 2", got)
	}
}

func TestReducedStillPictureHeader(t *testing.T) {
	t.Parallel()

	// Hand-craft a minimal sequence header with reduced_still_picture_header=1
	// Profile 0, still_picture=1, reduced=1, level_idx=1 (5 bits)
	// Then frame_width_bits_minus_1=3, frame_height_bits_minus_1=3 (4+4 bits)
	// Then width=128-1=127 (4 bits), height=96-1=95 (4 bits)
	// No frame_id_numbers_present (since reduced)
	// use_128x128_superblock=0, enable_filter_intra=0, enable_intra_edge_filter=0
	// No interintra/masked/etc (since reduced)
	// enable_superres=0, enable_cdef=0, enable_restoration=0
	// color_config: high_bitdepth=0 → 8-bit, mono_chrome=0
	// color_description_present=0 → unspecified
	// color_range=0
	// chroma_sample_position=0 (2 bits, since profile 0 → 4:2:0)
	// separate_uv_delta_q=0

	// Bits: profile(3)=000, still(1)=1, reduced(1)=1
	//       level_idx(5)=00001
	//       fw_bits(4)=0011, fh_bits(4)=0011
	//       width(4)=0111 1111, height(4)=0101 1111
	// Wait, width is fw_bits+1=4 bits, so 127 in 4 bits doesn't fit.
	// Let's use smaller: width=16, height=16
	// fw_bits_m1=3 → 4 bits for width, fh_bits_m1=3 → 4 bits for height
	// width-1=15=0b1111, height-1=15=0b1111
	// Then: use_128x128_sb=0, filter_intra=0, intra_edge_filter=0 (3 bits)
	// enable_superres=0, enable_cdef=0, enable_restoration=0 (3 bits)
	// high_bitdepth=0 (1 bit)
	// mono_chrome=0 (1 bit, profile != 1)
	// color_desc_present=0 (1 bit)
	// color_range=0 (1 bit)
	// chroma_sample_position=00 (2 bits)
	// separate_uv_delta_q=0 (1 bit)

	// Layout:
	// byte 0: 000 1 1 000 = 0x18
	// byte 1: 01 0011 00 = 0x4C (level_idx=00001, fw_bits=0011, start of fh_bits)
	// byte 2: 11 1111 11 = 0xFF (fh_bits last 2 bits=11, width=1111, height first 2=11)
	// byte 3: 11 000 000 = 0xC0 (height last 2=11, sb=0, filter_intra=0, intra_edge=0, superres=0, cdef=0)
	// byte 4: 0 0 0 0 0 0 00 = 0x00 (restoration=0, high_bitdepth=0, mono=0, cdp=0, color_range=0, csp=00, sep_uv=0)

	// Actually let me be more careful:
	// Bit stream (MSB first):
	// [0]   profile: 000
	// [3]   still_picture: 1
	// [4]   reduced_still_picture_header: 1
	// [5]   seq_level_idx_0: 00001
	// [10]  frame_width_bits_minus_1: 0011
	// [14]  frame_height_bits_minus_1: 0011
	// [18]  max_frame_width_minus_1 (4 bits): 1111
	// [22]  max_frame_height_minus_1 (4 bits): 1111
	// [26]  use_128x128_superblock: 0
	// [27]  enable_filter_intra: 0
	// [28]  enable_intra_edge_filter: 0
	// [29]  enable_superres: 0
	// [30]  enable_cdef: 0
	// [31]  enable_restoration: 0
	// [32]  high_bitdepth: 0
	// [33]  mono_chrome: 0
	// [34]  color_description_present_flag: 0
	// [35]  color_range: 0
	// [36]  chroma_sample_position: 00
	// [38]  separate_uv_delta_q: 0

	// Byte layout:
	// bits 0-7:   000 1 1 000  = 0x18
	// bits 8-15:  01 0011 00   = 0x4C
	// bits 16-23: 11 1111 11   = 0xFF
	// bits 24-31: 11 000000    = 0xC0
	// bits 32-39: 00000 000    = 0x00

	payload := []byte{0x18, 0x4C, 0xFF, 0xC0, 0x00}

	// Wrap in OBU header: type=1, has_size=1
	obu := make([]byte, 0, 2+len(payload))
	obu = append(obu, 0x0a)               // OBU header: type=1, has_size=1
	obu = append(obu, byte(len(payload))) // LEB128 size
	obu = append(obu, payload...)

	hdr, err := ParseAV1SequenceHeader(obu)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader error: %v", err)
	}

	if hdr.SeqProfile != 0 {
		t.Errorf("SeqProfile: got %d, want 0", hdr.SeqProfile)
	}
	if hdr.SeqLevelIdx != 1 {
		t.Errorf("SeqLevelIdx: got %d, want 1", hdr.SeqLevelIdx)
	}
	if hdr.SeqTier != 0 {
		t.Errorf("SeqTier: got %d, want 0", hdr.SeqTier)
	}
	if hdr.Width != 16 {
		t.Errorf("Width: got %d, want 16", hdr.Width)
	}
	if hdr.Height != 16 {
		t.Errorf("Height: got %d, want 16", hdr.Height)
	}
	if hdr.BitDepth != 8 {
		t.Errorf("BitDepth: got %d, want 8", hdr.BitDepth)
	}
}

func TestParseSequenceHeaderWithDecoderModelInfo(t *testing.T) {
	t.Parallel()

	// Hand-craft a sequence header payload with decoder_model_info and
	// operating_parameters_info to exercise the code path where
	// decoder_model_present_for_this_op=1 requires reading additional fields.
	//
	// Profile 0, still_picture=0, reduced_still_picture_header=0
	// timing_info_present=1, num_units_in_display_tick=1(32b), time_scale=30(32b)
	// equal_picture_interval=0
	// decoder_model_info_present_flag=1
	//   buffer_delay_length_minus_1=4 (5b, so delays are 5 bits each)
	//   num_units_in_decoding_tick=1 (32b)
	//   buffer_removal_time_length_minus_1=0 (5b)
	//   frame_presentation_time_length_minus_1=0 (5b)
	// initial_display_delay_present_flag=0
	// operating_points_cnt_minus_1=0 (5b = 00000, so 1 operating point)
	//   operating_point_idc[0]=0 (12b)
	//   seq_level_idx[0]=8 (5b=01000), tier not read (level<=7 check: 8>7 so tier read)
	//   seq_tier[0]=0 (1b)
	//   decoder_model_present_for_this_op[0]=1 (1b)
	//     decoder_buffer_delay[0]=0 (5b, since buffer_delay_length_minus_1=4)
	//     encoder_buffer_delay[0]=0 (5b)
	//     low_delay_mode_flag[0]=0 (1b)
	// frame_width_bits_minus_1=3 (4b=0011), frame_height_bits_minus_1=3 (4b=0011)
	// max_frame_width_minus_1=15 (4b=1111), max_frame_height_minus_1=15 (4b=1111)
	// frame_id_numbers_present=0
	// use_128x128_superblock=0, enable_filter_intra=0, enable_intra_edge_filter=0
	// enable_interintra_compound=0, enable_masked_compound=0, enable_warped_motion=0
	// enable_dual_filter=0, enable_order_hint=0
	// seq_choose_screen_content_tools=1
	// enable_superres=0, enable_cdef=0, enable_restoration=0
	// color_config: high_bitdepth=0, mono_chrome=0, color_description_present=0
	// color_range=0, chroma_sample_position=00, separate_uv_delta_q=0

	// Bit-by-bit layout:
	// [0-2]   seq_profile: 000
	// [3]     still_picture: 0
	// [4]     reduced_still_picture_header: 0
	// [5]     timing_info_present_flag: 1
	// [6-37]  num_units_in_display_tick: 00000000 00000000 00000000 00000001
	// [38-69] time_scale: 00000000 00000000 00000000 00011110
	// [70]    equal_picture_interval: 0
	// [71]    decoder_model_info_present_flag: 1
	// [72-76] buffer_delay_length_minus_1: 00100 (=4, so delays are 5 bits)
	// [77-108] num_units_in_decoding_tick: 00000000 00000000 00000000 00000001
	// [109-113] buffer_removal_time_length_minus_1: 00000
	// [114-118] frame_presentation_time_length_minus_1: 00000
	// [119]   initial_display_delay_present_flag: 0
	// [120-124] operating_points_cnt_minus_1: 00000
	// [125-136] operating_point_idc[0]: 000000000000
	// [137-141] seq_level_idx[0]: 01000 (=8)
	// [142]   seq_tier[0]: 0 (level>7 so tier is read)
	// [143]   decoder_model_present_for_this_op[0]: 1
	// [144-148] decoder_buffer_delay[0]: 00000
	// [149-153] encoder_buffer_delay[0]: 00000
	// [154]   low_delay_mode_flag[0]: 0
	// [155-158] frame_width_bits_minus_1: 0011
	// [159-162] frame_height_bits_minus_1: 0011
	// [163-166] max_frame_width_minus_1: 1111
	// [167-170] max_frame_height_minus_1: 1111
	// [171]   frame_id_numbers_present_flag: 0
	// [172]   use_128x128_superblock: 0
	// [173]   enable_filter_intra: 0
	// [174]   enable_intra_edge_filter: 0
	// [175]   enable_interintra_compound: 0
	// [176]   enable_masked_compound: 0
	// [177]   enable_warped_motion: 0
	// [178]   enable_dual_filter: 0
	// [179]   enable_order_hint: 0
	// [180]   seq_choose_screen_content_tools: 1
	// [181]   enable_superres: 0
	// [182]   enable_cdef: 0
	// [183]   enable_restoration: 0
	// [184]   high_bitdepth: 0
	// [185]   mono_chrome: 0
	// [186]   color_description_present_flag: 0
	// [187]   color_range: 0
	// [188-189] chroma_sample_position: 00
	// [190]   separate_uv_delta_q: 0

	// Convert bit positions to bytes:
	// byte 0  [0-7]:   00000 1 00 = 0x04
	// byte 1  [8-15]:  00000000
	// byte 2  [16-23]: 00000000
	// byte 3  [24-31]: 00000000
	// byte 4  [32-39]: 00000001
	// byte 5  [40-47]: 00000000
	// byte 6  [48-55]: 00000000
	// byte 7  [56-63]: 00000000
	// byte 8  [64-71]: 00011110 = 0x1E (bits 64-69 = time_scale last, 70=equal_pic=0, 71=dmi=1)
	//   Wait, let me redo this more carefully.

	// Let me just build it with a helper to be precise.
	bits := []uint{} // each element is 0 or 1

	appendBits := func(val uint, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, (val>>uint(i))&1)
		}
	}

	appendBits(0, 3)   // seq_profile = 0
	appendBits(0, 1)   // still_picture = 0
	appendBits(0, 1)   // reduced_still_picture_header = 0
	appendBits(1, 1)   // timing_info_present_flag = 1
	appendBits(1, 32)  // num_units_in_display_tick = 1
	appendBits(30, 32) // time_scale = 30
	appendBits(0, 1)   // equal_picture_interval = 0
	appendBits(1, 1)   // decoder_model_info_present_flag = 1
	appendBits(4, 5)   // buffer_delay_length_minus_1 = 4
	appendBits(1, 32)  // num_units_in_decoding_tick = 1
	appendBits(0, 5)   // buffer_removal_time_length_minus_1 = 0
	appendBits(0, 5)   // frame_presentation_time_length_minus_1 = 0
	appendBits(0, 1)   // initial_display_delay_present_flag = 0
	appendBits(0, 5)   // operating_points_cnt_minus_1 = 0
	appendBits(0, 12)  // operating_point_idc[0] = 0
	appendBits(8, 5)   // seq_level_idx[0] = 8
	appendBits(0, 1)   // seq_tier[0] = 0 (read because level > 7)
	appendBits(1, 1)   // decoder_model_present_for_this_op[0] = 1
	appendBits(0, 5)   // decoder_buffer_delay[0] = 0 (5 bits)
	appendBits(0, 5)   // encoder_buffer_delay[0] = 0 (5 bits)
	appendBits(0, 1)   // low_delay_mode_flag[0] = 0
	appendBits(3, 4)   // frame_width_bits_minus_1 = 3
	appendBits(3, 4)   // frame_height_bits_minus_1 = 3
	appendBits(15, 4)  // max_frame_width_minus_1 = 15 (width=16)
	appendBits(15, 4)  // max_frame_height_minus_1 = 15 (height=16)
	appendBits(0, 1)   // frame_id_numbers_present_flag = 0
	appendBits(0, 1)   // use_128x128_superblock = 0
	appendBits(0, 1)   // enable_filter_intra = 0
	appendBits(0, 1)   // enable_intra_edge_filter = 0
	appendBits(0, 1)   // enable_interintra_compound = 0
	appendBits(0, 1)   // enable_masked_compound = 0
	appendBits(0, 1)   // enable_warped_motion = 0
	appendBits(0, 1)   // enable_dual_filter = 0
	appendBits(0, 1)   // enable_order_hint = 0
	appendBits(1, 1)   // seq_choose_screen_content_tools = 1 (SELECT)
	// seqForceScreenContentTools = 2 (SELECT), which is > 0
	appendBits(1, 1) // seq_choose_integer_mv = 1 (SELECT)
	appendBits(0, 1) // enable_superres = 0
	appendBits(0, 1) // enable_cdef = 0
	appendBits(0, 1) // enable_restoration = 0
	appendBits(0, 1) // high_bitdepth = 0 → 8-bit
	appendBits(0, 1) // mono_chrome = 0
	appendBits(0, 1) // color_description_present_flag = 0
	appendBits(0, 1) // color_range = 0
	appendBits(0, 2) // chroma_sample_position = 0
	appendBits(0, 1) // separate_uv_delta_q = 0

	// Pack bits into bytes
	payload := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b == 1 {
			payload[i/8] |= 1 << (7 - uint(i%8))
		}
	}

	// Wrap in OBU header
	obu := make([]byte, 0, 2+len(payload))
	obu = append(obu, 0x0a)               // OBU header: type=1, has_size=1
	obu = append(obu, byte(len(payload))) // LEB128 size
	obu = append(obu, payload...)

	hdr, err := ParseAV1SequenceHeader(obu)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader error: %v", err)
	}

	if hdr.SeqProfile != 0 {
		t.Errorf("SeqProfile: got %d, want 0", hdr.SeqProfile)
	}
	if hdr.SeqLevelIdx != 8 {
		t.Errorf("SeqLevelIdx: got %d, want 8", hdr.SeqLevelIdx)
	}
	if hdr.SeqTier != 0 {
		t.Errorf("SeqTier: got %d, want 0", hdr.SeqTier)
	}
	if hdr.Width != 16 {
		t.Errorf("Width: got %d, want 16", hdr.Width)
	}
	if hdr.Height != 16 {
		t.Errorf("Height: got %d, want 16", hdr.Height)
	}
	if hdr.BitDepth != 8 {
		t.Errorf("BitDepth: got %d, want 8", hdr.BitDepth)
	}
}

func TestIsAV1KeyframeFrameHeaderOBU(t *testing.T) {
	t.Parallel()

	// Construct a temporal unit with OBU_FRAME_HEADER (type 3) instead of OBU_FRAME
	// OBU_FRAME_HEADER: type=3, has_size=1 → (3<<3)|2 = 0x1A
	// Payload: show_existing=0, frame_type=0 (KEY) → first byte = 0b0_00_xxxxx = 0x00
	tu := []byte{
		0x12, 0x00, // TD
		0x1a, 0x02, // OBU_FRAME_HEADER, size=2
		0x00, 0x00, // show_existing=0, frame_type=0 (KEY)
	}

	if !IsAV1Keyframe(tu) {
		t.Error("expected true for OBU_FRAME_HEADER with KEY_FRAME type")
	}
}

func TestIsAV1KeyframeShowExistingFrame(t *testing.T) {
	t.Parallel()

	// show_existing_frame = 1 → first payload byte: 1_xx_xxxxx = 0x80
	tu := []byte{
		0x12, 0x00, // TD
		0x32, 0x02, // OBU_FRAME, size=2
		0x80, 0x00, // show_existing_frame=1
	}

	if IsAV1Keyframe(tu) {
		t.Error("expected false for show_existing_frame=1")
	}
}

func TestFindSequenceHeaderOBUTruncated(t *testing.T) {
	t.Parallel()

	// Sequence header OBU header but truncated payload
	data := []byte{0x0a, 0x80} // has_size with unterminated LEB128
	obu := FindSequenceHeaderOBU(data)
	if obu != nil {
		t.Error("expected nil for truncated OBU")
	}
}
