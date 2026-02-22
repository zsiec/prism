package scte35

import (
	"encoding/hex"
	"testing"
)

// Golden vectors for byte-level verification of encode output.
var goldenVectors = map[string]string{
	"ProviderAdStart":       "fc302700000000000000fff00506fe000dbba00011020f43554549000000017fbf0000300101ee197d02",
	"DistributorAdStart":    "fc302c00000000000000fff00506fe000dbba00016021443554549000000027fff00002932e000003201031233f909",
	"DistributorAdEnd":      "fc302700000000000000fff00506fe000dbba00011020f43554549000000037fbf000033010352b10a71",
	"ProviderAdEnd":         "fc302700000000000000fff00506fe000dbba00011020f43554549000000047fbf0000310101de2663d0",
	"SpliceInsertOut":       "fc303200000000000000fff01005000000057fbf00fe007b98a0000101010011020f43554549000000057fbf00002201017f1add87",
	"SpliceInsertIn":        "fc302d00000000000000fff00b05000000067f1f00000101010011020f43554549000000067fbf0000230101c2262974",
	"ProgramStart":          "fc302700000000000000fff00506fe000dbba00011020f43554549000000077fbf0000100000ded1e682",
	"ContentID":             "fc302700000000000000fff00506fe000dbba00011020f43554549000000087fbf000001000090ab548a",
	"ChapterStart":          "fc302c00000000000000fff00506fe000dbba00016021443554549000000097fff00019bfcc00000200105bb3c1919",
	"ChapterEnd":            "fc302700000000000000fff00506fe000dbba00011020f435545490000000a7fbf0000210105d921d749",
	"NetworkStart":          "fc302700000000000000fff00506fe000dbba00011020f435545490000000b7fbf0000500000163074e3",
	"ProgramEnd":            "fc302700000000000000fff00506fe000dbba00011020f435545490000000c7fbf0000110000e767f265",
	"UnscheduledEventStart": "fc302700000000000000fff00506fe000dbba00011020f435545490000000d7fbf0000400000d6bf6b98",
	"UnscheduledEventEnd":   "fc302700000000000000fff00506fe000dbba00011020f435545490000000e7fbf00004100003b85a241",
	"ProviderPOStart":       "fc302c00000000000000fff00506fe000dbba000160214435545490000000f7fff00005265c0000034010288c9acbd",
	"ProviderPOEnd":         "fc302700000000000000fff00506fe000dbba00011020f43554549000000107fbf000035010213993e41",
}

type testScenario struct {
	name  string
	build func(eventID uint32) SpliceInfoSection
}

