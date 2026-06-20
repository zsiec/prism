package moqclient

import "testing"

// parseExts decodes the LOC extension list on every received media object, so it
// runs on attacker-influenced bytes the moment a stream is impaired or malformed.
// It must never panic / loop forever on any input — only return what it could
// parse. (See moq/fuzz_test.go for why codec-level fuzzing is the right layer for
// MoQ given QUIC encryption.)
func FuzzParseExts(f *testing.F) {
	// id 2 (capture timestamp, even -> varint) + id 4 (frame marking, keyframe bit)
	f.Add([]byte{0x02, 0x40, 0x64, 0x04, 0x20})
	// id 13 (odd -> length-prefixed video config)
	f.Add([]byte{0x0d, 0x03, 0x01, 0x02, 0x03})
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff})
	f.Fuzz(func(t *testing.T, exts []byte) { _ = parseExts(exts) })
}
