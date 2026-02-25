package mpegts

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
)

// buildTSPacket constructs a 188-byte TS packet with the given fields.
func buildTSPacket(pid uint16, cc uint8, pusi bool, payload []byte) []byte {
	return makePacket(pid, cc, pusi, payload)
}

// buildPATPayload creates a PAT payload with pointer field for embedding in TS.
func buildPATPayload(tsID uint16, programs []struct{ num, pid uint16 }) []byte {
	section := buildPAT(tsID, programs)
	payload := make([]byte, 1+len(section))
	payload[0] = 0x00 // pointer field
	copy(payload[1:], section)
	return payload
}

// buildPMTPayload creates a PMT payload with pointer field for embedding in TS.
func buildPMTPayload(programNum uint16, pcrPID uint16, streams []struct {
	streamType uint8
	pid        uint16
}) []byte {
	section := buildPMT(programNum, pcrPID, streams)
	payload := make([]byte, 1+len(section))
	payload[0] = 0x00
	copy(payload[1:], section)
	return payload
}

// buildPESPayload creates PES data for embedding in TS packets.
func buildPESPayload(streamID byte, pts int64, hasPTS bool, data []byte) []byte {
	return buildPESPacket(streamID, pts, 0, hasPTS, false, data)
}

func TestDemuxer_Synthetic(t *testing.T) {
	t.Parallel()
	// Build a synthetic TS stream: PAT → PMT → video PES → audio PES
	var stream bytes.Buffer

	// PAT packet (PID=0, CC=0, PUSI=true)
	patPayload := buildPATPayload(1, []struct{ num, pid uint16 }{{1, 0x1000}})
	stream.Write(buildTSPacket(0x0000, 0, true, patPayload))

	// PMT packet (PID=0x1000, CC=0, PUSI=true)
	pmtPayload := buildPMTPayload(1, 0x100, []struct {
		streamType uint8
		pid        uint16
	}{
		{0x1B, 0x100}, // H.264 video
		{0x0F, 0x101}, // AAC audio
	})
	stream.Write(buildTSPacket(0x1000, 0, true, pmtPayload))

	// Video PES packet (PID=0x100, CC=0, PUSI=true)
	videoData := []byte{0x00, 0x00, 0x00, 0x01, 0x65} // fake IDR NALU
	videoPES := buildPESPayload(0xE0, 90000, true, videoData)
	stream.Write(buildTSPacket(0x100, 0, true, videoPES))

	// Audio PES packet (PID=0x101, CC=0, PUSI=true)
	audioData := []byte{0xFF, 0xF1, 0x50, 0x40} // fake ADTS header
	audioPES := buildPESPayload(0xC0, 90000, true, audioData)
	stream.Write(buildTSPacket(0x101, 0, true, audioPES))

	// Another video PES to trigger flush of the first
	videoPES2 := buildPESPayload(0xE0, 93754, true, videoData)
	stream.Write(buildTSPacket(0x100, 1, true, videoPES2))

	// Another audio PES to trigger flush of the first
	audioPES2 := buildPESPayload(0xC0, 97680, true, audioData)
	stream.Write(buildTSPacket(0x101, 1, true, audioPES2))

	ctx := context.Background()
	dmx := NewDemuxer(ctx, &stream, DemuxerOptPacketSize(188))

	var gotPAT, gotPMT bool
	var videoPTS, audioPTS []int64

	for {
		data, err := dmx.NextData()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}

		if data.PAT != nil {
			gotPAT = true
			if len(data.PAT.Programs) != 1 {
				t.Errorf("PAT programs = %d, want 1", len(data.PAT.Programs))
			}
		}
		if data.PMT != nil {
			gotPMT = true
			if len(data.PMT.ElementaryStreams) != 2 {
				t.Errorf("PMT streams = %d, want 2", len(data.PMT.ElementaryStreams))
			}
		}
		if data.PES != nil {
			if data.PES.Header != nil && data.PES.Header.OptionalHeader != nil && data.PES.Header.OptionalHeader.PTS != nil {
				pid := data.FirstPacket.Header.PID
				if pid == 0x100 {
					videoPTS = append(videoPTS, data.PES.Header.OptionalHeader.PTS.Base)
				} else if pid == 0x101 {
					audioPTS = append(audioPTS, data.PES.Header.OptionalHeader.PTS.Base)
				}
			}
		}
	}

	if !gotPAT {
		t.Error("did not receive PAT")
	}
	if !gotPMT {
		t.Error("did not receive PMT")
	}
	if len(videoPTS) < 1 {
		t.Error("did not receive any video PES")
	} else if videoPTS[0] != 90000 {
		t.Errorf("first video PTS = %d, want 90000", videoPTS[0])
	}
	if len(audioPTS) < 1 {
		t.Error("did not receive any audio PES")
	} else if audioPTS[0] != 90000 {
		t.Errorf("first audio PTS = %d, want 90000", audioPTS[0])
	}
}

