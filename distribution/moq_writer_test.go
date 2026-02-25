package distribution

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/quic-go/quic-go/quicvarint"
	"github.com/zsiec/prism/media"
)

func TestMoQWriterSubgroupHeader(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(5, 128)
	var buf bytes.Buffer
	if err := w.WriteStreamHeader(&buf, TrackIDVideo, 42, 1000); err != nil {
		t.Fatalf("WriteStreamHeader failed: %v", err)
	}

	data := buf.Bytes()
	pos := 0

	// Stream type
	streamType, n, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse stream type: %v", err)
	}
	if streamType != moqStreamTypeSubgroupSIDExt {
		t.Errorf("stream type: got 0x%x, want 0x%x", streamType, moqStreamTypeSubgroupSIDExt)
	}
	pos += n

	// Track alias
	trackAlias, n, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse track alias: %v", err)
	}
	if trackAlias != 5 {
		t.Errorf("track alias: got %d, want 5", trackAlias)
	}
	pos += n

	// Group ID
	groupID, n, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse group ID: %v", err)
	}
	if groupID != 42 {
		t.Errorf("group ID: got %d, want 42", groupID)
	}
	pos += n

	// Subgroup ID
	subgroupID, n, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse subgroup ID: %v", err)
	}
	if subgroupID != 0 {
		t.Errorf("subgroup ID: got %d, want 0", subgroupID)
	}
	pos += n

	// Publisher priority (1 raw byte)
	if data[pos] != 128 {
		t.Errorf("publisher priority: got %d, want 128", data[pos])
	}
	pos++

	if pos != len(data) {
		t.Errorf("unexpected trailing bytes: consumed %d of %d", pos, len(data))
	}
}

func TestMoQWriterVideoFrameKeyframe(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(1, 0)
	var buf bytes.Buffer

	// Must call WriteStreamHeader first to reset objectID
	if err := w.WriteStreamHeader(&buf, TrackIDVideo, 1, 0); err != nil {
		t.Fatalf("WriteStreamHeader failed: %v", err)
	}
	buf.Reset()

	frame := &media.VideoFrame{
		PTS:        33000, // 33ms in µs
		IsKeyframe: true,
		NALUs:      [][]byte{{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xE0, 0x1E}},
		SPS:        []byte{0x67, 0x42, 0xE0, 0x1E},
		PPS:        []byte{0x68, 0xCE},
		Codec:      "h264",
	}

	n, err := w.WriteVideoFrame(&buf, frame)
	if err != nil {
		t.Fatalf("WriteVideoFrame failed: %v", err)
	}

	if n != int64(buf.Len()) {
		t.Errorf("bytes written: got %d, actual buffer %d", n, buf.Len())
	}

	data := buf.Bytes()
	pos := 0

	// Object ID should be 0 (first object after WriteStreamHeader)
	objectID, nn, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse object ID: %v", err)
	}
	if objectID != 0 {
		t.Errorf("object ID: got %d, want 0", objectID)
	}
	pos += nn

	// Extension headers length
	extLen, nn, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse ext length: %v", err)
	}
	pos += nn

	// Parse extensions
	extEnd := pos + int(extLen)
	foundTimestamp := false
	foundMarking := false
	foundConfig := false

	for pos < extEnd {
		extID, nn, err := quicvarint.Parse(data[pos:])
		if err != nil {
			t.Fatalf("parse ext ID: %v", err)
		}
		pos += nn

		if extID%2 == 0 {
			// Even ID: varint value
			val, nn, err := quicvarint.Parse(data[pos:])
			if err != nil {
				t.Fatalf("parse ext value: %v", err)
			}
			pos += nn

			switch extID {
			case locExtCaptureTimestamp:
				foundTimestamp = true
				if val != 33000 {
					t.Errorf("capture timestamp: got %d, want 33000", val)
				}
			case locExtVideoFrameMarking:
				foundMarking = true
				if val != vfmKeyframe {
					t.Errorf("video frame marking: got 0x%x, want 0x%x", val, vfmKeyframe)
				}
			}
		} else {
			// Odd ID: length-prefixed bytes
			valLen, nn, err := quicvarint.Parse(data[pos:])
			if err != nil {
				t.Fatalf("parse ext value length: %v", err)
			}
			pos += nn

			if extID == locExtVideoConfig {
				foundConfig = true
				configData := data[pos : pos+int(valLen)]
				// Verify it's a valid AVCDecoderConfigurationRecord
				if configData[0] != 1 {
					t.Errorf("AVC config version: got %d, want 1", configData[0])
				}
			}
			pos += int(valLen)
		}
	}

	if !foundTimestamp {
		t.Error("missing capture timestamp extension")
	}
	if !foundMarking {
		t.Error("missing video frame marking extension")
	}
	if !foundConfig {
		t.Error("missing video config extension")
	}

	// Payload length
	payloadLen, nn, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse payload length: %v", err)
	}
	pos += nn

	// Payload should be AVC1 format (4-byte length prefix + NALU data without start code)
	payload := data[pos : pos+int(payloadLen)]
	naluLen := binary.BigEndian.Uint32(payload[0:4])
	if naluLen != 4 { // 0x67 0x42 0xE0 0x1E
		t.Errorf("AVC1 NALU length: got %d, want 4", naluLen)
	}
}

