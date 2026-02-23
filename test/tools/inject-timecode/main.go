package main

import (
	"fmt"
	"os"

	"github.com/zsiec/prism/test/tools/tsutil"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: inject-timecode <input.ts> <output.ts>\n")
		fmt.Fprintf(os.Stderr, "Injects pic_timing SEI with clock_timestamp into H.264 video frames.\n")
		fmt.Fprintf(os.Stderr, "Input must be encoded with x264 nal-hrd + pic-struct flags.\n")
		os.Exit(1)
	}

	inData, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}

	vp := extractVUIParams(inData)
	fmt.Fprintf(os.Stderr, "VUI: cpb_removal_delay_len=%d dpb_output_delay_len=%d time_offset_len=%d pic_struct_present=%v\n",
		vp.cpbRemovalDelayLen, vp.dpbOutputDelayLen, vp.timeOffsetLen, vp.picStructPresent)

	if !vp.picStructPresent {
		fmt.Fprintf(os.Stderr, "error: SPS VUI pic_struct_present_flag must be 1\n")
		fmt.Fprintf(os.Stderr, "Encode with: -x264-params nal-hrd=cbr:vbv-bufsize=3000:vbv-maxrate=3000:pic-struct=1\n")
		os.Exit(1)
	}

	outData := rewriteTimecodes(inData, vp)

	if err := os.WriteFile(os.Args[2], outData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Wrote %d bytes to %s\n", len(outData), os.Args[2])
}

type vuiParams struct {
	cpbRemovalDelayLen int
	dpbOutputDelayLen  int
	timeOffsetLen      int
	picStructPresent   bool
	hrdPresent         bool
	videoPID           uint16
}

type bitReader struct {
	data []byte
	pos  int
	bit  int
}

func (br *bitReader) readBit() int {
	if br.pos >= len(br.data) {
		return 0
	}
	val := int((br.data[br.pos] >> (7 - br.bit)) & 1)
	br.bit++
	if br.bit == 8 {
		br.bit = 0
		br.pos++
	}
	return val
}

func (br *bitReader) readBits(n int) int {
	val := 0
	for i := 0; i < n; i++ {
		val = (val << 1) | br.readBit()
	}
	return val
}

func (br *bitReader) readUE() int {
	zeros := 0
	for br.readBit() == 0 {
		zeros++
		if zeros > 31 {
			return 0
		}
	}
	if zeros == 0 {
		return 0
	}
	return (1 << zeros) - 1 + br.readBits(zeros)
}

type bitWriter struct {
	buf []byte
	bit int
}

func (bw *bitWriter) writeBit(val int) {
	byteIdx := len(bw.buf) - 1
	if bw.bit == 0 {
		bw.buf = append(bw.buf, 0)
		byteIdx++
	}
	if val != 0 {
		bw.buf[byteIdx] |= byte(1 << (7 - bw.bit))
	}
	bw.bit = (bw.bit + 1) % 8
}

func (bw *bitWriter) writeBits(val, n int) {
	for i := n - 1; i >= 0; i-- {
		bw.writeBit((val >> i) & 1)
	}
}

func (bw *bitWriter) bytes() []byte {
	return bw.buf
}