func TestDemuxer_PacketsParser(t *testing.T) {
	t.Parallel()
	var stream bytes.Buffer

	// PAT
	patPayload := buildPATPayload(1, []struct{ num, pid uint16 }{{1, 0x1000}})
	stream.Write(buildTSPacket(0x0000, 0, true, patPayload))

	// PMT
	pmtPayload := buildPMTPayload(1, 0x100, []struct {
		streamType uint8
		pid        uint16
	}{{0x1B, 0x100}})
	stream.Write(buildTSPacket(0x1000, 0, true, pmtPayload))

	// Custom PID=500 (SCTE-35 like)
	customData := []byte{0xFC, 0x30, 0x11} // fake SCTE-35 header
	stream.Write(buildTSPacket(500, 0, true, customData))
	stream.Write(buildTSPacket(500, 1, true, customData)) // trigger flush

	parserCalled := false
	parser := func(ps []*Packet) ([]*DemuxerData, bool, error) {
		if ps[0].Header.PID == 500 {
			parserCalled = true
			return nil, true, nil // skip standard parsing
		}
		return nil, false, nil
	}

	ctx := context.Background()
	dmx := NewDemuxer(ctx, &stream, DemuxerOptPacketsParser(parser))

	for {
		_, err := dmx.NextData()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	if !parserCalled {
		t.Error("packets parser was not called")
	}
}

func TestDemuxer_EOF(t *testing.T) {
	t.Parallel()
	stream := bytes.NewReader([]byte{})
	ctx := context.Background()
	dmx := NewDemuxer(ctx, stream)

	_, err := dmx.NextData()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestDemuxer_ContextCancellation(t *testing.T) {
	t.Parallel()
	// Create a reader that never returns
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dmx := NewDemuxer(ctx, bytes.NewReader(make([]byte, 1000)))

	_, err := dmx.NextData()
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDemuxer_CorruptPacketSkipped(t *testing.T) {
	t.Parallel()
	var stream bytes.Buffer

	// Good PAT
	patPayload := buildPATPayload(1, []struct{ num, pid uint16 }{{1, 0x1000}})
	stream.Write(buildTSPacket(0x0000, 0, true, patPayload))

	// Corrupt packet (bad sync byte)
	corrupt := make([]byte, 188)
	corrupt[0] = 0x00
	stream.Write(corrupt)

	// Good PAT again (cc=1)
	stream.Write(buildTSPacket(0x0000, 1, true, patPayload))

	ctx := context.Background()
	dmx := NewDemuxer(ctx, &stream)

	gotPAT := 0
	for {
		data, err := dmx.NextData()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if data.PAT != nil {
			gotPAT++
		}
	}

	if gotPAT == 0 {
		t.Error("should have parsed at least one PAT despite corrupt packet")
	}
}

// TestDemuxer_GoldenVectors parses a real TS file and verifies PMT streams
// and PTS values against known-good values.
func TestDemuxer_GoldenVectors(t *testing.T) {
	t.Parallel()
	f, err := os.Open("../../test/harness/BigBuckBunny_256x144-24fps.ts")
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}
	defer f.Close()

	ctx := context.Background()
	dmx := NewDemuxer(ctx, f, DemuxerOptPacketSize(188))

	type goldenVideo struct {
		dataLen int
		pts     int64
		dts     int64
		hasDTS  bool
	}

	type goldenAudio struct {
		dataLen int
		pts     int64
	}

	expectedVideo := []goldenVideo{
		{1302, 133500, 126000, true},
		{118, 148500, 129750, true},
		{116, 141000, 133500, true},
		{116, 137250, 0, false},
		{3739, 144750, 141000, true},
		{26077, 163500, 144750, true},
		{26078, 156000, 148500, true},
		{26078, 152250, 0, false},
		{26077, 159750, 156000, true},
		{26078, 178500, 159750, true},
	}

	expectedAudio := []goldenAudio{
		{2847, 131580},
		{2725, 148860},
		{2763, 164220},
		{2810, 179580},
		{2790, 194940},
		{2774, 210300},
		{2800, 225660},
		{2773, 241020},
		{2766, 256380},
		{2808, 271740},
	}

	var videoPID, audioPID uint16
	var videoResults []goldenVideo
	var audioResults []goldenAudio
	pmtSeen := false

	for {
		data, err := dmx.NextData()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("NextData: %v", err)
		}

		if data.PMT != nil && !pmtSeen {
			pmtSeen = true
			// Verify PMT structure
			if len(data.PMT.ElementaryStreams) < 2 {
				t.Fatalf("PMT has %d streams, expected at least 2", len(data.PMT.ElementaryStreams))
			}
			for _, es := range data.PMT.ElementaryStreams {
				if es.StreamType == 0x1B && videoPID == 0 {
					videoPID = es.ElementaryPID
				}
				if es.StreamType == 0x0F && audioPID == 0 {
					audioPID = es.ElementaryPID
				}
			}
			if videoPID != 256 {
				t.Errorf("video PID = %d, want 256", videoPID)
			}
			if audioPID != 257 {
				t.Errorf("audio PID = %d, want 257", audioPID)
			}
			continue
		}

		if data.PES == nil {
			continue
		}

		pid := data.FirstPacket.Header.PID
		oh := data.PES.Header.OptionalHeader

		if pid == videoPID && len(videoResults) < len(expectedVideo) {
			gv := goldenVideo{dataLen: len(data.PES.Data)}
			if oh != nil && oh.PTS != nil {
				gv.pts = oh.PTS.Base
			}
			if oh != nil && oh.DTS != nil {
				gv.dts = oh.DTS.Base
				gv.hasDTS = true
			}
			videoResults = append(videoResults, gv)
		}

		if pid == audioPID && len(audioResults) < len(expectedAudio) {
			ga := goldenAudio{dataLen: len(data.PES.Data)}
			if oh != nil && oh.PTS != nil {
				ga.pts = oh.PTS.Base
			}
			audioResults = append(audioResults, ga)
		}

		if len(videoResults) >= len(expectedVideo) && len(audioResults) >= len(expectedAudio) {
			break
		}
	}

	if !pmtSeen {
		t.Fatal("PMT not found")
	}

	// Compare video
	for i, ev := range expectedVideo {
		if i >= len(videoResults) {
			t.Errorf("missing video result %d", i)
			continue
		}
		gv := videoResults[i]
		if gv.dataLen != ev.dataLen {
			t.Errorf("video[%d] dataLen = %d, want %d", i, gv.dataLen, ev.dataLen)
		}
		if gv.pts != ev.pts {
			t.Errorf("video[%d] PTS = %d, want %d", i, gv.pts, ev.pts)
		}
		if gv.hasDTS != ev.hasDTS {
			t.Errorf("video[%d] hasDTS = %v, want %v", i, gv.hasDTS, ev.hasDTS)
		}
		if gv.hasDTS && gv.dts != ev.dts {
			t.Errorf("video[%d] DTS = %d, want %d", i, gv.dts, ev.dts)
		}
	}

	// Compare audio
	for i, ea := range expectedAudio {
		if i >= len(audioResults) {
			t.Errorf("missing audio result %d", i)
			continue
		}
		ga := audioResults[i]
		if ga.dataLen != ea.dataLen {
			t.Errorf("audio[%d] dataLen = %d, want %d", i, ga.dataLen, ea.dataLen)
		}
		if ga.pts != ea.pts {
			t.Errorf("audio[%d] PTS = %d, want %d", i, ga.pts, ea.pts)
		}
	}
}

// Suppress unused import warning for binary package.
var _ = binary.BigEndian
