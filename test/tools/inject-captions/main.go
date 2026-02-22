package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/zsiec/prism/test/tools/tsutil"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: inject-captions <input.ts> <output.ts> <captions.srt> [captions2.srt ...]\n")
		fmt.Fprintf(os.Stderr, "Injects CEA-608 closed captions into H.264 video as A/53 SEI user data.\n")
		fmt.Fprintf(os.Stderr, "Each SRT file maps to a CEA-608 field/channel (CC1, CC2, CC3, CC4).\n")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	outputFile := os.Args[2]
	srtFiles := os.Args[3:]

	if len(srtFiles) > 4 {
		fmt.Fprintf(os.Stderr, "warning: only 4 CEA-608 channels supported, ignoring extra SRT files\n")
		srtFiles = srtFiles[:4]
	}

	tsData, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}

	var tracks [][]srtEntry
	for _, sf := range srtFiles {
		entries, err := parseSRT(sf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse SRT %s: %v\n", sf, err)
			os.Exit(1)
		}
		tracks = append(tracks, entries)
		fmt.Fprintf(os.Stderr, "SRT %s: %d entries\n", sf, len(entries))
	}

	videoPID := findVideoPID(tsData)
	if videoPID == 0 {
		fmt.Fprintf(os.Stderr, "error: no video PID found\n")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Video PID: 0x%04X\n", videoPID)

	pesPackets := tsutil.CollectPESPackets(tsData, videoPID)
	fmt.Fprintf(os.Stderr, "Video frames: %d\n", len(pesPackets))

	if hasCaptions := detectExistingCaptions(pesPackets); hasCaptions {
		fmt.Fprintf(os.Stderr, "Existing CEA-608/708 captions detected, skipping injection\n")
		if inputFile != outputFile {
			if err := tsutil.CopyFile(inputFile, outputFile); err != nil {
				fmt.Fprintf(os.Stderr, "copy: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Copied input to %s\n", outputFile)
		}
		os.Exit(0)
	}

	fps := detectFPS(tsData, videoPID)
	fmt.Fprintf(os.Stderr, "Detected FPS: %.2f\n", fps)

	// Build one flat triplet sequence per channel, then interleave into per-frame SEIs.
	// Each SRT maps to a channel: SRT 0→CC1 (field 0), SRT 1→CC3 (field 1),
	// SRT 2→CC2 (field 0), SRT 3→CC4 (field 1).
	numFrames := len(pesPackets)
	channelTriplets := make([][]ccTriplet, len(tracks))
	for chIdx, entries := range tracks {
		field := byte(0)
		if chIdx == 1 || chIdx == 3 {
			field = 1
		}
		channelTriplets[chIdx] = buildCaptionTriplets(entries, fps, numFrames, field)
		fmt.Fprintf(os.Stderr, "Channel %d: %d triplets for %d frames\n", chIdx, len(channelTriplets[chIdx]), numFrames)
	}

	for frameIdx := range pesPackets {
		var frameTriplets []ccTriplet
		for _, ct := range channelTriplets {
			if frameIdx < len(ct) {
				frameTriplets = append(frameTriplets, ct[frameIdx])
			}
		}
		if len(frameTriplets) == 0 {
			frameTriplets = append(frameTriplets, ccTriplet{ccType: 0, data1: 0x80, data2: 0x80})
		}

		seiNAL := buildCaptionSEI(frameTriplets)
		pesPackets[frameIdx].ESData = insertSEINAL(pesPackets[frameIdx].ESData, seiNAL)
	}

	outData := tsutil.RebuildTS(tsData, pesPackets, videoPID)
	if err := os.WriteFile(outputFile, outData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Wrote %d bytes to %s\n", len(outData), outputFile)
}

// --- SRT parsing ---

type srtEntry struct {
	startSec float64
	endSec   float64
	text     string
}

var timecodeRe = regexp.MustCompile(`(\d{2}):(\d{2}):(\d{2}),(\d{3})\s*-->\s*(\d{2}):(\d{2}):(\d{2}),(\d{3})`)

func parseSRT(path string) ([]srtEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []srtEntry
	scanner := bufio.NewScanner(f)
	state := 0 // 0=index, 1=timecode, 2=text
	var current srtEntry
	var textLines []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		switch state {
		case 0:
			if line == "" {
				continue
			}
			if _, err := strconv.Atoi(line); err == nil {
				state = 1
			}
		case 1:
			m := timecodeRe.FindStringSubmatch(line)
			if m == nil {
				state = 0
				continue
			}
			current.startSec = parseSRTTime(m[1], m[2], m[3], m[4])
			current.endSec = parseSRTTime(m[5], m[6], m[7], m[8])
			textLines = nil
			state = 2
		case 2:
			if line == "" {
				current.text = strings.Join(textLines, "\n")
				entries = append(entries, current)
				current = srtEntry{}
				textLines = nil
				state = 0
			} else {
				textLines = append(textLines, line)
			}
		}
	}

	if len(textLines) > 0 {
		current.text = strings.Join(textLines, "\n")
		entries = append(entries, current)
	}

	return entries, scanner.Err()
}

func parseSRTTime(h, m, s, ms string) float64 {
	hi, _ := strconv.Atoi(h)
	mi, _ := strconv.Atoi(m)
	si, _ := strconv.Atoi(s)
	msi, _ := strconv.Atoi(ms)
	return float64(hi)*3600 + float64(mi)*60 + float64(si) + float64(msi)/1000.0
}

// --- CEA-608 encoder ---

// buildCaptionTriplets converts SRT entries into a frame-indexed array of
// cc_data triplets using roll-up mode. Exactly one triplet per frame,
// following the same protocol as ccx's test vector generator:
//   - Control codes are sent twice (consecutive frames) for dedup
//   - Roll-up 2 mode (RU2 = 0x14 0x25)
//   - Carriage return (CR = 0x14 0x2D) between lines
//   - One character pair per frame
//   - Erase displayed memory (EDM = 0x14 0x2C) at end of caption
func buildCaptionTriplets(entries []srtEntry, fps float64, numFrames int, field byte) []ccTriplet {
	// First build the full command sequence as byte pairs
	var commands []cc608Pair
	for _, entry := range entries {
		startFrame := int(entry.startSec * fps)
		endFrame := int(entry.endSec * fps)
		if startFrame >= numFrames {
			break
		}
		if endFrame > numFrames {
			endFrame = numFrames
		}

		text := normalizeForCEA608(entry.text)

		// Build the pair sequence for this entry:
		// RU2 (x2), EDM (x2), PAC row14 (x2), text pairs
		var entryPairs []cc608Pair
		entryPairs = append(entryPairs,
			cc608Control(0x14, 0x25), // RU2
			cc608Control(0x14, 0x25), // RU2 dedup
			cc608Control(0x14, 0x2C), // EDM - clear previous
			cc608Control(0x14, 0x2C), // EDM dedup
			cc608Control(0x14, 0x60), // PAC row 14 (bottom), white, col 0
			cc608Control(0x14, 0x60), // PAC dedup
		)

		for i := 0; i < len(text); i += 2 {
			if i+1 < len(text) {
				entryPairs = append(entryPairs, cc608Text(text[i], text[i+1]))
			} else {
				entryPairs = append(entryPairs, cc608Text(text[i], 0x80))
			}
		}

		// Schedule: start emitting at startFrame, one pair per frame
		for i, p := range entryPairs {
			f := startFrame + i
			if f >= numFrames {
				break
			}
			cmd := p
			cmd.frame = f
			commands = append(commands, cmd)
		}

		// Schedule EDM at end time
		if endFrame < numFrames {
			edm1 := cc608Control(0x14, 0x2C)
			edm1.frame = endFrame
			edm2 := cc608Control(0x14, 0x2C)
			edm2.frame = endFrame + 1
			commands = append(commands, edm1, edm2)
		}
	}

	// Build per-frame triplets
	triplets := make([]ccTriplet, numFrames)
	for i := range triplets {
		triplets[i] = ccTriplet{ccType: field, data1: 0x80, data2: 0x80} // padding
	}
	for _, cmd := range commands {
		if cmd.frame >= 0 && cmd.frame < numFrames {
			triplets[cmd.frame] = ccTriplet{
				ccType: field,
				data1:  cmd.cc1,
				data2:  cmd.cc2,
			}
		}
	}

	return triplets
}

type cc608Pair struct {
	cc1, cc2 byte
	ctrl     bool
	frame    int // set during scheduling
}

func cc608Control(cc1, cc2 byte) cc608Pair { return cc608Pair{cc1: cc1, cc2: cc2, ctrl: true} }
func cc608Text(c1, c2 byte) cc608Pair      { return cc608Pair{cc1: c1, cc2: c2} }

func normalizeForCEA608(text string) []byte {
	lines := strings.Split(text, "\n")
	if len(lines) > 4 {
		lines = lines[:4]
	}
	for i, line := range lines {
		if len(line) > 32 {
			lines[i] = line[:32]
		}
	}
	combined := strings.Join(lines, " ")

	var out []byte
	for _, ch := range combined {
		if ch >= 0x20 && ch <= 0x7E {
			out = append(out, byte(ch))
		} else {
			out = append(out, '?')
		}
	}
	return out
}

// --- A/53 SEI NAL builder ---

type ccTriplet struct {
	ccType byte // 0=field1 (CC1/CC2), 1=field2 (CC3/CC4)
	data1  byte
	data2  byte
}

// buildCaptionSEI builds a complete H.264 SEI NAL unit containing A/53 GA94
// user_data_registered_itu_t_t35 caption data.
func buildCaptionSEI(triplets []ccTriplet) []byte {
	a53Payload := buildA53Payload(triplets)

	// SEI message: type 4 (user_data_registered_itu_t_t35), then size, then payload
	seiMessage := tsutil.EncodeSEIMessage(4, a53Payload)
	seiMessage = append(seiMessage, 0x80) // RBSP trailing bits

	// Full NAL: start code + NAL header (type 6 = SEI) + emulation-prevention-escaped payload
	var nal []byte
	nal = append(nal, 0x00, 0x00, 0x00, 0x01) // start code
	nal = append(nal, 0x06)                   // NAL header: type 6 (SEI), NRI=0
	nal = append(nal, tsutil.AddEPB(seiMessage)...)
	return nal
}

// buildA53Payload constructs the ATSC A/53 Part 4 cc_data() structure.
func buildA53Payload(triplets []ccTriplet) []byte {
	ccCount := len(triplets)
	if ccCount > 31 {
		ccCount = 31
	}

	var payload []byte
	payload = append(payload, 0xB5)       // itu_t_t35_country_code (United States)
	payload = append(payload, 0x00, 0x31) // itu_t_t35_provider_code (ATSC)
	payload = append(payload, 'G', 'A', '9', '4')
	payload = append(payload, 0x03) // user_data_type_code (cc_data)

	// cc_data_pkt: process_cc_data_flag=1, zero_bit=0, cc_count
	payload = append(payload, 0x40|byte(ccCount)&0x1F) // process_cc=1
	payload = append(payload, 0xFF)                    // em_data (reserved, all 1s)

	for i := 0; i < ccCount; i++ {
		t := triplets[i]
		// marker_bits(5) = 11111, cc_valid=1, cc_type(2)
		marker := byte(0xFC) | (t.ccType & 0x03) // 11111 1 cc_type
		// Add odd parity to data bytes
		payload = append(payload, marker, addParity(t.data1), addParity(t.data2))
	}

	payload = append(payload, 0xFF) // marker_bits (end)

	return payload
}

// addParity sets the high bit for odd parity (CEA-608 requirement).
func addParity(b byte) byte {
	b &= 0x7F
	ones := 0
	v := b
	for v != 0 {
		ones += int(v & 1)
		v >>= 1
	}
	if ones%2 == 0 {
		return b | 0x80
	}
	return b
}

// --- TS infrastructure (tool-specific helpers) ---

func findVideoPID(tsData []byte) uint16 {
	for off := 0; off+tsutil.TSPacketSize <= len(tsData); off += tsutil.TSPacketSize {
		pkt := tsData[off : off+tsutil.TSPacketSize]
		if pkt[0] != 0x47 {
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
		if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
			continue
		}

		streamID := payload[3]
		if streamID >= 0xE0 && streamID <= 0xEF {
			pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
			return pid
		}
	}
	return 0
}

func detectFPS(tsData []byte, videoPID uint16) float64 {
	var ptsList []int64
	for off := 0; off+tsutil.TSPacketSize <= len(tsData); off += tsutil.TSPacketSize {
		pkt := tsData[off : off+tsutil.TSPacketSize]
		if pkt[0] != 0x47 {
			continue
		}
		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
		if pid != videoPID {
			continue
		}
		if pkt[1]&0x40 == 0 {
			continue
		}
		headerLen := 4
		if pkt[3]&0x20 != 0 {
			headerLen = 5 + int(pkt[4])
		}
		if headerLen >= tsutil.TSPacketSize {
			continue
		}
		payload := pkt[headerLen:]
		if len(payload) < 14 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
			continue
		}
		flags := payload[7]
		if flags&0x80 != 0 {
			pts := extractPTS(payload[9:14])
			ptsList = append(ptsList, pts)
		}
		if len(ptsList) >= 30 {
			break
		}
	}

	if len(ptsList) < 2 {
		return 30.0
	}

	totalDelta := ptsList[len(ptsList)-1] - ptsList[0]
	avgDelta := float64(totalDelta) / float64(len(ptsList)-1)
	fps := 90000.0 / avgDelta

	// Snap to common rates
	common := []float64{23.976, 24, 25, 29.97, 30, 50, 59.94, 60}
	best := fps
	bestDiff := math.MaxFloat64
	for _, c := range common {
		d := math.Abs(fps - c)
		if d < bestDiff {
			bestDiff = d
			best = c
		}
	}
	return best
}