func extractVUIParams(tsData []byte) vuiParams {
	var vp vuiParams
	vp.cpbRemovalDelayLen = 24
	vp.dpbOutputDelayLen = 24

	for off := 0; off+tsutil.TSPacketSize <= len(tsData); off += tsutil.TSPacketSize {
		pkt := tsData[off : off+tsutil.TSPacketSize]
		if pkt[0] != 0x47 {
			continue
		}

		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
		if pid == 0 {
			continue
		}

		payloadStart := pkt[1]&0x40 != 0
		if !payloadStart {
			continue
		}

		headerLen := 4
		if pkt[3]&0x20 != 0 {
			adaptLen := int(pkt[4])
			headerLen = 5 + adaptLen
		}
		if headerLen >= tsutil.TSPacketSize {
			continue
		}

		payload := pkt[headerLen:]
		if len(payload) < 9 {
			continue
		}

		if payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
			continue
		}

		streamID := payload[3]
		if streamID < 0xE0 || streamID > 0xEF {
			continue
		}

		vp.videoPID = pid

		pesHeaderLen := int(payload[8])
		esStart := 9 + pesHeaderLen
		if esStart >= len(payload) {
			continue
		}
		es := payload[esStart:]

		nalus := tsutil.FindNALStarts(es)
		for _, ns := range nalus {
			if ns >= len(es) {
				continue
			}
			nalType := es[ns] & 0x1F
			if nalType != 7 {
				continue
			}

			end := len(es)
			for _, ns2 := range nalus {
				if ns2 > ns {
					for end > ns2 && es[end-1] == 0 {
						end--
					}
					end = ns2
					break
				}
			}

			spsData := es[ns:end]
			if len(spsData) < 4 {
				continue
			}

			vp = parseSPSForVUI(spsData)
			vp.videoPID = pid
			return vp
		}
	}

	return vp
}

func parseSPSForVUI(sps []byte) vuiParams {
	vp := vuiParams{
		cpbRemovalDelayLen: 24,
		dpbOutputDelayLen:  24,
	}

	rbsp := tsutil.RemoveEPB(sps[1:])
	br := &bitReader{data: rbsp}

	profileIdc := br.readBits(8)
	br.readBits(8) // constraint flags
	br.readBits(8) // level
	br.readUE()    // sps_id

	if profileIdc == 100 || profileIdc == 110 || profileIdc == 122 ||
		profileIdc == 244 || profileIdc == 44 || profileIdc == 83 ||
		profileIdc == 86 || profileIdc == 118 || profileIdc == 128 {
		chromaFmt := br.readUE()
		if chromaFmt == 3 {
			br.readBits(1)
		}
		br.readUE() // bit_depth_luma
		br.readUE() // bit_depth_chroma
		br.readBits(1)
		scalingMatrix := br.readBits(1)
		if scalingMatrix == 1 {
			limit := 8
			if chromaFmt == 3 {
				limit = 12
			}
			for i := 0; i < limit; i++ {
				flag := br.readBits(1)
				if flag == 1 {
					size := 16
					if i >= 6 {
						size = 64
					}
					lastScale := 8
					nextScale := 8
					for j := 0; j < size; j++ {
						if nextScale != 0 {
							delta := br.readUE()
							d := int(delta)
							if delta%2 == 0 {
								d = -int(delta / 2)
							} else {
								d = int((delta + 1) / 2)
							}
							nextScale = (lastScale + d + 256) % 256
						}
						if nextScale != 0 {
							lastScale = nextScale
						}
					}
				}
			}
		}
	}

	br.readUE() // log2_max_frame_num
	pocType := br.readUE()
	if pocType == 0 {
		br.readUE()
	} else if pocType == 1 {
		br.readBits(1)
		br.readUE()
		br.readUE()
		nrf := br.readUE()
		for i := 0; i < int(nrf); i++ {
			br.readUE()
		}
	}
	br.readUE()    // max_num_ref_frames
	br.readBits(1) // gaps

	br.readUE() // pic_width
	br.readUE() // pic_height
	frameMbsOnly := br.readBits(1)
	if frameMbsOnly == 0 {
		br.readBits(1)
	}
	br.readBits(1) // direct_8x8
	cropFlag := br.readBits(1)
	if cropFlag == 1 {
		br.readUE()
		br.readUE()
		br.readUE()
		br.readUE()
	}

	vuiPresent := br.readBits(1)
	if vuiPresent == 0 {
		return vp
	}

	arInfoPresent := br.readBits(1)
	if arInfoPresent == 1 {
		arIdc := br.readBits(8)
		if arIdc == 255 {
			br.readBits(16)
			br.readBits(16)
		}
	}

	overscanPresent := br.readBits(1)
	if overscanPresent == 1 {
		br.readBits(1)
	}

	videoSignalPresent := br.readBits(1)
	if videoSignalPresent == 1 {
		br.readBits(3)
		br.readBits(1)
		colourDesc := br.readBits(1)
		if colourDesc == 1 {
			br.readBits(24)
		}
	}

	chromaLocPresent := br.readBits(1)
	if chromaLocPresent == 1 {
		br.readUE()
		br.readUE()
	}

	timingInfoPresent := br.readBits(1)
	if timingInfoPresent == 1 {
		br.readBits(32)
		br.readBits(32)
		br.readBits(1)
	}

	parseHRD := func() {
		cpbCnt := br.readUE()
		br.readBits(4)
		br.readBits(4)
		for i := 0; i <= int(cpbCnt); i++ {
			br.readUE()
			br.readUE()
			br.readBits(1)
		}
		br.readBits(5)                             // initial_cpb_removal_delay_length_minus1
		vp.cpbRemovalDelayLen = br.readBits(5) + 1 // cpb_removal_delay_length_minus1
		vp.dpbOutputDelayLen = br.readBits(5) + 1  // dpb_output_delay_length_minus1
		vp.timeOffsetLen = br.readBits(5)          // time_offset_length
		vp.hrdPresent = true
	}

	nalHRDPresent := br.readBits(1)
	if nalHRDPresent == 1 {
		parseHRD()
	}

	vclHRDPresent := br.readBits(1)
	if vclHRDPresent == 1 && !vp.hrdPresent {
		parseHRD()
	}

	if nalHRDPresent == 1 || vclHRDPresent == 1 {
		br.readBits(1) // low_delay_hrd_flag
	}

	vp.picStructPresent = br.readBits(1) == 1

	return vp
}

