package dash

import (
	"bytes"
	"testing"

	"github.com/Eyevinn/mp4ff/aac"
	"github.com/Eyevinn/mp4ff/av1"
	"github.com/Eyevinn/mp4ff/mp4"
)

func TestScaleToMicroseconds(t *testing.T) {
	tests := []struct {
		name      string
		ts        uint64
		timescale uint32
		want      int64
	}{
		{"zero", 0, 90000, 0},
		{"one second at 90kHz", 90000, 90000, 1_000_000},
		{"half second at 48kHz", 24000, 48000, 500_000},
		{"two seconds at 90kHz", 180000, 90000, 2_000_000},
		{"one frame at 30fps/90kHz", 3000, 90000, 33333},
		{"one sample at 44100", 1, 44100, 22}, // 1_000_000/44100 = 22.67 -> 22
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaleToMicroseconds(tt.ts, tt.timescale)
			if got != tt.want {
				t.Errorf("scaleToMicroseconds(%d, %d) = %d, want %d",
					tt.ts, tt.timescale, got, tt.want)
			}
		})
	}
}

func TestParseInitSegmentNil(t *testing.T) {
	_, err := parseInitSegment(nil)
	if err == nil {
		t.Error("expected error for nil data")
	}

	_, err = parseInitSegment([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestParseInitSegmentInvalid(t *testing.T) {
	_, err := parseInitSegment([]byte("not valid mp4 data at all"))
	if err == nil {
		t.Error("expected error for invalid data")
	}
}

func TestParseMediaSegmentNil(t *testing.T) {
	_, err := parseMediaSegment(nil, &initSegmentInfo{}, true)
	if err == nil {
		t.Error("expected error for nil data")
	}

	_, err = parseMediaSegment([]byte{}, &initSegmentInfo{}, true)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestParseMediaSegmentNilInit(t *testing.T) {
	_, err := parseMediaSegment([]byte{0x00}, nil, true)
	if err == nil {
		t.Error("expected error for nil init info")
	}
}

// buildAV1InitSegment creates a minimal AV1+AAC init segment using mp4ff
// builder APIs. Returns the encoded bytes.
func buildAV1InitSegment(t *testing.T) []byte {
	t.Helper()

	init := mp4.CreateEmptyInit()

	// Add video track (AV1, 1920x1080, 90kHz timescale)
	videoTrak := init.AddEmptyTrack(90000, "video", "und")
	videoTrak.Tkhd.Width = mp4.Fixed32(1920 << 16)
	videoTrak.Tkhd.Height = mp4.Fixed32(1080 << 16)

	// Create an Av1C box with a minimal AV1 codec config
	av1C := &mp4.Av1CBox{
		CodecConfRec: av1.CodecConfRec{
			Version:      1,
			SeqProfile:   0,
			SeqLevelIdx0: 9,
			SeqTier0:     0,
			HighBitdepth: 0,
			// Build a minimal sequence header OBU for ConfigOBUs.
			// OBU type 1 (seq header), has_size=1:
			//   header byte: 0b0_0001_0_1_0 = 0x0A
			//   size: LEB128 for 4 bytes = 0x04
			//   payload: 4 bytes of minimal data
			ConfigOBUs: buildMinimalSeqHeaderOBU(),
		},
	}

	// Create AV1 visual sample entry and add to stsd
	av01 := mp4.CreateVisualSampleEntryBox("av01", 1920, 1080, av1C)
	videoTrak.Mdia.Minf.Stbl.Stsd.AddChild(av01)

	// Add audio track (AAC, 48kHz, stereo)
	audioTrak := init.AddEmptyTrack(48000, "audio", "und")
	err := audioTrak.SetAACDescriptor(aac.AAClc, 48000)
	if err != nil {
		t.Fatalf("SetAACDescriptor: %v", err)
	}

	var buf bytes.Buffer
	err = init.Encode(&buf)
	if err != nil {
		t.Fatalf("encode init segment: %v", err)
	}
	return buf.Bytes()
}

// buildMinimalSeqHeaderOBU returns a minimal AV1 sequence header OBU with
// size field. This is not a fully valid sequence header but contains enough
// structure for FindSequenceHeaderOBU to locate it.
func buildMinimalSeqHeaderOBU() []byte {
	// OBU header: type=1 (seq header), has_size=1 => 0x0A
	// Size in LEB128: we'll use a small payload
	// A minimal sequence_header_obu payload (not fully decodeable but
	// structurally valid enough for FindSequenceHeaderOBU to find and return)
	payload := []byte{
		0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00,
	}
	obu := []byte{0x0A, byte(len(payload))}
	obu = append(obu, payload...)
	return obu
}

func TestParseInitSegmentAV1(t *testing.T) {
	data := buildAV1InitSegment(t)

	info, err := parseInitSegment(data)
	if err != nil {
		t.Fatalf("parseInitSegment: %v", err)
	}

	if info.VideoCodec != "av1" {
		t.Errorf("VideoCodec = %q, want av1", info.VideoCodec)
	}
	if info.Width != 1920 {
		t.Errorf("Width = %d, want 1920", info.Width)
	}
	if info.Height != 1080 {
		t.Errorf("Height = %d, want 1080", info.Height)
	}
	if info.VideoTimescale != 90000 {
		t.Errorf("VideoTimescale = %d, want 90000", info.VideoTimescale)
	}
	if info.VideoTrackID == 0 {
		t.Error("VideoTrackID should not be zero")
	}
	if info.SeqHeaderOBU == nil {
		t.Error("SeqHeaderOBU should not be nil")
	}

	// Audio track
	if info.AudioCodec != "mp4a.40.2" {
		t.Errorf("AudioCodec = %q, want mp4a.40.2", info.AudioCodec)
	}
	if info.SampleRate != 48000 {
		t.Errorf("SampleRate = %d, want 48000", info.SampleRate)
	}
	if info.Channels != 2 {
		t.Errorf("Channels = %d, want 2", info.Channels)
	}
	if info.AudioTimescale != 48000 {
		t.Errorf("AudioTimescale = %d, want 48000", info.AudioTimescale)
	}
	if info.AudioTrackID == 0 {
		t.Error("AudioTrackID should not be zero")
	}

	// Trex boxes should be resolved
	if info.videoTrex == nil {
		t.Error("videoTrex should not be nil")
	}
	if info.audioTrex == nil {
		t.Error("audioTrex should not be nil")
	}
}

// buildVideoMediaSegment creates a minimal fMP4 media segment with a single
// video fragment containing 3 samples.
func buildVideoMediaSegment(t *testing.T, trackID uint32) []byte {
	t.Helper()

	seg := mp4.NewMediaSegment()
	frag, err := mp4.CreateFragment(1, trackID)
	if err != nil {
		t.Fatalf("CreateFragment: %v", err)
	}

	// Add 3 samples: first is a keyframe, others are not
	sampleData := []byte{0x01, 0x02, 0x03, 0x04}
	baseDecodeTime := uint64(0)

	// Keyframe sample
	frag.AddFullSample(mp4.FullSample{
		Sample: mp4.Sample{
			Flags:                 mp4.SyncSampleFlags,
			Dur:                   3000, // 1 frame at 30fps in 90kHz
			Size:                  uint32(len(sampleData)),
			CompositionTimeOffset: 0,
		},
		DecodeTime: baseDecodeTime,
		Data:       sampleData,
	})

	// Non-keyframe sample
	frag.AddFullSample(mp4.FullSample{
		Sample: mp4.Sample{
			Flags:                 mp4.NonSyncSampleFlags,
			Dur:                   3000,
			Size:                  uint32(len(sampleData)),
			CompositionTimeOffset: 3000, // PTS > DTS
		},
		DecodeTime: baseDecodeTime + 3000,
		Data:       sampleData,
	})

	// Another non-keyframe sample
	frag.AddFullSample(mp4.FullSample{
		Sample: mp4.Sample{
			Flags:                 mp4.NonSyncSampleFlags,
			Dur:                   3000,
			Size:                  uint32(len(sampleData)),
			CompositionTimeOffset: 0,
		},
		DecodeTime: baseDecodeTime + 6000,
		Data:       sampleData,
	})

	seg.AddFragment(frag)

	var buf bytes.Buffer
	err = seg.Encode(&buf)
	if err != nil {
		t.Fatalf("encode media segment: %v", err)
	}
	return buf.Bytes()
}

func TestParseMediaSegmentVideo(t *testing.T) {
	// First build an init segment to get metadata
	initData := buildAV1InitSegment(t)
	info, err := parseInitSegment(initData)
	if err != nil {
		t.Fatalf("parseInitSegment: %v", err)
	}

	// Build a video media segment
	segData := buildVideoMediaSegment(t, info.VideoTrackID)

	samples, err := parseMediaSegment(segData, info, true)
	if err != nil {
		t.Fatalf("parseMediaSegment: %v", err)
	}

	if len(samples) != 3 {
		t.Fatalf("sample count = %d, want 3", len(samples))
	}

	// First sample: keyframe at DTS=0
	if !samples[0].IsKeyframe {
		t.Error("sample[0] should be a keyframe")
	}
	if samples[0].DTS != 0 {
		t.Errorf("sample[0].DTS = %d, want 0", samples[0].DTS)
	}
	if samples[0].PTS != 0 {
		t.Errorf("sample[0].PTS = %d, want 0", samples[0].PTS)
	}
	if len(samples[0].Data) != 4 {
		t.Errorf("sample[0].Data length = %d, want 4", len(samples[0].Data))
	}

	// Second sample: non-keyframe at DTS=3000/90000 * 1e6 = 33333us
	if samples[1].IsKeyframe {
		t.Error("sample[1] should not be a keyframe")
	}
	expectedDTS := scaleToMicroseconds(3000, 90000)
	if samples[1].DTS != expectedDTS {
		t.Errorf("sample[1].DTS = %d, want %d", samples[1].DTS, expectedDTS)
	}
	// PTS = (3000 + 3000) / 90000 * 1e6 = 66666us
	expectedPTS := scaleToMicroseconds(6000, 90000)
	if samples[1].PTS != expectedPTS {
		t.Errorf("sample[1].PTS = %d, want %d", samples[1].PTS, expectedPTS)
	}

	// Third sample: non-keyframe at DTS=6000/90000 * 1e6 = 66666us
	expectedDTS3 := scaleToMicroseconds(6000, 90000)
	if samples[2].DTS != expectedDTS3 {
		t.Errorf("sample[2].DTS = %d, want %d", samples[2].DTS, expectedDTS3)
	}
}

func TestParseMediaSegmentZeroTimescale(t *testing.T) {
	initInfo := &initSegmentInfo{
		VideoTimescale: 0,
		videoTrex:      &mp4.TrexBox{TrackID: 1},
	}
	_, err := parseMediaSegment([]byte{0x00}, initInfo, true)
	if err == nil {
		t.Error("expected error for zero timescale")
	}
}

// buildAudioMediaSegment creates a minimal fMP4 media segment with audio
// samples.
func buildAudioMediaSegment(t *testing.T, trackID uint32) []byte {
	t.Helper()

	seg := mp4.NewMediaSegment()
	frag, err := mp4.CreateFragment(1, trackID)
	if err != nil {
		t.Fatalf("CreateFragment: %v", err)
	}

	// Add 2 audio samples (AAC frames are all sync)
	sampleData := make([]byte, 128)
	for i := range sampleData {
		sampleData[i] = byte(i)
	}

	frag.AddFullSample(mp4.FullSample{
		Sample: mp4.Sample{
			Flags: mp4.SyncSampleFlags,
			Dur:   1024, // typical AAC frame duration at 48kHz
			Size:  uint32(len(sampleData)),
		},
		DecodeTime: 0,
		Data:       sampleData,
	})

	frag.AddFullSample(mp4.FullSample{
		Sample: mp4.Sample{
			Flags: mp4.SyncSampleFlags,
			Dur:   1024,
			Size:  uint32(len(sampleData)),
		},
		DecodeTime: 1024,
		Data:       sampleData,
	})

	seg.AddFragment(frag)

	var buf bytes.Buffer
	err = seg.Encode(&buf)
	if err != nil {
		t.Fatalf("encode media segment: %v", err)
	}
	return buf.Bytes()
}

func TestParseMediaSegmentAudio(t *testing.T) {
	initData := buildAV1InitSegment(t)
	info, err := parseInitSegment(initData)
	if err != nil {
		t.Fatalf("parseInitSegment: %v", err)
	}

	segData := buildAudioMediaSegment(t, info.AudioTrackID)

	samples, err := parseMediaSegment(segData, info, false)
	if err != nil {
		t.Fatalf("parseMediaSegment: %v", err)
	}

	if len(samples) != 2 {
		t.Fatalf("sample count = %d, want 2", len(samples))
	}

	// First audio sample: DTS=0
	if samples[0].DTS != 0 {
		t.Errorf("sample[0].DTS = %d, want 0", samples[0].DTS)
	}

	// Second audio sample: DTS = 1024/48000 * 1e6 = 21333us
	expectedDTS := scaleToMicroseconds(1024, 48000)
	if samples[1].DTS != expectedDTS {
		t.Errorf("sample[1].DTS = %d, want %d", samples[1].DTS, expectedDTS)
	}
}
