package scte35

import (
	"encoding/hex"
	"testing"
)

func FuzzDecodeBytes(f *testing.F) {
	// Seed with golden vectors
	for _, hexStr := range goldenVectors {
		data, _ := hex.DecodeString(hexStr)
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		DecodeBytes(data) // must not panic
	})
}