func rewriteTimecodes(tsData []byte, vp vuiParams) []byte {
	pesPackets := tsutil.CollectPESPackets(tsData, vp.videoPID)

	frameNum := 0
	startTC := [4]int{1, 0, 0, 0} // 01:00:00:00
	fps := 30

	for i, pp := range pesPackets {
		tc := computeTimecode(startTC, frameNum, fps)
		frameNum++

		newES := injectClockTimestamp(pp.ESData, vp, tc)
		if newES != nil {
			pesPackets[i].ESData = newES
		}
	}

	return tsutil.RebuildTS(tsData, pesPackets, vp.videoPID)
}

func injectClockTimestamp(esData []byte, vp vuiParams, tc [4]int) []byte {
	nalStarts := tsutil.FindNALStarts(esData)
	type seiLoc struct {
		scStart int
		nalEnd  int
		si      int
	}
	var seiNALs []seiLoc

	for si, ns := range nalStarts {
		if ns >= len(esData) {
			continue
		}
		nalType := esData[ns] & 0x1F
		if nalType != 6 {
			continue
		}

		end := len(esData)
		if si+1 < len(nalStarts) {
			sc := nalStarts[si+1]
			if sc >= 4 && esData[sc-4] == 0 && esData[sc-3] == 0 && esData[sc-2] == 0 && esData[sc-1] == 1 {
				end = sc - 4
			} else if sc >= 3 && esData[sc-3] == 0 && esData[sc-2] == 0 && esData[sc-1] == 1 {
				end = sc - 3
			}
		}

		scStart := ns
		if ns >= 4 && esData[ns-4] == 0 && esData[ns-3] == 0 && esData[ns-2] == 0 && esData[ns-1] == 1 {
			scStart = ns - 4
		} else if ns >= 3 && esData[ns-3] == 0 && esData[ns-2] == 0 && esData[ns-1] == 1 {
			scStart = ns - 3
		}

		seiNALs = append(seiNALs, seiLoc{scStart: scStart, nalEnd: end, si: si})
	}

	for i := len(seiNALs) - 1; i >= 0; i-- {
		loc := seiNALs[i]
		ns := nalStarts[loc.si]
		seiPayload := tsutil.RemoveEPB(esData[ns+1 : loc.nalEnd])
		newSEI := rewritePicTimingSEI(seiPayload, vp, tc)
		if newSEI == nil {
			continue
		}

		nalHdr := esData[ns]
		var newNAL []byte
		newNAL = append(newNAL, 0, 0, 0, 1)
		newNAL = append(newNAL, nalHdr)
		newNAL = append(newNAL, tsutil.AddEPB(newSEI)...)

		var result []byte
		result = append(result, esData[:loc.scStart]...)
		result = append(result, newNAL...)
		result = append(result, esData[loc.nalEnd:]...)
		return result
	}
	return nil
}

