package demux

import "testing"

func BenchmarkParseADTS(b *testing.B) {
	// Valid ADTS frame: 7-byte header + 6 bytes payload
	header := []byte{0xFF, 0xF1, 0x4C, 0x80, 0x01, 0xA0, 0xFC}
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	data := append(header, payload...)

	b.SetBytes(int64(len(data)))
	for b.Loop() {
		ParseADTS(data)
	}
}
