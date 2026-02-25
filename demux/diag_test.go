package demux

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/mpegts"
)

func TestDiag_DecodedCaptions(t *testing.T) {
	t.Parallel()

	f, err := os.Open("../../test/harness/BigBuckBunny_256x144-24fps.ts")
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}
	defer f.Close()

	ctx := context.Background()
	dmx := mpegts.NewDemuxer(ctx, f, mpegts.DemuxerOptPacketSize(188))

	var videoPID uint16
	totalFrames := 0
	decs := map[int]*ccx.CEA608Decoder{
		1: ccx.NewCEA608Decoder(),
		2: ccx.NewCEA608Decoder(),
		3: ccx.NewCEA608Decoder(),
		4: ccx.NewCEA608Decoder(),
	}

	for totalFrames < 100 {
		data, err := dmx.NextData()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("NextData: %v", err)
		}

		if data.PMT != nil {
			for _, es := range data.PMT.ElementaryStreams {
				if es.StreamType == 0x1B {
					videoPID = es.ElementaryPID
				}
			}
			continue
		}

		if data.PES == nil || data.FirstPacket.Header.PID != videoPID {
			continue
		}

		totalFrames++
		nalus := ParseAnnexB(data.PES.Data)
		for _, nalu := range nalus {
			if nalu.Type != NALTypeSEI {
				continue
			}
			cd := ccx.ExtractCaptions(nalu.Data)
			if cd == nil {
				continue
			}

			for _, pair := range cd.CC608Pairs {
				cc1, cc2 := pair.Data[0], pair.Data[1]
				isCtrl := cc1 >= 0x10 && cc1 <= 0x1F
				t.Logf("Frame %3d CC%d f%d: %02x %02x ctrl=%v", totalFrames, pair.Channel, pair.Field, cc1, cc2, isCtrl)
				dec := decs[pair.Channel]
				if dec == nil {
					continue
				}
				text := dec.Decode(cc1, cc2)
				if text != "" {
					t.Logf("  → CH%d text: %q", pair.Channel, text)
				}
			}
		}
	}

	if totalFrames == 0 {
		t.Error("expected totalFrames > 0")
	}
}

func TestDiag_DTVCC(t *testing.T) {
	t.Parallel()

	f, err := os.Open("../../test/harness/BigBuckBunny_256x144-24fps.ts")
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}
	defer f.Close()

	ctx := context.Background()
	dmx := mpegts.NewDemuxer(ctx, f, mpegts.DemuxerOptPacketSize(188))

	var videoPID uint16
	totalFrames := 0

	dec := ccx.NewCEA708Decoder()

	for totalFrames < 100 {
		data, err := dmx.NextData()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("NextData: %v", err)
		}

		if data.PMT != nil {
			for _, es := range data.PMT.ElementaryStreams {
				if es.StreamType == 0x1B {
					videoPID = es.ElementaryPID
				}
			}
			continue
		}

		if data.PES == nil || data.FirstPacket.Header.PID != videoPID {
			continue
		}

		totalFrames++
		nalus := ParseAnnexB(data.PES.Data)
		for _, nalu := range nalus {
			if nalu.Type != NALTypeSEI {
				continue
			}
			cd := ccx.ExtractCaptions(nalu.Data)
			if cd == nil {
				continue
			}

			for _, pair := range cd.DTVCC {
				text := dec.AddTriplet(pair)
				if text != "" {
					t.Logf("Frame %3d: svc1: %q", totalFrames, text)
				}
			}
		}
	}

	if totalFrames == 0 {
		t.Error("expected totalFrames > 0")
	}
}
