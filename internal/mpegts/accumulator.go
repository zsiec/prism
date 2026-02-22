package mpegts

import "sort"

const pidPAT = 0x0000

// programMap tracks which PIDs carry PMT sections.
type programMap struct {
	m map[uint16]bool
}

func newProgramMap() *programMap {
	return &programMap{m: make(map[uint16]bool)}
}

func (pm *programMap) addPMTPID(pid uint16) {
	pm.m[pid] = true
}

func (pm *programMap) isPMTPID(pid uint16) bool {
	return pm.m[pid]
}

// packetAccumulator buffers packets for a single PID until a flush trigger.
type packetAccumulator struct {
	pid        uint16
	packets    []*Packet
	programMap *programMap
}

func newPacketAccumulator(pid uint16, pm *programMap) *packetAccumulator {
	return &packetAccumulator{
		pid:        pid,
		programMap: pm,
	}
}

func (pa *packetAccumulator) add(p *Packet) []*Packet {
	// Skip packets with transport errors.
	if p.Header.TransportErrorIndicator {
		pa.packets = nil
		return nil
	}

	// Skip adaptation-only packets (no payload).
	if !p.Header.HasPayload {
		return nil
	}

	// Discontinuity check: compare CC against last buffered packet.
	// A signaled discontinuity indicator means the CC jump is expected.
	if len(pa.packets) > 0 && !p.Header.DiscontinuityIndicator {
		prev := pa.packets[len(pa.packets)-1].Header.ContinuityCounter
		expected := (prev + 1) & 0x0F
		if p.Header.ContinuityCounter != expected {
			if p.Header.ContinuityCounter == prev {
				return nil // duplicate packet, drop
			}
			// Unsignaled discontinuity — discard buffered packets.
			pa.packets = nil
		}
	}

	var flushed []*Packet

	if p.Header.PayloadUnitStartIndicator && len(pa.packets) > 0 {
		flushed = pa.packets
		pa.packets = nil
	}

	pa.packets = append(pa.packets, p)

	// For PSI PIDs, check if the section is complete.
	if flushed == nil && pa.isPSI() && isPSIComplete(pa.packets) {
		flushed = pa.packets
		pa.packets = nil
	}

	return flushed
}

func (pa *packetAccumulator) isPSI() bool {
	return pa.pid == pidPAT || pa.programMap.isPMTPID(pa.pid)
}

func (pa *packetAccumulator) flush() []*Packet {
	if len(pa.packets) == 0 {
		return nil
	}
	flushed := pa.packets
	pa.packets = nil
	return flushed
}

// isPSIComplete checks whether the accumulated payloads contain a complete PSI section.
func isPSIComplete(packets []*Packet) bool {
	var payload []byte
	for _, p := range packets {
		payload = append(payload, p.Payload...)
	}
	if len(payload) < 1 {
		return false
	}

	pointerField := int(payload[0])
	offset := 1 + pointerField
	if offset >= len(payload) {
		return false
	}

	// Walk sections.
	for offset < len(payload) {
		if payload[offset] == 0xFF {
			return true // stuffing bytes, section is complete
		}
		if offset+3 > len(payload) {
			return false
		}
		// section_syntax_indicator must be 1 for PAT/PMT.
		// Zero-padding bytes will have this bit clear.
		if payload[offset+1]&0x80 == 0 {
			return true // not a valid section header, treat as padding
		}
		sectionLength := int(payload[offset+1]&0x0F)<<8 | int(payload[offset+2])
		needed := 3 + sectionLength
		if offset+needed > len(payload) {
			return false
		}
		offset += needed
	}
	return true
}

// packetPool manages per-PID accumulators.
type packetPool struct {
	accs       map[uint16]*packetAccumulator
	programMap *programMap
}

func newPacketPool(pm *programMap) *packetPool {
	return &packetPool{
		accs:       make(map[uint16]*packetAccumulator),
		programMap: pm,
	}
}

func (pp *packetPool) add(p *Packet) []*Packet {
	pid := p.Header.PID
	acc, ok := pp.accs[pid]
	if !ok {
		acc = newPacketAccumulator(pid, pp.programMap)
		pp.accs[pid] = acc
	}
	return acc.add(p)
}

func (pp *packetPool) dump() [][]*Packet {
	// Sort by PID so PAT (PID 0) is processed before PMT PIDs.
	pids := make([]int, 0, len(pp.accs))
	for pid := range pp.accs {
		pids = append(pids, int(pid))
	}
	sort.Ints(pids)

	var all [][]*Packet
	for _, pid := range pids {
		if packets := pp.accs[uint16(pid)].flush(); packets != nil {
			all = append(all, packets)
		}
	}
	return all
}
