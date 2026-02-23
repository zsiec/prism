package main

import (
	"fmt"
	"os"

	"github.com/zsiec/prism/internal/scte35"
)

const tsPacketSize = 188

type scte35Scenario struct {
	label       string
	commandType string
	build       func(eventID uint32) scte35.SpliceInfoSection
}

var adBreakScenarios = []scte35Scenario{
	{
		label:       "Provider Ad Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeProviderAdStart,
						SegmentNum:          1,
						SegmentsExpected:    1,
					},
				},
			}
		},
	},
	{
		label:       "Distributor Ad Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			dur := uint64(30 * 90000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID:  eventID,
						SegmentationTypeID:   scte35.SegmentationTypeDistributorAdStart,
						SegmentationDuration: &dur,
						SegmentNum:           1,
						SegmentsExpected:     3,
					},
				},
			}
		},
	},
	{
		label:       "Distributor Ad End",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeDistributorAdEnd,
						SegmentNum:          1,
						SegmentsExpected:    3,
					},
				},
			}
		},
	},
	{
		label:       "Provider Ad End",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeProviderAdEnd,
						SegmentNum:          1,
						SegmentsExpected:    1,
					},
				},
			}
		},
	},
	{
		label:       "Break Start (splice_insert out)",
		commandType: "splice_insert",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.SpliceInsert{
					SpliceEventID:         eventID,
					OutOfNetworkIndicator: true,
					SpliceImmediateFlag:   true,
					BreakDuration: &scte35.BreakDuration{
						AutoReturn: true,
						Duration:   90 * 90000,
					},
					UniqueProgramID: 1,
					AvailNum:        1,
					AvailsExpected:  1,
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeBreakStart,
						SegmentNum:          1,
						SegmentsExpected:    1,
					},
				},
			}
		},
	},
	{
		label:       "Break End (splice_insert in)",
		commandType: "splice_insert",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.SpliceInsert{
					SpliceEventID:         eventID,
					OutOfNetworkIndicator: false,
					SpliceImmediateFlag:   true,
					UniqueProgramID:       1,
					AvailNum:              1,
					AvailsExpected:        1,
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeBreakEnd,
						SegmentNum:          1,
						SegmentsExpected:    1,
					},
				},
			}
		},
	},
	{
		label:       "Program Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeProgramStart,
						SegmentNum:          0,
						SegmentsExpected:    0,
					},
				},
			}
		},
	},
	{
		label:       "Content Identification",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeContentIdentification,
					},
				},
			}
		},
	},
	{
		label:       "Chapter Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			dur := uint64(300 * 90000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID:  eventID,
						SegmentationTypeID:   scte35.SegmentationTypeChapterStart,
						SegmentationDuration: &dur,
						SegmentNum:           1,
						SegmentsExpected:     5,
					},
				},
			}
		},
	},
	{
		label:       "Chapter End",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeChapterEnd,
						SegmentNum:          1,
						SegmentsExpected:    5,
					},
				},
			}
		},
	},
	{
		label:       "Network Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeNetworkStart,
					},
				},
			}
		},
	},
	{
		label:       "Program End",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeProgramEnd,
						SegmentNum:          0,
						SegmentsExpected:    0,
					},
				},
			}
		},
	},
	{
		label:       "Unscheduled Event Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeUnscheduledEventStart,
					},
				},
			}
		},
	},
	{
		label:       "Unscheduled Event End",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeUnscheduledEventEnd,
					},
				},
			}
		},
	},
	{
		label:       "Provider Placement Opportunity Start",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			dur := uint64(60 * 90000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID:  eventID,
						SegmentationTypeID:   scte35.SegmentationTypeProviderPOStart,
						SegmentationDuration: &dur,
						SegmentNum:           1,
						SegmentsExpected:     2,
					},
				},
			}
		},
	},
	{
		label:       "Provider Placement Opportunity End",
		commandType: "time_signal",
		build: func(eventID uint32) scte35.SpliceInfoSection {
			pts := uint64(900000)
			return scte35.SpliceInfoSection{
				SAPType: 3,
				Tier:    0xFFF,
				SpliceCommand: &scte35.TimeSignal{
					SpliceTime: scte35.SpliceTime{PTSTime: &pts},
				},
				SpliceDescriptors: scte35.SpliceDescriptors{
					&scte35.SegmentationDescriptor{
						SegmentationEventID: eventID,
						SegmentationTypeID:  scte35.SegmentationTypeProviderPOEnd,
						SegmentNum:          1,
						SegmentsExpected:    2,
					},
				},
			}
		},
	},
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: inject-scte35 <input.ts> <output.ts> [interval_seconds]\n")
		os.Exit(1)
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]
	intervalSec := 8.0
	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%f", &intervalSec)
	}

	input, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read input: %v\n", err)
		os.Exit(1)
	}

	if len(input)%tsPacketSize != 0 {
		fmt.Fprintf(os.Stderr, "Input file size is not a multiple of %d bytes\n", tsPacketSize)
		os.Exit(1)
	}

	scte35PID := uint16(500)
	pmtPID := findPMTPID(input)
	if pmtPID == 0 {
		fmt.Fprintf(os.Stderr, "Could not find PMT PID\n")
		os.Exit(1)
	}

	fmt.Printf("PMT PID: 0x%04X, SCTE-35 PID: 0x%04X (%d), interval: %.0fs\n", pmtPID, scte35PID, scte35PID, intervalSec)
	fmt.Printf("Scenarios: %d different SCTE-35 event types\n", len(adBreakScenarios))

	pcrBase := int64(0)
	intervalTicks := int64(intervalSec * 90000)
	lastInsertPCR := int64(-intervalTicks)
	scenarioIdx := 0
	eventID := uint32(1)
	cc := byte(0)
	injected := 0

	var output []byte
	for i := 0; i < len(input); i += tsPacketSize {
		pkt := input[i : i+tsPacketSize]

		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])

		if pid == pmtPID && !hasSCTE35InPMT(pkt, scte35PID) {
			modified := addSCTE35ToPMT(pkt, scte35PID)
			output = append(output, modified...)
			continue
		}

		if hasPCR(pkt) {
			pcrBase = extractPCR(pkt)
		}

		output = append(output, pkt...)

		if pcrBase > 0 && pcrBase-lastInsertPCR >= intervalTicks {
			lastInsertPCR = pcrBase

			scenario := adBreakScenarios[scenarioIdx%len(adBreakScenarios)]
			sis := scenario.build(eventID)

			payload, err := sis.Encode()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to encode SCTE-35 (%s): %v\n", scenario.label, err)
				os.Exit(1)
			}

			tsPkt := wrapInTSPacket(scte35PID, cc, payload)
			cc = (cc + 1) & 0x0F
			output = append(output, tsPkt...)
			injected++

			fmt.Printf("  [%2d] %-45s cmd=%-12s eventID=%d  PCR=%.2fs\n",
				injected, scenario.label, scenario.commandType, eventID, float64(pcrBase)/90000.0)

			eventID++
			scenarioIdx++
		}
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Done: %s (%d packets, %d SCTE-35 events)\n", outputPath, len(output)/tsPacketSize, injected)
}

