package main

// CEA-708 (DTVCC) caption injection: builds styled Service 1 caption packets
// that are carried as cc_type 2/3 triplets in the A/53 cc_data structure.
//
// Reference: CEA-708-E §§ 6-8, ATSC A/53 Part 4 § 6.2.3.1

// --- Style presets ---

type cea708Style struct {
	Name      string
	FontStyle byte // 0=default, 1=mono-serif, 2=prop-serif, 3=mono-sans, 4=prop-sans, 5=casual, 6=cursive, 7=small-caps
	FontSize  byte // 0=small, 1=standard, 2=large
	Italic    bool
	Underline bool
	EdgeType  byte // 0=none, 1=raised, 2=depressed, 3=uniform, 4=drop-shadow
	FGColor   [3]byte
	FGOpacity byte // 0=solid, 1=flash, 2=translucent, 3=transparent
	BGColor   [3]byte
	BGOpacity byte
	EdgeColor [3]byte
	// Window attributes
	FillColor   [3]byte
	FillOpacity byte
	BorderColor [3]byte
	BorderType  byte // 0=none, 1=raised, 2=depressed, 3=uniform, 4=drop-shadow
	Justify     byte // 0=left, 1=right, 2=center, 3=full
}

var stylePresets = []cea708Style{
	{
		Name: "Breaking News", FontStyle: 2, FontSize: 2,
		EdgeType: 1,
		FGColor:  [3]byte{2, 2, 2}, FGOpacity: 0,
		BGColor: [3]byte{2, 0, 0}, BGOpacity: 0,
		EdgeColor: [3]byte{2, 2, 0},
		FillColor: [3]byte{2, 0, 0}, FillOpacity: 0,
		BorderColor: [3]byte{2, 2, 0}, BorderType: 3,
		Justify: 2,
	},
	{
		Name: "Sports", FontStyle: 4, FontSize: 1,
		EdgeType: 4,
		FGColor:  [3]byte{2, 2, 0}, FGOpacity: 0,
		BGColor: [3]byte{0, 0, 0}, BGOpacity: 2,
		EdgeColor: [3]byte{0, 0, 0},
		FillColor: [3]byte{0, 0, 0}, FillOpacity: 2,
		BorderColor: [3]byte{0, 0, 0}, BorderType: 0,
		Justify: 0,
	},
	{
		Name: "Documentary", FontStyle: 2, FontSize: 1,
		FGColor: [3]byte{2, 2, 2}, FGOpacity: 0,
		BGColor: [3]byte{0, 0, 1}, BGOpacity: 2,
		EdgeColor: [3]byte{0, 0, 0},
		FillColor: [3]byte{0, 0, 1}, FillOpacity: 2,
		BorderColor: [3]byte{0, 0, 0}, BorderType: 0,
		Justify: 0,
	},
	{
		Name: "Pop-up", FontStyle: 5, FontSize: 1,
		EdgeType: 3,
		FGColor:  [3]byte{0, 2, 2}, FGOpacity: 0,
		BGColor: [3]byte{0, 0, 0}, BGOpacity: 0,
		EdgeColor: [3]byte{0, 2, 2},
		FillColor: [3]byte{0, 0, 0}, FillOpacity: 0,
		BorderColor: [3]byte{0, 2, 2}, BorderType: 3,
		Justify: 2,
	},
	{
		Name: "Alert", FontStyle: 4, FontSize: 2,
		Italic: true, Underline: true,
		FGColor: [3]byte{2, 0, 0}, FGOpacity: 0,
		BGColor: [3]byte{2, 2, 0}, BGOpacity: 0,
		EdgeColor: [3]byte{0, 0, 0},
		FillColor: [3]byte{2, 2, 0}, FillOpacity: 0,
		BorderColor: [3]byte{0, 0, 0}, BorderType: 0,
		Justify: 2,
	},
	{
		Name: "Classic", FontStyle: 3, FontSize: 1,
		EdgeType: 3,
		FGColor:  [3]byte{2, 2, 2}, FGOpacity: 0,
		BGColor: [3]byte{0, 0, 0}, BGOpacity: 3,
		EdgeColor: [3]byte{0, 0, 0},
		FillColor: [3]byte{0, 0, 0}, FillOpacity: 3,
		BorderColor: [3]byte{0, 0, 0}, BorderType: 0,
		Justify: 0,
	},
}

// --- DTVCC command opcodes ---

const (
	dtvccDeleteWindows  byte = 0x8C
	dtvccDefineWindow   byte = 0x98 // 0x98–0x9F for windows 0–7
	dtvccDisplayWindows byte = 0x89
	dtvccSetWinAttr     byte = 0x97
	dtvccSetPenAttr     byte = 0x90
	dtvccSetPenColor    byte = 0x91
	dtvccSetPenLoc      byte = 0x92
	dtvccCR             byte = 0x0D
)

// --- Command encoders ---

func cmdDeleteWindows(windowBits byte) []byte {
	return []byte{dtvccDeleteWindows, windowBits}
}

