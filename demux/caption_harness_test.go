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

func TestCaptionHarness(t *testing.T) {
	files := []string{
		"../../test/harness/BigBuckBunny_transcoded.ts",
		"../../test/harness/BigBuckBunny_256x144-24fps.ts",
	}

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			runCaptionHarness(t, path, 0)
		})
	}
}

func TestCaptionHarness_Exhaustive(t *testing.T) {
	path := "../../test/harness/BigBuckBunny_transcoded.ts"
	runCaptionHarness(t, path, 3)
}

func runCaptionHarness(t *testing.T, path string, loops int) {
	t.Helper()
	if loops <= 0 {
		loops = 1
	}

	cea608Decs := map[int]*ccx.CEA608Decoder{
		1: ccx.NewCEA608Decoder(),
		2: ccx.NewCEA608Decoder(),
		3: ccx.NewCEA608Decoder(),
		4: ccx.NewCEA608Decoder(),
	}
	cea608Texts := map[int][]string{}

	cea708Svcs := map[int]*ccx.CEA708Service{}
	for i := 1; i <= 6; i++ {
		cea708Svcs[i] = ccx.NewCEA708Service()
	}
	cea708Texts := map[int][]string{}

	totalFrames := 0

	for loop := 0; loop < loops; loop++ {
		f, err := os.Open(path)
		if err != nil {
			t.Skipf("test file not available: %v", err)
		}

		ctx := context.Background()
		dmx := mpegts.NewDemuxer(ctx, f, mpegts.DemuxerOptPacketSize(188))

		var videoPID uint16
		var packetBuf []byte

		processCompletedPacket := func() {
			if len(packetBuf) < 1 {
				return
			}
			packetSize := ccx.DTVCCPacketSize(packetBuf[0])
			if len(packetBuf) < packetSize {
				return
			}
			for _, block := range ccx.ParseDTVCCPacket(packetBuf[:packetSize]) {
				svc, ok := cea708Svcs[block.ServiceNum]
				if ok && svc.ProcessBlock(block.Data) {
					text := svc.DisplayText()
					if text != "" {
						cea708Texts[block.ServiceNum] = append(cea708Texts[block.ServiceNum], text)
					}
				}
			}
		}

		for {
			data, err := dmx.NextData()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				f.Close()
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
					dec := cea608Decs[pair.Channel]
					if dec == nil {
						continue
					}
					text := dec.Decode(pair.Data[0], pair.Data[1])
					if text != "" {
						cea608Texts[pair.Channel] = append(cea608Texts[pair.Channel], text)
					}
				}

				for _, pair := range cd.DTVCC {
					if pair.Start {
						processCompletedPacket()
						packetBuf = packetBuf[:0]
					}
					packetBuf = append(packetBuf, pair.Data[0], pair.Data[1])
				}
			}
		}
		processCompletedPacket()
		f.Close()
	}

	t.Logf("Processed %d video frames across %d loop(s)", totalFrames, loops)

	for ch := 1; ch <= 4; ch++ {
		texts := cea608Texts[ch]
		if len(texts) > 0 {
			t.Logf("=== CEA-608 CC%d (%d updates) ===", ch, len(texts))
			for _, txt := range texts {
				t.Logf("  %q", txt)
			}
		}
	}

	for svc := 1; svc <= 6; svc++ {
		texts := cea708Texts[svc]
		if len(texts) > 0 {
			t.Logf("=== CEA-708 Service %d (%d updates) ===", svc, len(texts))
			for _, txt := range texts {
				t.Logf("  %q", txt)
			}
		}
	}

	for ch := 1; ch <= 4; ch++ {
		dec := cea608Decs[ch]
		if len(cea608Texts[ch]) > 0 {
			regions := dec.StyledRegions()
			t.Logf("CEA-608 CC%d: %d styled regions", ch, len(regions))
		}
	}
	for svcNum := 1; svcNum <= 6; svcNum++ {
		svc := cea708Svcs[svcNum]
		if len(cea708Texts[svcNum]) > 0 {
			regions := svc.StyledRegions()
			t.Logf("CEA-708 Svc%d: %d styled regions", svcNum, len(regions))
		}
	}
}
