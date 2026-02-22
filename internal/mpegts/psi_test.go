package mpegts

import (
	"encoding/binary"
	"testing"
)

// buildPAT constructs a valid PAT section with CRC32.
func buildPAT(tsID uint16, programs []struct{ num, pid uint16 }) []byte {
	// entries: 4 bytes each
	entryLen := len(programs) * 4
	sectionLength := 5 + entryLen + 4 // 5 fixed header bytes after section_length + entries + CRC

	data := make([]byte, 3+sectionLength)
	data[0] = tableIDPAT
	data[1] = 0xB0 | byte(sectionLength>>8)&0x0F // section_syntax_indicator=1
	data[2] = byte(sectionLength)
	data[3] = byte(tsID >> 8)
	data[4] = byte(tsID)
	data[5] = 0xC1 // reserved(2) + version(0) + current_next(1)
	data[6] = 0x00 // section_number
	data[7] = 0x00 // last_section_number

	offset := 8
	for _, p := range programs {
		data[offset] = byte(p.num >> 8)
		data[offset+1] = byte(p.num)
		data[offset+2] = 0xE0 | byte(p.pid>>8)&0x1F // reserved(3) + PID
		data[offset+3] = byte(p.pid)
		offset += 4
	}

	crc := computeCRC32(data[:offset])
	binary.BigEndian.PutUint32(data[offset:], crc)
	return data
}

// buildPMT constructs a valid PMT section with CRC32.
func buildPMT(programNum uint16, pcrPID uint16, streams []struct {
	streamType uint8
	pid        uint16
}) []byte {
	esLen := 0
	for range streams {
		esLen += 5 // stream_type(1) + reserved+PID(2) + reserved+ES_info_length(2)
	}
	sectionLength := 9 + esLen + 4 // 9 fixed bytes after section_length field + ES entries + CRC

	data := make([]byte, 3+sectionLength)
	data[0] = tableIDPMT
	data[1] = 0xB0 | byte(sectionLength>>8)&0x0F
	data[2] = byte(sectionLength)
	data[3] = byte(programNum >> 8)
	data[4] = byte(programNum)
	data[5] = 0xC1 // reserved + version + current_next
	data[6] = 0x00 // section_number
	data[7] = 0x00 // last_section_number
	data[8] = 0xE0 | byte(pcrPID>>8)&0x1F
	data[9] = byte(pcrPID)
	data[10] = 0xF0 // reserved(4) + program_info_length(12) = 0
	data[11] = 0x00

	offset := 12
	for _, s := range streams {
		data[offset] = s.streamType
		data[offset+1] = 0xE0 | byte(s.pid>>8)&0x1F
		data[offset+2] = byte(s.pid)
		data[offset+3] = 0xF0 // reserved(4) + ES_info_length(12) = 0
		data[offset+4] = 0x00
		offset += 5
	}

	crc := computeCRC32(data[:offset])
	binary.BigEndian.PutUint32(data[offset:], crc)
	return data
}

func TestParsePATSection_OneProgram(t *testing.T) {
	t.Parallel()
	programs := []struct{ num, pid uint16 }{{1, 0x1000}}
	data := buildPAT(1, programs)

	pat, err := parsePATSection(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pat.Programs) != 1 {
		t.Fatalf("expected 1 program, got %d", len(pat.Programs))
	}
	if pat.Programs[0].ProgramNumber != 1 {
		t.Errorf("program number = %d, want 1", pat.Programs[0].ProgramNumber)
	}
	if pat.Programs[0].ProgramMapID != 0x1000 {
		t.Errorf("PMT PID = 0x%X, want 0x1000", pat.Programs[0].ProgramMapID)
	}
}

func TestParsePATSection_TwoPrograms(t *testing.T) {
	t.Parallel()
	programs := []struct{ num, pid uint16 }{{1, 0x100}, {2, 0x200}}
	data := buildPAT(1, programs)

	pat, err := parsePATSection(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pat.Programs) != 2 {
		t.Fatalf("expected 2 programs, got %d", len(pat.Programs))
	}
}

func TestParsePATSection_SkipsNIT(t *testing.T) {
	t.Parallel()
	// program_number=0 is NIT, should be skipped
	programs := []struct{ num, pid uint16 }{{0, 0x10}, {1, 0x100}}
	data := buildPAT(1, programs)

	pat, err := parsePATSection(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pat.Programs) != 1 {
		t.Fatalf("expected 1 program (NIT skipped), got %d", len(pat.Programs))
	}
}