// cmdDefineWindow defines window 0 anchored at bottom-center.
// Parameters: visible, row_lock, col_lock, priority, relative_pos,
// anchor_v, anchor_h, row_count, col_count, anchor_point, pen_style, win_style
func cmdDefineWindow(rowCount, colCount byte) []byte {
	cmd := make([]byte, 7)
	cmd[0] = dtvccDefineWindow // window ID 0 (0x98 + window_id)
	// byte1: [visible(1) | row_lock(1) | col_lock(1) | priority(3) | relative_pos(1) | anchor_v_hi(1)]
	//        visible=0 (we display later), row_lock=0, col_lock=0, priority=0, relative=1, anchor_v[8]=0
	cmd[1] = 0x02 // relative_pos=1
	// byte2: anchor_v[7:0] — 85 = ~bottom (out of 99 for relative positioning)
	cmd[2] = 85
	// byte3: anchor_h — 50 = center
	cmd[3] = 50
	// byte4: [anchor_point(4) | row_count(4)]
	// anchor_point=8 (bottom-center), row_count-1
	cmd[4] = (8 << 4) | ((rowCount - 1) & 0x0F)
	// byte5: [col_count(6) | reserved(2)]
	cmd[5] = ((colCount - 1) & 0x3F) << 2
	// byte6: [window_style(3) | pen_style(3) | reserved(2)]
	cmd[6] = 0
	return cmd
}

func cmdSetWindowAttributes(s cea708Style) []byte {
	cmd := make([]byte, 5)
	cmd[0] = dtvccSetWinAttr
	// byte1: [fill_opacity(2) | fill_r(2) | fill_g(2) | fill_b(2)]
	cmd[1] = (s.FillOpacity << 6) | (s.FillColor[0] << 4) | (s.FillColor[1] << 2) | s.FillColor[2]
	// byte2: [border_type_lo(2) | border_r(2) | border_g(2) | border_b(2)]
	cmd[2] = ((s.BorderType & 0x03) << 6) | (s.BorderColor[0] << 4) | (s.BorderColor[1] << 2) | s.BorderColor[2]
	// byte3: [border_type_hi(1) | print_dir(2) | scroll_dir(2) | justify(2) | word_wrap(1)]
	cmd[3] = (((s.BorderType >> 2) & 0x01) << 7) | (s.Justify << 1) | 0x01 // word_wrap=1
	// byte4: [display_effect(2) | effect_dir(2) | effect_speed(4)]
	cmd[4] = 0
	return cmd
}

func cmdSetPenAttributes(s cea708Style) []byte {
	cmd := make([]byte, 3)
	cmd[0] = dtvccSetPenAttr
	// byte1: [pen_size(2) | offset(2) | text_tag(4)]
	cmd[1] = (s.FontSize << 6) | (0x01 << 4) // offset=normal(1), text_tag=0
	// byte2: [font_style(3) | edge_type(3) | underline(1) | italic(1)]
	italic := byte(0)
	if s.Italic {
		italic = 1
	}
	underline := byte(0)
	if s.Underline {
		underline = 1
	}
	cmd[2] = (s.FontStyle << 5) | (s.EdgeType << 2) | (underline << 1) | italic
	return cmd
}

func cmdSetPenColor(s cea708Style) []byte {
	cmd := make([]byte, 4)
	cmd[0] = dtvccSetPenColor
	// byte1: [fg_opacity(2) | fg_r(2) | fg_g(2) | fg_b(2)]
	cmd[1] = (s.FGOpacity << 6) | (s.FGColor[0] << 4) | (s.FGColor[1] << 2) | s.FGColor[2]
	// byte2: [bg_opacity(2) | bg_r(2) | bg_g(2) | bg_b(2)]
	cmd[2] = (s.BGOpacity << 6) | (s.BGColor[0] << 4) | (s.BGColor[1] << 2) | s.BGColor[2]
	// byte3: [edge_r(2) | edge_g(2) | edge_b(2) | reserved(2)]
	cmd[3] = (s.EdgeColor[0] << 6) | (s.EdgeColor[1] << 4) | (s.EdgeColor[2] << 2)
	return cmd
}

func cmdSetPenLocation(row, col byte) []byte {
	return []byte{dtvccSetPenLoc, row & 0x0F, col & 0x3F}
}

func cmdDisplayWindows(windowBits byte) []byte {
	return []byte{dtvccDisplayWindows, windowBits}
}

// --- DTVCC packet construction ---

// buildServiceBlock wraps service commands into a service block for service 1.
func buildServiceBlock(serviceData []byte) []byte {
	// Service block header: [service_number(3) | block_size(5)]
	blockSize := len(serviceData)
	if blockSize > 31 {
		blockSize = 31
	}
	header := byte((1 << 5) | (blockSize & 0x1F)) // service_number=1

	var block []byte
	block = append(block, header)
	block = append(block, serviceData[:blockSize]...)
	return block
}

