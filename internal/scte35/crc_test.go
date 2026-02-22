package scte35

import (
	"encoding/hex"
	"testing"
)

func TestCRC32MPEG2KnownVector(t *testing.T) {
	t.Parallel()
	// "123456789" is a standard test vector for CRC algorithms.
	data := []byte("123456789")
	got := crc32MPEG2(data)
	want := uint32(0x0376E6E7)
	if got != want {
		t.Errorf("crc32MPEG2(%q) = 0x%08X, want 0x%08X", data, got, want)
	}
}

func TestCRC32MPEG2RoundTrip(t *testing.T) {
	t.Parallel()
	data := []byte{0xFC, 0x30, 0x27, 0x00, 0x00, 0x00, 0x00, 0x00}
	crc := crc32MPEG2(data)
	full := make([]byte, len(data)+4)
	copy(full, data)
	full[len(data)] = byte(crc >> 24)
	full[len(data)+1] = byte(crc >> 16)
	full[len(data)+2] = byte(crc >> 8)
	full[len(data)+3] = byte(crc)
	if err := verifyCRC32(full); err != nil {
		t.Errorf("verifyCRC32 failed on round-trip data: %v", err)
	}
}

func TestVerifyCRC32GoldenVector(t *testing.T) {
	t.Parallel()
	// Use the first golden vector from the test suite — a complete SCTE-35 section.
	data, _ := hex.DecodeString("fc302700000000000000fff00506fe000dbba00011020f43554549000000017fbf0000300101ee197d02")
	if err := verifyCRC32(data); err != nil {
		t.Errorf("verifyCRC32 failed on golden vector: %v", err)
	}
}

func TestVerifyCRC32Corrupted(t *testing.T) {
	t.Parallel()
	data, _ := hex.DecodeString("fc302700000000000000fff00506fe000dbba00011020f43554549000000017fbf0000300101ee197d02")
	data[10] ^= 0xFF // corrupt a byte
	if err := verifyCRC32(data); err == nil {
		t.Error("expected CRC error on corrupted data")
	}
}

func TestVerifyCRC32TooShort(t *testing.T) {
	t.Parallel()
	if err := verifyCRC32([]byte{0x01, 0x02}); err == nil {
		t.Error("expected error on short data")
	}
}