func TestMoQWriterVideoFrameDelta(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(1, 0)
	var buf bytes.Buffer

	if err := w.WriteStreamHeader(&buf, TrackIDVideo, 1, 0); err != nil {
		t.Fatalf("WriteStreamHeader failed: %v", err)
	}
	buf.Reset()

	// Write keyframe first (objectID=0)
	keyframe := &media.VideoFrame{
		PTS:        0,
		IsKeyframe: true,
		NALUs:      [][]byte{{0x00, 0x00, 0x00, 0x01, 0x65, 0x00}},
		SPS:        []byte{0x67, 0x42, 0xE0, 0x1E},
		PPS:        []byte{0x68, 0xCE},
		Codec:      "h264",
	}
	if _, err := w.WriteVideoFrame(&buf, keyframe); err != nil {
		t.Fatalf("write keyframe: %v", err)
	}
	buf.Reset()

	// Write delta frame (objectID=1)
	delta := &media.VideoFrame{
		PTS:        33000,
		IsKeyframe: false,
		NALUs:      [][]byte{{0x00, 0x00, 0x00, 0x01, 0x41, 0x9A}},
		Codec:      "h264",
	}
	if _, err := w.WriteVideoFrame(&buf, delta); err != nil {
		t.Fatalf("write delta: %v", err)
	}

	data := buf.Bytes()

	// Object ID should be 1
	objectID, nn, err := quicvarint.Parse(data)
	if err != nil {
		t.Fatalf("parse object ID: %v", err)
	}
	if objectID != 1 {
		t.Errorf("object ID: got %d, want 1", objectID)
	}
	pos := nn

	// Parse extensions — should NOT have video config
	extLen, nn, err := quicvarint.Parse(data[pos:])
	if err != nil {
		t.Fatalf("parse ext length: %v", err)
	}
	pos += nn

	extEnd := pos + int(extLen)
	for pos < extEnd {
		extID, nn, err := quicvarint.Parse(data[pos:])
		if err != nil {
			break
		}
		pos += nn

		if extID == locExtVideoConfig {
			t.Error("delta frame should not have video config extension")
		}

		if extID%2 == 0 {
			val, nn, _ := quicvarint.Parse(data[pos:])
			pos += nn
			if extID == locExtVideoFrameMarking && val != vfmNonKeyframe {
				t.Errorf("delta frame marking: got 0x%x, want 0x%x", val, vfmNonKeyframe)
			}
		} else {
			valLen, nn, _ := quicvarint.Parse(data[pos:])
			pos += nn
			pos += int(valLen)
		}
	}
}

func TestMoQWriterAudioFrame(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(2, 64)
	var buf bytes.Buffer

	if err := w.WriteStreamHeader(&buf, AudioTrackID(0), 0, 0); err != nil {
		t.Fatalf("WriteStreamHeader failed: %v", err)
	}
	buf.Reset()

	// ADTS frame: 7-byte header + 4 bytes payload
	adts := []byte{0xFF, 0xF1, 0x50, 0x80, 0x02, 0x00, 0xFC, 0xDE, 0xAD, 0xBE, 0xEF}

	n, err := w.WriteAudioFrame(&buf, adts, 5000)
	if err != nil {
		t.Fatalf("WriteAudioFrame failed: %v", err)
	}

	if n != int64(buf.Len()) {
		t.Errorf("bytes written: got %d, actual buffer %d", n, buf.Len())
	}

	data := buf.Bytes()
	pos := 0

	// Object ID
	objectID, nn, _ := quicvarint.Parse(data[pos:])
	if objectID != 0 {
		t.Errorf("object ID: got %d, want 0", objectID)
	}
	pos += nn

	// Extension headers length
	extLen, nn, _ := quicvarint.Parse(data[pos:])
	pos += nn

	// Parse timestamp extension
	extEnd := pos + int(extLen)
	for pos < extEnd {
		extID, nn, _ := quicvarint.Parse(data[pos:])
		pos += nn

		if extID == locExtCaptureTimestamp {
			val, nn, _ := quicvarint.Parse(data[pos:])
			pos += nn
			if val != 5_000_000 { // 5000ms * 1000 = 5000000µs
				t.Errorf("capture timestamp: got %d, want 5000000", val)
			}
		} else if extID%2 == 0 {
			_, nn, _ := quicvarint.Parse(data[pos:])
			pos += nn
		} else {
			valLen, nn, _ := quicvarint.Parse(data[pos:])
			pos += nn
			pos += int(valLen)
		}
	}

	// Payload length
	payloadLen, nn, _ := quicvarint.Parse(data[pos:])
	pos += nn

	// Payload should be raw AAC (ADTS stripped: 4 bytes)
	if payloadLen != 4 {
		t.Errorf("payload length: got %d, want 4", payloadLen)
	}

	payload := data[pos : pos+int(payloadLen)]
	if !bytes.Equal(payload, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("payload mismatch: got %x", payload)
	}
}