func extractPTS(data []byte) int64 {
	pts := int64(data[0]>>1&0x07) << 30
	pts |= int64(data[1]) << 22
	pts |= int64(data[2]>>1) << 15
	pts |= int64(data[3]) << 7
	pts |= int64(data[4] >> 1)
	return pts
}

// insertSEINAL inserts a new SEI NAL unit into the elementary stream data,
// placing it before the first VCL NAL (IDR or non-IDR slice) as required by
// ITU-T H.264 section 7.4.1.2.3.
func insertSEINAL(esData []byte, seiNAL []byte) []byte {
	nalStarts := tsutil.FindNALStarts(esData)

	for _, ns := range nalStarts {
		if ns >= len(esData) {
			continue
		}
		nalType := esData[ns] & 0x1F
		// VCL NAL types: 1 (non-IDR slice), 5 (IDR slice)
		if nalType == 1 || nalType == 5 {
			// Find the start code position for this NAL
			insertPos := ns
			if ns >= 4 && esData[ns-4] == 0 && esData[ns-3] == 0 && esData[ns-2] == 0 && esData[ns-1] == 1 {
				insertPos = ns - 4
			} else if ns >= 3 && esData[ns-3] == 0 && esData[ns-2] == 0 && esData[ns-1] == 1 {
				insertPos = ns - 3
			}

			var result []byte
			result = append(result, esData[:insertPos]...)
			result = append(result, seiNAL...)
			result = append(result, esData[insertPos:]...)
			return result
		}
	}

	// No VCL found, append at the end
	return append(esData, seiNAL...)
}