// buildDTVCCPacket wraps a service block into a complete DTVCC packet with header.
func buildDTVCCPacket(serviceBlock []byte, seq byte) []byte {
	packetSize := len(serviceBlock)
	// size_code = ceil(packetSize / 2) — the packet is padded to size_code*2 bytes
	sizeCode := (packetSize + 1) / 2
	if sizeCode > 63 {
		sizeCode = 63
	}

	header := ((seq & 0x03) << 6) | byte(sizeCode&0x3F)

	var pkt []byte
	pkt = append(pkt, header)
	pkt = append(pkt, serviceBlock...)

	// Pad to size_code * 2 bytes total (including header)
	targetLen := sizeCode*2 + 1 // +1 for header byte
	for len(pkt) < targetLen {
		pkt = append(pkt, 0x00)
	}

	return pkt
}

// dtvccPacketToTriplets splits a DTVCC packet into cc_data triplets:
// first pair gets cc_type=3 (DTVCC start), rest get cc_type=2 (continuation).
func dtvccPacketToTriplets(pkt []byte) []ccTriplet {
	var triplets []ccTriplet
	for i := 0; i < len(pkt); i += 2 {
		b1 := pkt[i]
		b2 := byte(0x00)
		if i+1 < len(pkt) {
			b2 = pkt[i+1]
		}

		ccType := byte(2) // continuation
		if i == 0 {
			ccType = 3 // DTVCC start
		}
		triplets = append(triplets, ccTriplet{ccType: ccType, data1: b1, data2: b2})
	}
	return triplets
}

// --- Per-caption DTVCC service data ---

// buildCaptionServiceData builds the full DTVCC service command sequence for one
// caption entry with the given style.
func buildCaptionServiceData(text string, style cea708Style) []byte {
	lines := wordWrapForCEA608(text, 3)
	rowCount := byte(len(lines))
	if rowCount < 1 {
		rowCount = 1
	}
	colCount := byte(32)

	var data []byte
	data = append(data, cmdDeleteWindows(0x01)...) // delete window 0
	data = append(data, cmdDefineWindow(rowCount, colCount)...)
	data = append(data, cmdSetWindowAttributes(style)...)
	data = append(data, cmdSetPenAttributes(style)...)
	data = append(data, cmdSetPenColor(style)...)
	data = append(data, cmdSetPenLocation(0, 0)...)

	for li, line := range lines {
		for _, ch := range line {
			if ch >= 0x20 && ch <= 0x7E {
				data = append(data, ch)
			} else {
				data = append(data, '?')
			}
		}
		if li < len(lines)-1 {
			data = append(data, dtvccCR)
		}
	}

	data = append(data, cmdDisplayWindows(0x01)...) // display window 0
	return data
}

// --- Frame-level DTVCC triplet scheduling ---

// buildDTVCCFrames generates per-frame DTVCC triplets for the primary SRT track.
// Each caption entry gets a styled Service 1 packet; styles cycle through presets.
func buildDTVCCFrames(entries []srtEntry, fps float64, numFrames int) [][]ccTriplet {
	frames := make([][]ccTriplet, numFrames)
	seq := byte(0)

	for entryIdx, entry := range entries {
		startFrame := int(entry.startSec * fps)
		endFrame := int(entry.endSec * fps)
		if startFrame >= numFrames {
			break
		}
		if endFrame > numFrames {
			endFrame = numFrames
		}

		style := stylePresets[entryIdx%len(stylePresets)]
		serviceData := buildCaptionServiceData(entry.text, style)

		// Service data may exceed one service block (31 bytes max).
		// Split into multiple DTVCC packets if needed.
		var allTriplets []ccTriplet
		for off := 0; off < len(serviceData); {
			chunkEnd := off + 31
			if chunkEnd > len(serviceData) {
				chunkEnd = len(serviceData)
			}
			chunk := serviceData[off:chunkEnd]
			sb := buildServiceBlock(chunk)
			pkt := buildDTVCCPacket(sb, seq)
			seq = (seq + 1) & 0x03
			allTriplets = append(allTriplets, dtvccPacketToTriplets(pkt)...)
			off = chunkEnd
		}

		// Spread triplets across frames starting at startFrame.
		// Max ~29 DTVCC triplets per frame to leave room for 608 fallback.
		const maxPerFrame = 29
		frameOff := 0
		for i := 0; i < len(allTriplets); {
			f := startFrame + frameOff
			if f >= numFrames {
				break
			}
			end := i + maxPerFrame
			if end > len(allTriplets) {
				end = len(allTriplets)
			}
			frames[f] = append(frames[f], allTriplets[i:end]...)
			i = end
			frameOff++
		}

		// At end time, send a delete-window packet to clear the caption.
		if endFrame > 0 && endFrame < numFrames {
			clearData := cmdDeleteWindows(0x01)
			sb := buildServiceBlock(clearData)
			pkt := buildDTVCCPacket(sb, seq)
			seq = (seq + 1) & 0x03
			frames[endFrame] = append(frames[endFrame], dtvccPacketToTriplets(pkt)...)
		}
	}

	return frames
}