func rewritePicTimingSEI(payload []byte, vp vuiParams, tc [4]int) []byte {
	i := 0
	var messages [][]byte
	foundPicTiming := false

	for i < len(payload) {
		if payload[i] == 0x80 {
			break
		}

		pt := 0
		for i < len(payload) && payload[i] == 0xFF {
			pt += 255
			i++
		}
		if i >= len(payload) {
			break
		}
		pt += int(payload[i])
		i++

		ps := 0
		for i < len(payload) && payload[i] == 0xFF {
			ps += 255
			i++
		}
		if i >= len(payload) {
			break
		}
		ps += int(payload[i])
		i++

		if i+ps > len(payload) {
			break
		}
		msgPayload := payload[i : i+ps]
		i += ps

		if pt == 1 {
			newPT := buildPicTimingWithTC(msgPayload, vp, tc)
			messages = append(messages, tsutil.EncodeSEIMessage(1, newPT))
			foundPicTiming = true
		} else {
			messages = append(messages, tsutil.EncodeSEIMessage(pt, msgPayload))
		}
	}

	if !foundPicTiming {
		return nil
	}

	var out []byte
	for _, m := range messages {
		out = append(out, m...)
	}
	out = append(out, 0x80)
	return out
}

func buildPicTimingWithTC(origPayload []byte, vp vuiParams, tc [4]int) []byte {
	br := &bitReader{data: origPayload}

	cpbRemovalDelay := br.readBits(vp.cpbRemovalDelayLen)
	dpbOutputDelay := br.readBits(vp.dpbOutputDelayLen)

	bw := &bitWriter{}
	bw.writeBits(cpbRemovalDelay, vp.cpbRemovalDelayLen)
	bw.writeBits(dpbOutputDelay, vp.dpbOutputDelayLen)

	picStruct := 0
	if vp.picStructPresent {
		picStruct = br.readBits(4)
		bw.writeBits(picStruct, 4)

		numClockTS := 1
		switch picStruct {
		case 0, 1, 2:
			numClockTS = 1
		case 3, 4:
			numClockTS = 2
		case 5, 6, 7, 8:
			numClockTS = 3
		}

		bw.writeBit(1) // clock_timestamp_flag = 1 for first

		bw.writeBits(0, 2)     // ct_type = 0 (progressive)
		bw.writeBit(0)         // nuit_field_based_flag
		bw.writeBits(4, 5)     // counting_type = 4 (30fps no drop)
		bw.writeBit(1)         // full_timestamp_flag
		bw.writeBit(0)         // discontinuity_flag
		bw.writeBit(0)         // cnt_dropped_flag
		bw.writeBits(tc[3], 8) // n_frames
		bw.writeBits(tc[2], 6) // seconds
		bw.writeBits(tc[1], 6) // minutes
		bw.writeBits(tc[0], 5) // hours

		if vp.timeOffsetLen > 0 {
			bw.writeBits(0, vp.timeOffsetLen)
		}

		for i := 1; i < numClockTS; i++ {
			bw.writeBit(0) // clock_timestamp_flag = 0 for remaining
		}
	}

	return bw.bytes()
}

func computeTimecode(start [4]int, frameNum, fps int) [4]int {
	totalFrames := start[0]*3600*fps + start[1]*60*fps + start[2]*fps + start[3] + frameNum
	h := totalFrames / (3600 * fps)
	totalFrames %= 3600 * fps
	m := totalFrames / (60 * fps)
	totalFrames %= 60 * fps
	s := totalFrames / fps
	f := totalFrames % fps
	return [4]int{h % 24, m, s, f}
}