func wrapInTSPacket(pid uint16, cc byte, payload []byte) []byte {
	section := make([]byte, 0, len(payload)+1)
	section = append(section, 0x00)
	section = append(section, payload...)

	pkt := make([]byte, tsPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x40 | byte(pid>>8)
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F)

	if len(section) < tsPacketSize-4 {
		copy(pkt[4:], section)
		for i := 4 + len(section); i < tsPacketSize; i++ {
			pkt[i] = 0xFF
		}
	} else {
		copy(pkt[4:], section[:tsPacketSize-4])
	}

	return pkt
}

func findPMTPID(data []byte) uint16 {
	for i := 0; i < len(data); i += tsPacketSize {
		pkt := data[i : i+tsPacketSize]
		if pkt[0] != 0x47 {
			continue
		}
		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
		if pid != 0 {
			continue
		}
		if pkt[1]&0x40 == 0 {
			continue
		}
		offset := 4
		if pkt[3]&0x20 != 0 {
			offset += 1 + int(pkt[4])
		}
		if offset >= tsPacketSize || pkt[3]&0x10 == 0 {
			continue
		}
		pointer := pkt[offset]
		offset += 1 + int(pointer)
		if offset+12 > tsPacketSize {
			continue
		}
		if pkt[offset] != 0x00 {
			continue
		}
		sectionLen := int(pkt[offset+1]&0x0F)<<8 | int(pkt[offset+2])
		offset += 8
		remaining := sectionLen - 5 - 4
		for remaining >= 4 && offset+4 <= tsPacketSize {
			progNum := uint16(pkt[offset])<<8 | uint16(pkt[offset+1])
			pmtPID := (uint16(pkt[offset+2]&0x1F) << 8) | uint16(pkt[offset+3])
			if progNum != 0 {
				return pmtPID
			}
			offset += 4
			remaining -= 4
		}
	}
	return 0
}

