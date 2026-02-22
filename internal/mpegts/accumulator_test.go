package mpegts

import "testing"

func TestAccumulator_PUSIFlush(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	p1 := &Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x01}}
	flushed := acc.add(p1)
	if flushed != nil {
		t.Error("first packet should not flush")
	}

	p2 := &Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, ContinuityCounter: 1}, Payload: []byte{0x02}}
	flushed = acc.add(p2)
	if flushed != nil {
		t.Error("continuation should not flush")
	}

	p3 := &Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 2}, Payload: []byte{0x03}}
	flushed = acc.add(p3)
	if len(flushed) != 2 {
		t.Errorf("PUSI should flush 2 packets, got %d", len(flushed))
	}
}

func TestAccumulator_CCDiscontinuity(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x01}})
	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, ContinuityCounter: 1}, Payload: []byte{0x02}})

	// CC jump from 1 to 5 (skip 2,3,4)
	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, ContinuityCounter: 5}, Payload: []byte{0x03}})

	// Flush with new PUSI should only have the packet after discontinuity
	flushed := acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 6}, Payload: []byte{0x04}})
	if len(flushed) != 1 {
		t.Errorf("after discontinuity, should flush 1 packet, got %d", len(flushed))
	}
}

func TestAccumulator_DuplicateFilter(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 3}, Payload: []byte{0x01}})
	// Duplicate with same CC
	flushed := acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, ContinuityCounter: 3}, Payload: []byte{0x01}})
	if flushed != nil {
		t.Error("duplicate should be filtered")
	}

	// Next PUSI should only flush 1 packet (the original, not the dupe)
	flushed = acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 4}, Payload: []byte{0x02}})
	if len(flushed) != 1 {
		t.Errorf("should flush 1 packet, got %d", len(flushed))
	}
}

func TestAccumulator_TEIDiscard(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x01}})
	// TEI packet
	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, TransportErrorIndicator: true, ContinuityCounter: 1}, Payload: []byte{0x02}})

	// After TEI, buffer should be cleared
	flushed := acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 2}, Payload: []byte{0x03}})
	if flushed != nil {
		t.Error("after TEI, there should be no buffered packets to flush")
	}
}

func TestAccumulator_AdaptationOnlySkipped(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x01}})
	// Adaptation-only packet (no payload)
	flushed := acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: false, HasAdaptationField: true, ContinuityCounter: 0}})
	if flushed != nil {
		t.Error("adaptation-only should not trigger flush")
	}
}

func TestAccumulator_CCWraparound(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 15}, Payload: []byte{0x01}})
	// CC wraps from 15 to 0
	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, ContinuityCounter: 0}, Payload: []byte{0x02}})

	flushed := acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 1}, Payload: []byte{0x03}})
	if len(flushed) != 2 {
		t.Errorf("CC wraparound should preserve buffer, got %d packets", len(flushed))
	}
}

func TestAccumulator_DiscontinuityIndicator(t *testing.T) {
	pm := newProgramMap()
	acc := newPacketAccumulator(0x100, pm)

	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x01}})
	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, ContinuityCounter: 1}, Payload: []byte{0x02}})

	// CC jump from 1 to 9, but discontinuity indicator is set — buffer should be preserved.
	acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, HasAdaptationField: true, DiscontinuityIndicator: true, ContinuityCounter: 9}, Payload: []byte{0x03}})

	flushed := acc.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 10}, Payload: []byte{0x04}})
	if len(flushed) != 3 {
		t.Errorf("discontinuity indicator should preserve buffer, got %d packets", len(flushed))
	}
}

func TestPacketPool_Dump(t *testing.T) {
	pm := newProgramMap()
	pp := newPacketPool(pm)

	pp.add(&Packet{Header: PacketHeader{PID: 0x100, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x01}})
	pp.add(&Packet{Header: PacketHeader{PID: 0x200, HasPayload: true, PayloadUnitStartIndicator: true, ContinuityCounter: 0}, Payload: []byte{0x02}})

	all := pp.dump()
	if len(all) != 2 {
		t.Errorf("dump should return 2 groups, got %d", len(all))
	}
}

func TestIsPSIComplete_SingleSection(t *testing.T) {
	// Build a minimal PAT-like section:
	// pointer_field=0, table_id=0x00, section_syntax_indicator=1, section_length=5
	payload := []byte{
		0x00,       // pointer field
		0x00,       // table_id (PAT)
		0x80, 0x05, // section_syntax_indicator=1, section_length=5
		0x01, 0x02, 0x03, 0x04, 0x05, // section data (5 bytes)
	}
	packets := []*Packet{{Payload: payload}}
	if !isPSIComplete(packets) {
		t.Error("expected PSI complete")
	}
}

func TestIsPSIComplete_Incomplete(t *testing.T) {
	payload := []byte{
		0x00,       // pointer field
		0x00,       // table_id (PAT)
		0x80, 0x0A, // section_syntax_indicator=1, section_length=10
		0x01, 0x02, 0x03, // only 3 of 10 bytes
	}
	packets := []*Packet{{Payload: payload}}
	if isPSIComplete(packets) {
		t.Error("expected PSI incomplete")
	}
}

func TestIsPSIComplete_WithPadding(t *testing.T) {
	payload := []byte{
		0x00,       // pointer field
		0x00,       // table_id
		0x00, 0x02, // section_length = 2
		0x01, 0x02, // section data
		0xFF, 0xFF, // padding
	}
	packets := []*Packet{{Payload: payload}}
	if !isPSIComplete(packets) {
		t.Error("expected PSI complete with padding")
	}
}
