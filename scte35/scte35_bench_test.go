package scte35

import (
	"encoding/hex"
	"testing"
)

func BenchmarkDecode(b *testing.B) {
	data, _ := hex.DecodeString(goldenVectors["SpliceInsertOut"])
	b.SetBytes(int64(len(data)))

	for b.Loop() {
		DecodeBytes(data)
	}
}

func BenchmarkEncode(b *testing.B) {
	pts := uint64(900000)
	sis := SpliceInfoSection{
		SAPType: 3, Tier: 0xFFF,
		SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
		SpliceDescriptors: SpliceDescriptors{
			&SegmentationDescriptor{
				SegmentationEventID: 1,
				SegmentationTypeID:  SegmentationTypeProviderAdStart,
				SegmentNum:          1,
				SegmentsExpected:    1,
			},
		},
	}

	for b.Loop() {
		sis.Encode()
	}
}