func hasSCTE35InPMT(pkt []byte, scte35PID uint16) bool {
	offset := 4
	if pkt[3]&0x20 != 0 {
		offset += 1 + int(pkt[4])
	}
	if offset >= tsPacketSize || pkt[3]&0x10 == 0 {
		return false
	}
	pointer := pkt[offset]
	offset += 1 + int(pointer)
	if offset+4 > tsPacketSize || pkt[offset] != 0x02 {
		return false
	}
	sectionLen := int(pkt[offset+1]&0x0F)<<8 | int(pkt[offset+2])
	end := offset + 3 + sectionLen
	if end > tsPacketSize {
		end = tsPacketSize
	}
	progInfoLen := int(pkt[offset+10]&0x0F)<<8 | int(pkt[offset+11])
	pos := offset + 12 + progInfoLen
	for pos+5 <= end-4 {
		esPID := (uint16(pkt[pos+1]&0x1F) << 8) | uint16(pkt[pos+2])
		if esPID == scte35PID {
			return true
		}
		esInfoLen := int(pkt[pos+3]&0x0F)<<8 | int(pkt[pos+4])
		pos += 5 + esInfoLen
	}
	return false
}

func addSCTE35ToPMT(pkt []byte, scte35PID uint16) []byte {
	out := make([]byte, tsPacketSize)
	copy(out, pkt)

	offset := 4
	if out[3]&0x20 != 0 {
		offset += 1 + int(out[4])
	}
	if offset >= tsPacketSize || out[3]&0x10 == 0 {
		return pkt
	}
	pointer := out[offset]
	pmtStart := offset + 1 + int(pointer)
	if pmtStart+4 > tsPacketSize || out[pmtStart] != 0x02 {
		return pkt
	}

	sectionLen := int(out[pmtStart+1]&0x0F)<<8 | int(out[pmtStart+2])
	sectionEnd := pmtStart + 3 + sectionLen
	if sectionEnd > tsPacketSize || sectionEnd < 4 {
		return pkt
	}

	crcPos := sectionEnd - 4

	entry := []byte{
		0x86,
		byte(scte35PID>>8) | 0xE0,
		byte(scte35PID & 0xFF),
		0xF0, 0x00,
	}

	newSection := make([]byte, 0, tsPacketSize)
	newSection = append(newSection, out[pmtStart:crcPos]...)
	newSection = append(newSection, entry...)

	newSectionLen := len(newSection) - 3 + 4
	newSection[1] = (newSection[1] & 0xF0) | byte(newSectionLen>>8)
	newSection[2] = byte(newSectionLen & 0xFF)

	crc := crc32MPEG2(newSection)
	newSection = append(newSection, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))

	result := make([]byte, tsPacketSize)
	copy(result, out[:pmtStart])
	copy(result[pmtStart:], newSection)
	for i := pmtStart + len(newSection); i < tsPacketSize; i++ {
		result[i] = 0xFF
	}

	return result
}

func hasPCR(pkt []byte) bool {
	if pkt[3]&0x20 == 0 {
		return false
	}
	afLen := pkt[4]
	if afLen < 7 {
		return false
	}
	return pkt[5]&0x10 != 0
}

func extractPCR(pkt []byte) int64 {
	base := int64(pkt[6])<<25 | int64(pkt[7])<<17 | int64(pkt[8])<<9 | int64(pkt[9])<<1 | int64(pkt[10]>>7)
	return base
}

func crc32MPEG2(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