var testScenarios = []testScenario{
	{
		name: "ProviderAdStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeProviderAdStart, SegmentNum: 1, SegmentsExpected: 1},
				},
			}
		},
	},
	{
		name: "DistributorAdStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			dur := uint64(30 * 90000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeDistributorAdStart, SegmentationDuration: &dur, SegmentNum: 1, SegmentsExpected: 3},
				},
			}
		},
	},
	{
		name: "DistributorAdEnd",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeDistributorAdEnd, SegmentNum: 1, SegmentsExpected: 3},
				},
			}
		},
	},
	{
		name: "ProviderAdEnd",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeProviderAdEnd, SegmentNum: 1, SegmentsExpected: 1},
				},
			}
		},
	},
	{
		name: "SpliceInsertOut",
		build: func(eventID uint32) SpliceInfoSection {
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &SpliceInsert{
					SpliceEventID: eventID, OutOfNetworkIndicator: true, SpliceImmediateFlag: true,
					BreakDuration:   &BreakDuration{AutoReturn: true, Duration: 90 * 90000},
					UniqueProgramID: 1, AvailNum: 1, AvailsExpected: 1,
				},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeBreakStart, SegmentNum: 1, SegmentsExpected: 1},
				},
			}
		},
	},
	{
		name: "SpliceInsertIn",
		build: func(eventID uint32) SpliceInfoSection {
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &SpliceInsert{
					SpliceEventID: eventID, OutOfNetworkIndicator: false, SpliceImmediateFlag: true,
					UniqueProgramID: 1, AvailNum: 1, AvailsExpected: 1,
				},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeBreakEnd, SegmentNum: 1, SegmentsExpected: 1},
				},
			}
		},
	},
	{
		name: "ProgramStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeProgramStart, SegmentNum: 0, SegmentsExpected: 0},
				},
			}
		},
	},
	{
		name: "ContentID",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeContentIdentification},
				},
			}
		},
	},
	{
		name: "ChapterStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			dur := uint64(300 * 90000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeChapterStart, SegmentationDuration: &dur, SegmentNum: 1, SegmentsExpected: 5},
				},
			}
		},
	},
	{
		name: "ChapterEnd",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeChapterEnd, SegmentNum: 1, SegmentsExpected: 5},
				},
			}
		},
	},
	{
		name: "NetworkStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeNetworkStart},
				},
			}
		},
	},
	{
		name: "ProgramEnd",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeProgramEnd, SegmentNum: 0, SegmentsExpected: 0},
				},
			}
		},
	},
	{
		name: "UnscheduledEventStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeUnscheduledEventStart},
				},
			}
		},
	},
	{
		name: "UnscheduledEventEnd",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeUnscheduledEventEnd},
				},
			}
		},
	},
	{
		name: "ProviderPOStart",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			dur := uint64(60 * 90000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeProviderPOStart, SegmentationDuration: &dur, SegmentNum: 1, SegmentsExpected: 2},
				},
			}
		},
	},
	{
		name: "ProviderPOEnd",
		build: func(eventID uint32) SpliceInfoSection {
			pts := uint64(900000)
			return SpliceInfoSection{
				SAPType: 3, Tier: 0xFFF,
				SpliceCommand: &TimeSignal{SpliceTime: SpliceTime{PTSTime: &pts}},
				SpliceDescriptors: SpliceDescriptors{
					&SegmentationDescriptor{SegmentationEventID: eventID, SegmentationTypeID: SegmentationTypeProviderPOEnd, SegmentNum: 1, SegmentsExpected: 2},
				},
			}
		},
	},
}