func TestMoQWriterCaptionFrame(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(10, 200)
	var buf bytes.Buffer

	if err := w.WriteStreamHeader(&buf, TrackIDCaptions, 0, 0); err != nil {
		t.Fatalf("WriteStreamHeader failed: %v", err)
	}
	buf.Reset()

	captionData := []byte{0xCC, 0x02, 0x01, 0x02, 0x03}

	n, err := w.WriteCaptionFrame(&buf, captionData, 3000)
	if err != nil {
		t.Fatalf("WriteCaptionFrame failed: %v", err)
	}

	if n != int64(buf.Len()) {
		t.Errorf("bytes written: got %d, actual buffer %d", n, buf.Len())
	}

	data := buf.Bytes()
	pos := 0

	// Object ID
	objectID, nn, _ := quicvarint.Parse(data[pos:])
	if objectID != 0 {
		t.Errorf("object ID: got %d, want 0", objectID)
	}
	pos += nn

	// Skip extensions
	extLen, nn, _ := quicvarint.Parse(data[pos:])
	pos += nn
	pos += int(extLen)

	// Payload length
	payloadLen, nn, _ := quicvarint.Parse(data[pos:])
	pos += nn

	// Caption data should be passed through unchanged
	if payloadLen != uint64(len(captionData)) {
		t.Errorf("payload length: got %d, want %d", payloadLen, len(captionData))
	}

	payload := data[pos : pos+int(payloadLen)]
	if !bytes.Equal(payload, captionData) {
		t.Errorf("payload mismatch: got %x", payload)
	}
}

func TestMoQWriterObjectIDReset(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(1, 0)
	var buf bytes.Buffer

	// First stream
	if err := w.WriteStreamHeader(&buf, TrackIDVideo, 1, 0); err != nil {
		t.Fatal(err)
	}
	buf.Reset()

	frame := &media.VideoFrame{
		PTS:   0,
		NALUs: [][]byte{{0x00, 0x00, 0x00, 0x01, 0x65}},
		Codec: "h264",
	}

	// Write two frames (objectID 0, 1)
	w.WriteVideoFrame(&buf, frame)
	w.WriteVideoFrame(&buf, frame)
	buf.Reset()

	// New stream header should reset objectID
	if err := w.WriteStreamHeader(&buf, TrackIDVideo, 2, 0); err != nil {
		t.Fatal(err)
	}
	buf.Reset()

	if _, err := w.WriteVideoFrame(&buf, frame); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	objectID, _, _ := quicvarint.Parse(data)
	if objectID != 0 {
		t.Errorf("object ID after reset: got %d, want 0", objectID)
	}
}

func TestMoQWriterBytesWritten(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(1, 0)
	var buf bytes.Buffer

	if err := w.WriteStreamHeader(&buf, TrackIDVideo, 1, 0); err != nil {
		t.Fatal(err)
	}
	buf.Reset()

	frame := &media.VideoFrame{
		PTS:        1000,
		IsKeyframe: true,
		NALUs:      [][]byte{{0x00, 0x00, 0x00, 0x01, 0x65, 0x00, 0x01, 0x02}},
		SPS:        []byte{0x67, 0x42, 0xE0, 0x1E},
		PPS:        []byte{0x68, 0xCE},
		Codec:      "h264",
	}

	n, err := w.WriteVideoFrame(&buf, frame)
	if err != nil {
		t.Fatalf("WriteVideoFrame failed: %v", err)
	}

	if n != int64(buf.Len()) {
		t.Errorf("WriteVideoFrame: returned %d, buffer has %d", n, buf.Len())
	}

	buf.Reset()
	n, err = w.WriteAudioFrame(&buf, []byte{0xFF, 0xF1, 0x50, 0x80, 0x02, 0x00, 0xFC, 0xAA}, 100)
	if err != nil {
		t.Fatalf("WriteAudioFrame failed: %v", err)
	}
	if n != int64(buf.Len()) {
		t.Errorf("WriteAudioFrame: returned %d, buffer has %d", n, buf.Len())
	}

	buf.Reset()
	n, err = w.WriteCaptionFrame(&buf, []byte{0x01, 0x02, 0x03}, 200)
	if err != nil {
		t.Fatalf("WriteCaptionFrame failed: %v", err)
	}
	if n != int64(buf.Len()) {
		t.Errorf("WriteCaptionFrame: returned %d, buffer has %d", n, buf.Len())
	}
}

func TestMoQWriterStreamHeaderSize(t *testing.T) {
	t.Parallel()
	w := NewMoQWriter(1, 0)
	size := w.StreamHeaderSize()
	if size < 4 {
		t.Errorf("header size too small: got %d", size)
	}
	if size > 20 {
		t.Errorf("header size unexpectedly large: got %d", size)
	}
}
