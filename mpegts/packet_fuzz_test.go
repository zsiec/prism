package mpegts

import "testing"

func FuzzParsePacket(f *testing.F) {
	// Seed: valid 188-byte TS packet (sync byte 0x47)
	pkt := make([]byte, 188)
	pkt[0] = 0x47 // sync byte
	pkt[1] = 0x40 // PUSI=1, PID=0
	pkt[2] = 0x00
	pkt[3] = 0x10 // no adaptation, has payload
	f.Add(pkt)

	// Seed: packet with adaptation field
	afPkt := make([]byte, 188)
	afPkt[0] = 0x47
	afPkt[1] = 0x01 // PID high bits
	afPkt[2] = 0x00 // PID low bits
	afPkt[3] = 0x30 // adaptation + payload
	afPkt[4] = 0x07 // adaptation field length
	f.Add(afPkt)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) != 188 {
			return
		}
		parsePacket(data) // must not panic
	})
}
