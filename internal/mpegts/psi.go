package mpegts

import "fmt"

const (
	tableIDPAT = 0x00
	tableIDPMT = 0x02
)

func isPSIPayload(pid uint16, pm *programMap) bool {
	return pid == pidPAT || pm.isPMTPID(pid)
}

func parsePSI(payload []byte, pid uint16, firstPacket *Packet, pm *programMap) ([]*DemuxerData, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("mpegts: PSI payload too short")
	}

	pointerField := int(payload[0])
	offset := 1 + pointerField
	if offset >= len(payload) {
		return nil, fmt.Errorf("mpegts: PSI pointer field out of range")
	}

	var results []*DemuxerData

	for offset < len(payload) {
		tableID := payload[offset]
		if tableID == 0xFF {
			break // stuffing bytes
		}
		if offset+3 > len(payload) {
			break
		}

		// section_syntax_indicator must be 1 for PAT/PMT.
		// Zero padding bytes will have this bit clear.
		if payload[offset+1]&0x80 == 0 {
			break
		}

		sectionLength := int(payload[offset+1]&0x0F)<<8 | int(payload[offset+2])
		sectionEnd := offset + 3 + sectionLength
		if sectionEnd > len(payload) {
			break
		}

		sectionData := payload[offset:sectionEnd]

		switch tableID {
		case tableIDPAT:
			pat, err := parsePATSection(sectionData)
			if err != nil {
				return results, err
			}
			results = append(results, &DemuxerData{
				FirstPacket: firstPacket,
				PAT:         pat,
			})

		case tableIDPMT:
			pmt, err := parsePMTSection(sectionData)
			if err != nil {
				return results, err
			}
			results = append(results, &DemuxerData{
				FirstPacket: firstPacket,
				PMT:         pmt,
			})
		}

		offset = sectionEnd
	}

	return results, nil
}

func parsePATSection(data []byte) (*PATData, error) {
	if err := verifyCRC32(data); err != nil {
		return nil, fmt.Errorf("mpegts: PAT %w", err)
	}

	// data layout:
	// [0]    table_id
	// [1-2]  section_syntax_indicator(1) + zero(1) + reserved(2) + section_length(12)
	// [3-4]  transport_stream_id
	// [5]    reserved(2) + version(5) + current_next(1)
	// [6]    section_number
	// [7]    last_section_number
	// [8..N-4] program entries (4 bytes each)
	// [N-4..N] CRC32

	if len(data) < 12 { // minimum: 8 header + 4 CRC
		return nil, fmt.Errorf("mpegts: PAT too short")
	}

	sectionLength := int(data[1]&0x0F)<<8 | int(data[2])
	// Entry data starts at byte 8, ends 4 bytes before the section end.
	entryStart := 8
	entryEnd := 3 + sectionLength - 4 // subtract CRC32
	if entryEnd > len(data)-4 {
		entryEnd = len(data) - 4
	}

	pat := &PATData{}
	for i := entryStart; i+4 <= entryEnd; i += 4 {
		programNumber := uint16(data[i])<<8 | uint16(data[i+1])
		pmtPID := uint16(data[i+2]&0x1F)<<8 | uint16(data[i+3])

		if programNumber == 0 {
			continue // NIT PID, skip
		}

		pat.Programs = append(pat.Programs, &PATProgram{
			ProgramNumber: programNumber,
			ProgramMapID:  pmtPID,
		})
	}

	return pat, nil
}

func parsePMTSection(data []byte) (*PMTData, error) {
	if err := verifyCRC32(data); err != nil {
		return nil, fmt.Errorf("mpegts: PMT %w", err)
	}

	// data layout:
	// [0]    table_id
	// [1-2]  section_syntax_indicator(1) + zero(1) + reserved(2) + section_length(12)
	// [3-4]  program_number
	// [5]    reserved(2) + version(5) + current_next(1)
	// [6]    section_number
	// [7]    last_section_number
	// [8-9]  reserved(3) + PCR_PID(13)
	// [10-11] reserved(4) + program_info_length(12)
	// [...] program descriptors
	// [...] elementary stream entries
	// [...] CRC32

	if len(data) < 16 { // minimum: 12 header + 4 CRC
		return nil, fmt.Errorf("mpegts: PMT too short")
	}

	sectionLength := int(data[1]&0x0F)<<8 | int(data[2])
	sectionEnd := 3 + sectionLength

	programInfoLength := int(data[10]&0x0F)<<8 | int(data[11])
	offset := 12 + programInfoLength

	pmt := &PMTData{}
	// Parse elementary stream entries until 4 bytes before section end (CRC).
	for offset+5 <= sectionEnd-4 {
		streamType := data[offset]
		elementaryPID := uint16(data[offset+1]&0x1F)<<8 | uint16(data[offset+2])
		esInfoLength := int(data[offset+3]&0x0F)<<8 | int(data[offset+4])

		pmt.ElementaryStreams = append(pmt.ElementaryStreams, &PMTElementaryStream{
			ElementaryPID: elementaryPID,
			StreamType:    streamType,
		})

		offset += 5 + esInfoLength
	}

	return pmt, nil
}
