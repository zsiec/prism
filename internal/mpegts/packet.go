package mpegts

import "fmt"

const (
	packetSize = 188
	syncByte   = 0x47
)

func parsePacket(buf []byte) (*Packet, error) {
	if len(buf) != packetSize {
		return nil, fmt.Errorf("mpegts: packet size %d, expected %d", len(buf), packetSize)
	}
	if buf[0] != syncByte {
		return nil, fmt.Errorf("mpegts: invalid sync byte 0x%02X", buf[0])
	}

	p := &Packet{}
	p.Header.TransportErrorIndicator = buf[1]&0x80 != 0
	p.Header.PayloadUnitStartIndicator = buf[1]&0x40 != 0
	p.Header.PID = uint16(buf[1]&0x1F)<<8 | uint16(buf[2])
	p.Header.HasAdaptationField = buf[3]&0x20 != 0
	p.Header.HasPayload = buf[3]&0x10 != 0
	p.Header.ContinuityCounter = buf[3] & 0x0F

	offset := 4

	if p.Header.HasAdaptationField {
		if offset >= packetSize {
			return p, nil
		}
		afLen := int(buf[offset])
		if afLen > 0 && offset+1 < packetSize {
			p.Header.DiscontinuityIndicator = buf[offset+1]&0x80 != 0
		}
		offset += 1 + afLen
		if offset > packetSize {
			offset = packetSize
		}
	}

	if p.Header.HasPayload && offset < packetSize {
		p.Payload = make([]byte, packetSize-offset)
		copy(p.Payload, buf[offset:])
	}

	return p, nil
}
