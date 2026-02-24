package main

import (
	"os"
	"testing"

	"github.com/zsiec/prism/test/tools/tsutil"
)

func TestDecodePTSRoundTrip(t *testing.T) {
	tests := []int64{0, 133500, 90000, 10929750, 1<<33 - 1}
	for _, want := range tests {
		b := [5]byte{0x20, 0, 1, 0, 1} // prefix nibble 0010
		encodePTS(b[:], want)
		got := decodePTS(b[:])
		if got != want {
			t.Errorf("PTS round-trip %d: got %d", want, got)
		}
	}
}

func TestEncodePTSPreservesPrefix(t *testing.T) {
	for _, prefix := range []byte{0x20, 0x30, 0x10} {
		b := [5]byte{prefix | 0x01, 0, 1, 0, 1}
		encodePTS(b[:], 90000)
		if b[0]&0xF0 != prefix {
			t.Errorf("prefix 0x%02X changed to 0x%02X", prefix, b[0]&0xF0)
		}
	}
}

func TestDecodePCRRoundTrip(t *testing.T) {
	tests := []int64{0, 90000, 45000, 1<<33 - 1}
	for _, want := range tests {
		b := [6]byte{0, 0, 0, 0, 0x7E, 0x00}
		encodePCR(b[:], want)
		got := decodePCR(b[:])
		if got != want {
			t.Errorf("PCR round-trip %d: got %d", want, got)
		}
	}
}

func TestPCRPreservesExtension(t *testing.T) {
	b := [6]byte{0, 0, 0, 0, 0x7F, 0xFF} // ext = 0x1FF = 511
	encodePCR(b[:], 45000)
	ext := uint16(b[4]&0x01)<<8 | uint16(b[5])
	if ext != 511 {
		t.Errorf("PCR extension: got %d, want 511", ext)
	}
}

func TestScanTimestampsRealFile(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../harness/BigBuckBunny_256x144-24fps.ts")
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}
	if len(data) < tsutil.TSPacketSize || data[0] != 0x47 {
		t.Skip("not a valid TS file")
	}

	entries, firstPTS, lastPTS := scanTimestamps(data)
	if len(entries) == 0 {
		t.Fatal("no timestamp entries found")
	}
	if firstPTS < 0 {
		t.Fatal("firstPTS not found")
	}
	if lastPTS <= firstPTS {
		t.Errorf("lastPTS (%d) <= firstPTS (%d)", lastPTS, firstPTS)
	}

	durationSec := float64(lastPTS-firstPTS) / 90000
	t.Logf("found %d timestamp locations, PTS range: %d..%d (%.1fs)",
		len(entries), firstPTS, lastPTS, durationSec)
}

func TestAddTimestampOffset(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../harness/BigBuckBunny_256x144-24fps.ts")
	if err != nil {
		t.Skipf("test file not available: %v", err)
	}

	// Make a copy so we don't mutate the file for other tests.
	buf := make([]byte, len(data))
	copy(buf, data)

	entries, firstPTS, lastPTS := scanTimestamps(buf)
	if len(entries) == 0 {
		t.Skip("no timestamps found")
	}

	delta := lastPTS - firstPTS + 3750

	addTimestampOffset(buf, entries, delta)

	// Re-scan and verify PTS shifted.
	_, newFirst, _ := scanTimestamps(buf)
	if newFirst != firstPTS+delta {
		t.Errorf("after offset: firstPTS = %d, want %d", newFirst, firstPTS+delta)
	}

	// Restore and verify.
	addTimestampOffset(buf, entries, -delta)
	_, restored, _ := scanTimestamps(buf)
	if restored != firstPTS {
		t.Errorf("after restore: firstPTS = %d, want %d", restored, firstPTS)
	}
}