func TestParsePATSection_BadCRC(t *testing.T) {
	t.Parallel()
	programs := []struct{ num, pid uint16 }{{1, 0x100}}
	data := buildPAT(1, programs)
	data[len(data)-1] ^= 0xFF // corrupt CRC

	_, err := parsePATSection(data)
	if err == nil {
		t.Error("expected CRC error")
	}
}

func TestParsePMTSection_H264_AAC(t *testing.T) {
	t.Parallel()
	streams := []struct {
		streamType uint8
		pid        uint16
	}{
		{0x1B, 481}, // H.264
		{0x0F, 494}, // AAC
	}
	data := buildPMT(1, 481, streams)

	pmt, err := parsePMTSection(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pmt.ElementaryStreams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(pmt.ElementaryStreams))
	}
	if pmt.ElementaryStreams[0].StreamType != 0x1B {
		t.Errorf("stream 0 type = 0x%02X, want 0x1B", pmt.ElementaryStreams[0].StreamType)
	}
	if pmt.ElementaryStreams[0].ElementaryPID != 481 {
		t.Errorf("stream 0 PID = %d, want 481", pmt.ElementaryStreams[0].ElementaryPID)
	}
	if pmt.ElementaryStreams[1].StreamType != 0x0F {
		t.Errorf("stream 1 type = 0x%02X, want 0x0F", pmt.ElementaryStreams[1].StreamType)
	}
	if pmt.ElementaryStreams[1].ElementaryPID != 494 {
		t.Errorf("stream 1 PID = %d, want 494", pmt.ElementaryStreams[1].ElementaryPID)
	}
}

func TestParsePMTSection_BadCRC(t *testing.T) {
	t.Parallel()
	streams := []struct {
		streamType uint8
		pid        uint16
	}{
		{0x1B, 481},
	}
	data := buildPMT(1, 481, streams)
	data[len(data)-1] ^= 0xFF

	_, err := parsePMTSection(data)
	if err == nil {
		t.Error("expected CRC error")
	}
}

func TestParsePSI_PAT(t *testing.T) {
	t.Parallel()
	programs := []struct{ num, pid uint16 }{{1, 0x1000}}
	section := buildPAT(1, programs)

	// Wrap in PSI payload with pointer field
	payload := make([]byte, 1+len(section))
	payload[0] = 0x00 // pointer field
	copy(payload[1:], section)

	pm := newProgramMap()
	firstPkt := &Packet{Header: PacketHeader{PID: pidPAT}}

	results, err := parsePSI(payload, pidPAT, firstPkt, pm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].PAT == nil {
		t.Fatal("expected PAT data")
	}
	if len(results[0].PAT.Programs) != 1 {
		t.Errorf("expected 1 program, got %d", len(results[0].PAT.Programs))
	}
}

func TestParsePSI_PMT(t *testing.T) {
	t.Parallel()
	streams := []struct {
		streamType uint8
		pid        uint16
	}{{0x1B, 481}, {0x0F, 494}}
	section := buildPMT(1, 481, streams)

	payload := make([]byte, 1+len(section))
	payload[0] = 0x00
	copy(payload[1:], section)

	pm := newProgramMap()
	pm.addPMTPID(0x1000)
	firstPkt := &Packet{Header: PacketHeader{PID: 0x1000}}

	results, err := parsePSI(payload, 0x1000, firstPkt, pm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].PMT == nil {
		t.Fatal("expected PMT data")
	}
}

func TestParsePSI_WithPointerField(t *testing.T) {
	t.Parallel()
	programs := []struct{ num, pid uint16 }{{1, 0x1000}}
	section := buildPAT(1, programs)

	// Pointer field = 3, with 3 filler bytes before the section
	payload := make([]byte, 1+3+len(section))
	payload[0] = 0x03 // pointer field
	payload[1] = 0xFF
	payload[2] = 0xFF
	payload[3] = 0xFF
	copy(payload[4:], section)

	pm := newProgramMap()
	firstPkt := &Packet{Header: PacketHeader{PID: pidPAT}}

	results, err := parsePSI(payload, pidPAT, firstPkt, pm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestParsePSI_PaddingBytes(t *testing.T) {
	t.Parallel()
	programs := []struct{ num, pid uint16 }{{1, 0x1000}}
	section := buildPAT(1, programs)

	// Section followed by 0xFF padding
	payload := make([]byte, 1+len(section)+5)
	payload[0] = 0x00
	copy(payload[1:], section)
	for i := 1 + len(section); i < len(payload); i++ {
		payload[i] = 0xFF
	}

	pm := newProgramMap()
	firstPkt := &Packet{Header: PacketHeader{PID: pidPAT}}

	results, err := parsePSI(payload, pidPAT, firstPkt, pm)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (padding ignored), got %d", len(results))
	}
}
