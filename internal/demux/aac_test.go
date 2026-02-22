package demux

import (
	"testing"
)

func TestParseADTS(t *testing.T) {
	t.Parallel()
	// Build a minimal ADTS frame
	// ADTS header (7 bytes, no CRC):
	// Sync word: 0xFFF
	// MPEG-4, Layer 0, no CRC
	// Profile: AAC-LC (1), Sample rate: 48kHz (3), Channels: 2
	frameData := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	frameLen := 7 + len(frameData) // header + payload

	header := make([]byte, 7)
	header[0] = 0xFF
	header[1] = 0xF1 // MPEG-4, Layer 0, no CRC protection
	// Profile=1(AAC-LC), SampleRate=3(48kHz), private=0, channel=2
	header[2] = 0x50 | (3 << 2)     // profile(2bits) | sampleRateIdx(4bits)
	header[2] = (1 << 6) | (3 << 2) // AAC-LC=01, 48kHz idx=3
	header[3] = (2 << 6)            // channel config = 2 (upper 2 bits + 1 from prev byte)

	// Actually let me construct this more carefully
	// Byte 2: [profile:2][sampling_freq_idx:4][private:1][channel_cfg_hi:1]
	// profile = 1 (AAC-LC), sampling_freq_idx = 3 (48kHz), private = 0, channel_cfg_hi = 0
	header[2] = (1 << 6) | (3 << 2) // 0x4C

	// Byte 3: [channel_cfg_lo:2][original_copy:1][home:1][copyright_id:1][copyright_start:1][frame_length_hi:2]
	// channel_cfg = 2 (stereo), so lo bits = 10, others = 0
	header[3] = (2 << 6) | byte((frameLen>>11)&0x03)

	// Byte 4: [frame_length_mid:8]
	header[4] = byte((frameLen >> 3) & 0xFF)

	// Byte 5: [frame_length_lo:3][buffer_fullness_hi:5]
	header[5] = byte((frameLen&0x07)<<5) | 0x1F // buffer fullness = 0x7FF (VBR)

	// Byte 6: [buffer_fullness_lo:6][num_frames_minus1:2]
	header[6] = 0xFC // buffer fullness (continued) + 0 frames-1

	adts := append(header, frameData...)

	frames, err := ParseADTS(adts)
	if err != nil {
		t.Fatalf("ParseADTS failed: %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}

	if frames[0].SampleRate != 48000 {
		t.Errorf("expected sample rate 48000, got %d", frames[0].SampleRate)
	}

	if frames[0].Channels != 2 {
		t.Errorf("expected 2 channels, got %d", frames[0].Channels)
	}

	if len(frames[0].Data) != frameLen {
		t.Errorf("expected frame data length %d, got %d", frameLen, len(frames[0].Data))
	}
}

func TestParseADTSEmpty(t *testing.T) {
	t.Parallel()
	frames, err := ParseADTS(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 0 {
		t.Errorf("expected 0 frames for empty input, got %d", len(frames))
	}
}

func TestParseADTSTruncated(t *testing.T) {
	t.Parallel()
	// Just a sync word, not enough for a full header
	data := []byte{0xFF, 0xF1, 0x50, 0x80, 0x00}
	frames, err := ParseADTS(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return 0 frames since there's not enough data
	if len(frames) != 0 {
		t.Errorf("expected 0 frames for truncated input, got %d", len(frames))
	}
}