// detectExistingCaptions scans the first N video PES packets for A/53 GA94
// caption data in SEI NAL units. Returns true if any non-empty cc_data is found.
func detectExistingCaptions(pesPackets []tsutil.PESPacket) bool {
	limit := 120
	if len(pesPackets) < limit {
		limit = len(pesPackets)
	}

	for i := 0; i < limit; i++ {
		if hasA53CaptionSEI(pesPackets[i].ESData) {
			return true
		}
	}
	return false
}

// hasA53CaptionSEI checks if the elementary stream data contains an SEI NAL
// with a user_data_registered_itu_t_t35 payload carrying A/53 GA94 cc_data
// that has at least one valid (non-null) caption byte pair.
func hasA53CaptionSEI(esData []byte) bool {
	nalStarts := tsutil.FindNALStarts(esData)

	for si, ns := range nalStarts {
		if ns >= len(esData) {
			continue
		}
		nalType := esData[ns] & 0x1F
		if nalType != 6 { // SEI
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

		seiPayload := tsutil.RemoveEPB(esData[ns+1 : end])
		if containsA53Captions(seiPayload) {
			return true
		}
	}
	return false
}

// containsA53Captions walks SEI message payloads looking for
// user_data_registered_itu_t_t35 (type 4) with A/53 GA94 cc_data that
// contains at least one valid caption triplet.
func containsA53Captions(seiPayload []byte) bool {
	i := 0
	for i < len(seiPayload) {
		if seiPayload[i] == 0x80 {
			break
		}

		payloadType := 0
		for i < len(seiPayload) && seiPayload[i] == 0xFF {
			payloadType += 255
			i++
		}
		if i >= len(seiPayload) {
			break
		}
		payloadType += int(seiPayload[i])
		i++

		payloadSize := 0
		for i < len(seiPayload) && seiPayload[i] == 0xFF {
			payloadSize += 255
			i++
		}
		if i >= len(seiPayload) {
			break
		}
		payloadSize += int(seiPayload[i])
		i++

		if i+payloadSize > len(seiPayload) {
			break
		}

		if payloadType == 4 {
			payload := seiPayload[i : i+payloadSize]
			if isA53WithCaptions(payload) {
				return true
			}
		}
		i += payloadSize
	}
	return false
}

// isA53WithCaptions checks an ITU-T T.35 payload for A/53 GA94 cc_data
// containing at least one valid (non-padding) caption triplet.
func isA53WithCaptions(payload []byte) bool {
	if len(payload) < 10 {
		return false
	}
	// Country code: US (0xB5), provider: ATSC (0x0031), identifier: GA94
	if payload[0] != 0xB5 || payload[1] != 0x00 || payload[2] != 0x31 {
		return false
	}
	if payload[3] != 'G' || payload[4] != 'A' || payload[5] != '9' || payload[6] != '4' {
		return false
	}
	if payload[7] != 0x03 { // user_data_type_code = cc_data
		return false
	}

	ccHeader := payload[8]
	if ccHeader&0x40 == 0 { // process_cc_data_flag
		return false
	}
	ccCount := int(ccHeader & 0x1F)

	tripletStart := 10
	if tripletStart+ccCount*3 > len(payload) {
		return false
	}

	for j := 0; j < ccCount; j++ {
		offset := tripletStart + j*3
		marker := payload[offset]
		cc1 := payload[offset+1] & 0x7F
		cc2 := payload[offset+2] & 0x7F

		if marker&0x04 == 0 { // cc_valid
			continue
		}
		// Non-null pair = real caption data
		if cc1 != 0 || cc2 != 0 {
			return true
		}
	}
	return false
}
