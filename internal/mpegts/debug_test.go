package mpegts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestDebugSynthetic(t *testing.T) {
	t.Parallel()

	var stream bytes.Buffer

	// PAT
	patPayload := buildPATPayload(1, []struct{ num, pid uint16 }{{1, 0x1000}})
	stream.Write(buildTSPacket(0x0000, 0, true, patPayload))

	// PMT
	pmtPayload := buildPMTPayload(1, 0x100, []struct {
		streamType uint8
		pid        uint16
	}{
		{0x1B, 0x100},
		{0x0F, 0x101},
	})
	stream.Write(buildTSPacket(0x1000, 0, true, pmtPayload))

	// Video + trigger
	videoData := []byte{0x00, 0x00, 0x00, 0x01, 0x65}
	videoPES := buildPESPayload(0xE0, 90000, true, videoData)
	stream.Write(buildTSPacket(0x100, 0, true, videoPES))
	stream.Write(buildTSPacket(0x100, 1, true, buildPESPayload(0xE0, 93754, true, videoData)))

	ctx := context.Background()
	dmx := NewDemuxer(ctx, &stream, DemuxerOptPacketSize(188))

	var parsedPAT, parsedPMT bool
	for i := 0; i < 10; i++ {
		data, err := dmx.NextData()
		if errors.Is(err, io.EOF) {
			t.Logf("EOF at iteration %d", i)
			break
		}
		if err != nil {
			t.Fatalf("Error at iteration %d: %v", i, err)
		}
		if data.PAT != nil {
			parsedPAT = true
		}
		if data.PMT != nil {
			parsedPMT = true
		}
		t.Logf("iter %d: PAT=%v PMT=%v PES=%v firstPID=%d",
			i, data.PAT != nil, data.PMT != nil, data.PES != nil,
			data.FirstPacket.Header.PID)
	}

	if !parsedPAT {
		t.Error("expected at least one PAT to be parsed")
	}
	if !parsedPMT {
		t.Error("expected at least one PMT to be parsed")
	}
}