func TestGoldenVectors(t *testing.T) {
	t.Parallel()
	for i, tc := range testScenarios {
		eventID := uint32(i + 1)
		sis := tc.build(eventID)
		got, err := sis.Encode()
		if err != nil {
			t.Fatalf("%s: Encode failed: %v", tc.name, err)
		}
		gotHex := hex.EncodeToString(got)
		wantHex := goldenVectors[tc.name]
		if gotHex != wantHex {
			t.Errorf("%s:\n  got  %s\n  want %s", tc.name, gotHex, wantHex)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for i, tc := range testScenarios {
		eventID := uint32(i + 1)
		sis := tc.build(eventID)
		encoded, err := sis.Encode()
		if err != nil {
			t.Fatalf("%s: Encode failed: %v", tc.name, err)
		}

		decoded, err := DecodeBytes(encoded)
		if err != nil {
			t.Fatalf("%s: DecodeBytes failed: %v", tc.name, err)
		}

		if decoded.SAPType != sis.SAPType {
			t.Errorf("%s: SAPType = %d, want %d", tc.name, decoded.SAPType, sis.SAPType)
		}
		if decoded.Tier != sis.Tier {
			t.Errorf("%s: Tier = %d, want %d", tc.name, decoded.Tier, sis.Tier)
		}
		if decoded.SpliceCommand == nil {
			t.Fatalf("%s: SpliceCommand is nil", tc.name)
		}
		if decoded.SpliceCommand.Type() != sis.SpliceCommand.Type() {
			t.Errorf("%s: command type = 0x%02X, want 0x%02X", tc.name, decoded.SpliceCommand.Type(), sis.SpliceCommand.Type())
		}

		switch origCmd := sis.SpliceCommand.(type) {
		case *TimeSignal:
			decCmd, ok := decoded.SpliceCommand.(*TimeSignal)
			if !ok {
				t.Fatalf("%s: command is not TimeSignal", tc.name)
			}
			if origCmd.SpliceTime.PTSTime != nil {
				if decCmd.SpliceTime.PTSTime == nil {
					t.Errorf("%s: PTSTime is nil, want %d", tc.name, *origCmd.SpliceTime.PTSTime)
				} else if *decCmd.SpliceTime.PTSTime != *origCmd.SpliceTime.PTSTime {
					t.Errorf("%s: PTSTime = %d, want %d", tc.name, *decCmd.SpliceTime.PTSTime, *origCmd.SpliceTime.PTSTime)
				}
			}
		case *SpliceInsert:
			decCmd, ok := decoded.SpliceCommand.(*SpliceInsert)
			if !ok {
				t.Fatalf("%s: command is not SpliceInsert", tc.name)
			}
			if decCmd.SpliceEventID != origCmd.SpliceEventID {
				t.Errorf("%s: SpliceEventID = %d, want %d", tc.name, decCmd.SpliceEventID, origCmd.SpliceEventID)
			}
			if decCmd.OutOfNetworkIndicator != origCmd.OutOfNetworkIndicator {
				t.Errorf("%s: OutOfNetworkIndicator = %v, want %v", tc.name, decCmd.OutOfNetworkIndicator, origCmd.OutOfNetworkIndicator)
			}
			if decCmd.SpliceImmediateFlag != origCmd.SpliceImmediateFlag {
				t.Errorf("%s: SpliceImmediateFlag = %v, want %v", tc.name, decCmd.SpliceImmediateFlag, origCmd.SpliceImmediateFlag)
			}
			if origCmd.BreakDuration != nil {
				if decCmd.BreakDuration == nil {
					t.Errorf("%s: BreakDuration is nil", tc.name)
				} else {
					if decCmd.BreakDuration.Duration != origCmd.BreakDuration.Duration {
						t.Errorf("%s: Duration = %d, want %d", tc.name, decCmd.BreakDuration.Duration, origCmd.BreakDuration.Duration)
					}
					if decCmd.BreakDuration.AutoReturn != origCmd.BreakDuration.AutoReturn {
						t.Errorf("%s: AutoReturn = %v, want %v", tc.name, decCmd.BreakDuration.AutoReturn, origCmd.BreakDuration.AutoReturn)
					}
				}
			}
		}

		if len(decoded.SpliceDescriptors) != len(sis.SpliceDescriptors) {
			t.Fatalf("%s: descriptor count = %d, want %d", tc.name, len(decoded.SpliceDescriptors), len(sis.SpliceDescriptors))
		}
		for j, origDesc := range sis.SpliceDescriptors {
			origSD := origDesc.(*SegmentationDescriptor)
			decSD, ok := decoded.SpliceDescriptors[j].(*SegmentationDescriptor)
			if !ok {
				t.Fatalf("%s: descriptor %d is not SegmentationDescriptor", tc.name, j)
			}
			if decSD.SegmentationEventID != origSD.SegmentationEventID {
				t.Errorf("%s: desc EventID = %d, want %d", tc.name, decSD.SegmentationEventID, origSD.SegmentationEventID)
			}
			if decSD.SegmentationTypeID != origSD.SegmentationTypeID {
				t.Errorf("%s: desc TypeID = 0x%02X, want 0x%02X", tc.name, decSD.SegmentationTypeID, origSD.SegmentationTypeID)
			}
			if origSD.SegmentationDuration != nil {
				if decSD.SegmentationDuration == nil {
					t.Errorf("%s: desc Duration is nil, want %d", tc.name, *origSD.SegmentationDuration)
				} else if *decSD.SegmentationDuration != *origSD.SegmentationDuration {
					t.Errorf("%s: desc Duration = %d, want %d", tc.name, *decSD.SegmentationDuration, *origSD.SegmentationDuration)
				}
			}
			if decSD.SegmentNum != origSD.SegmentNum {
				t.Errorf("%s: desc SegmentNum = %d, want %d", tc.name, decSD.SegmentNum, origSD.SegmentNum)
			}
			if decSD.SegmentsExpected != origSD.SegmentsExpected {
				t.Errorf("%s: desc SegmentsExpected = %d, want %d", tc.name, decSD.SegmentsExpected, origSD.SegmentsExpected)
			}
		}
	}
}

func TestDecodeGoldenVectors(t *testing.T) {
	t.Parallel()
	for name, hexStr := range goldenVectors {
		data, err := hex.DecodeString(hexStr)
		if err != nil {
			t.Fatalf("%s: hex decode: %v", name, err)
		}
		sis, err := DecodeBytes(data)
		if err != nil {
			t.Errorf("%s: DecodeBytes failed: %v", name, err)
			continue
		}
		if sis.SpliceCommand == nil {
			t.Errorf("%s: SpliceCommand is nil", name)
		}
	}
}

func TestDecodeCorruptedCRC(t *testing.T) {
	t.Parallel()
	data, _ := hex.DecodeString(goldenVectors["ProviderAdStart"])
	data[10] ^= 0xFF
	_, err := DecodeBytes(data)
	if err == nil {
		t.Error("expected CRC error on corrupted data")
	}
}

func TestDecodeUnknownCommandType(t *testing.T) {
	t.Parallel()
	// Build a minimal section with unknown command type 0xFF.
	w := newBitWriter(20)
	w.putUint32(8, 0xFC)   // table_id
	w.putBit(false)        // section_syntax_indicator
	w.putBit(false)        // private_indicator
	w.putUint32(2, 3)      // sap_type
	w.putUint32(12, 13)    // section_length: everything after this field
	w.putUint32(8, 0)      // protocol_version
	w.putBit(false)        // encrypted_packet
	w.putUint32(6, 0)      // encryption_algorithm
	w.putUint64(33, 0)     // pts_adjustment
	w.putUint32(8, 0)      // cw_index
	w.putUint32(12, 0xFFF) // tier
	w.putUint32(12, 0)     // splice_command_length (0 bytes)
	w.putUint32(8, 0xFF)   // unknown command type
	w.putUint32(16, 0)     // descriptor_loop_length

	// Compute and append CRC
	partial := w.bytes()[:16]
	crc := crc32MPEG2(partial)
	w.putUint32(32, crc)

	sis, err := DecodeBytes(w.bytes())
	if err != nil {
		t.Fatalf("DecodeBytes failed on unknown command: %v", err)
	}
	if sis.SpliceCommand == nil {
		t.Fatal("SpliceCommand is nil")
	}
}

func TestSegmentationDescriptorName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typeID uint32
		want   string
	}{
		{SegmentationTypeProviderAdStart, "Provider Advertisement Start"},
		{SegmentationTypeDistributorAdEnd, "Distributor Advertisement End"},
		{SegmentationTypeBreakStart, "Break Start"},
		{SegmentationTypeProgramStart, "Program Start"},
		{SegmentationTypeNetworkStart, "Network Start"},
		{SegmentationTypeChapterStart, "Chapter Start"},
		{SegmentationTypeUnscheduledEventStart, "Unscheduled Event Start"},
		{SegmentationTypeProviderPOStart, "Provider Placement Opportunity Start"},
		{SegmentationTypeContentIdentification, "Content Identification"},
		{0xFE, "Unknown"},
	}
	for _, tc := range tests {
		sd := &SegmentationDescriptor{SegmentationTypeID: tc.typeID}
		if got := sd.Name(); got != tc.want {
			t.Errorf("Name() for 0x%02X = %q, want %q", tc.typeID, got, tc.want)
		}
	}
}

func TestSpliceNullEncodeDecode(t *testing.T) {
	t.Parallel()
	sis := SpliceInfoSection{
		SAPType:       3,
		Tier:          0xFFF,
		SpliceCommand: &SpliceNull{},
	}
	encoded, err := sis.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	decoded, err := DecodeBytes(encoded)
	if err != nil {
		t.Fatalf("DecodeBytes failed: %v", err)
	}
	if _, ok := decoded.SpliceCommand.(*SpliceNull); !ok {
		t.Errorf("expected SpliceNull, got %T", decoded.SpliceCommand)
	}
}
