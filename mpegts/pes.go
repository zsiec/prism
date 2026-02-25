package mpegts

import "fmt"

// isPESPayload checks for the PES start code prefix (0x000001).
func isPESPayload(data []byte) bool {
	return len(data) >= 3 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01
}

func parsePES(payload []byte) (*PESData, error) {
	if len(payload) < 6 {
		return nil, fmt.Errorf("mpegts: PES packet too short (%d bytes)", len(payload))
	}
	if !isPESPayload(payload) {
		return nil, fmt.Errorf("mpegts: invalid PES start code")
	}

	streamID := payload[3]
	packetLength := int(payload[4])<<8 | int(payload[5])

	pes := &PESData{
		Header: &PESHeader{
			StreamID: streamID,
		},
	}

	// Stream IDs that don't have an optional PES header:
	// padding_stream (0xBE), private_stream_2 (0xBF),
	// ECM (0xF0), EMM (0xF1), program_stream_directory (0xFF),
	// DSMCC (0xF2), ITU-T Rec. H.222.1 type E (0xF8)
	hasOptionalHeader := streamID != 0xBE && streamID != 0xBF &&
		streamID != 0xF0 && streamID != 0xF1 &&
		streamID != 0xF2 && streamID != 0xF8 && streamID != 0xFF

	if !hasOptionalHeader {
		if packetLength > 0 && 6+packetLength <= len(payload) {
			pes.Data = payload[6 : 6+packetLength]
		} else {
			pes.Data = payload[6:]
		}
		return pes, nil
	}

	if len(payload) < 9 {
		return nil, fmt.Errorf("mpegts: PES optional header too short")
	}

	// Optional header
	// payload[6]: marker(2) + scrambling(2) + priority(1) + alignment(1) + copyright(1) + original(1)
	// payload[7]: PTS_DTS_indicator(2) + ESCR(1) + ES_rate(1) + DSM_trick(1) + additional_copy(1) + CRC(1) + extension(1)
	// payload[8]: PES_header_data_length
	ptsDTSIndicator := (payload[7] >> 6) & 0x03
	headerDataLength := int(payload[8])

	dataStart := 9 + headerDataLength
	if dataStart > len(payload) {
		dataStart = len(payload)
	}

	pes.Header.OptionalHeader = &PESOptionalHeader{}

	switch ptsDTSIndicator {
	case 2: // PTS only
		if len(payload) >= 14 {
			pes.Header.OptionalHeader.PTS = parsePTSOrDTS(payload[9:14])
		}
	case 3: // PTS + DTS
		if len(payload) >= 19 {
			pes.Header.OptionalHeader.PTS = parsePTSOrDTS(payload[9:14])
			pes.Header.OptionalHeader.DTS = parsePTSOrDTS(payload[14:19])
		}
	}

	if packetLength > 0 {
		totalPES := 6 + packetLength
		if totalPES <= len(payload) {
			pes.Data = payload[dataStart:totalPES]
		} else {
			pes.Data = payload[dataStart:]
		}
	} else {
		// packetLength=0 means unbounded (video streams)
		pes.Data = payload[dataStart:]
	}

	return pes, nil
}

// parsePTSOrDTS extracts a 33-bit timestamp from 5 PES timestamp bytes.
func parsePTSOrDTS(bs []byte) *ClockReference {
	if len(bs) < 5 {
		return nil
	}
	base := int64(bs[0]>>1&0x07)<<30 |
		int64(bs[1])<<22 |
		int64(bs[2]>>1&0x7F)<<15 |
		int64(bs[3])<<7 |
		int64(bs[4]>>1&0x7F)
	return &ClockReference{Base: base}
}
