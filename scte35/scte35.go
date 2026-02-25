// Package scte35 implements encoding and decoding of SCTE-35 splice information
// sections per the ANSI/SCTE 35 specification. Only the command types and
// descriptor types used by this project are supported: SpliceNull, SpliceInsert,
// TimeSignal, and SegmentationDescriptor.
package scte35

import "fmt"

const (
	tableID = 0xFC

	SpliceNullType   uint32 = 0x00
	SpliceInsertType uint32 = 0x05
	TimeSignalType   uint32 = 0x06
)

// SpliceCommand is the interface for splice command types.
type SpliceCommand interface {
	Type() uint32
	decode([]byte) error
	encode() ([]byte, error)
	commandLength() int
}

// SpliceDescriptor is the interface for splice descriptor types.
type SpliceDescriptor interface {
	Tag() uint32
	decode([]byte) error
	encode() ([]byte, error)
	descriptorLength() int
}

// SpliceDescriptors is a slice of SpliceDescriptor.
type SpliceDescriptors []SpliceDescriptor

// SpliceTime carries an optional PTS time.
type SpliceTime struct {
	PTSTime *uint64
}

// BreakDuration specifies the duration of a commercial break.
type BreakDuration struct {
	AutoReturn bool
	Duration   uint64
}

// SpliceInfoSection is the top-level SCTE-35 structure.
type SpliceInfoSection struct {
	SAPType           uint32
	PTSAdjustment     uint64
	Tier              uint32
	SpliceCommand     SpliceCommand
	SpliceDescriptors SpliceDescriptors
}

// DecodeBytes decodes a binary SCTE-35 splice_info_section.
func DecodeBytes(data []byte) (*SpliceInfoSection, error) {
	sis := &SpliceInfoSection{}
	if err := sis.decode(data); err != nil {
		return sis, err
	}
	return sis, nil
}

func (sis *SpliceInfoSection) decode(data []byte) error {
	if err := verifyCRC32(data); err != nil {
		return err
	}

	r := newBitReader(data)
	r.skip(8) // table_id
	r.skip(1) // section_syntax_indicator
	r.skip(1) // private_indicator
	sis.SAPType = r.readUint32(2)
	sectionLength := int(r.readUint32(12))

	r.skip(8) // protocol_version
	r.skip(1) // encrypted_packet
	r.skip(6) // encryption_algorithm
	sis.PTSAdjustment = r.readUint64(33)
	r.skip(8) // cw_index
	sis.Tier = r.readUint32(12)

	spliceCommandLength := int(r.readUint32(12))
	spliceCommandType := r.readUint32(8)

	if spliceCommandLength == 0xFFF {
		// Legacy: compute from section_length.
		// section_length covers everything after the 3-byte header prefix through CRC.
		// Already consumed: protocol(1) + encrypted+algo(1) + ptsAdj(5 bytes=33+7 bits, but 33bits -> 4.125 bytes)
		// Actually: after sectionLength field, we've consumed 11 bytes (88 bits of the
		// fixed header fields) plus splice_command_length(12) + command_type(8) = 20 more bits.
		// Compute remaining for command: section_length - header_bytes - descriptor_loop - crc
		// This is complex; for legacy, read until remaining matches descriptor_loop + crc.
		// Simplified: consume remaining section bytes minus what we need for descriptors+crc.
		// Legacy: splice_command_length=0xFFF. Decode the command to discover
		// its length, then parse descriptors from the remaining bytes.
		remaining := sectionLength - 11 // bytes after fixed header fields, before CRC
		allRemaining := r.readBytes(remaining - 4)
		cmd, err := decodeSpliceCommand(spliceCommandType, allRemaining)
		if err != nil {
			return fmt.Errorf("scte35: decoding command type 0x%02X: %w", spliceCommandType, err)
		}
		sis.SpliceCommand = cmd
		cmdLen := cmd.commandLength()
		if cmdLen < len(allRemaining)-2 {
			descData := allRemaining[cmdLen+2:] // skip descriptor_loop_length
			descLoopLen := int(allRemaining[cmdLen])<<8 | int(allRemaining[cmdLen+1])
			if descLoopLen > 0 && descLoopLen <= len(descData) {
				descs, derr := decodeSpliceDescriptors(descData[:descLoopLen])
				if derr != nil {
					return derr
				}
				sis.SpliceDescriptors = descs
			}
		}
	} else {
		cmdData := r.readBytes(spliceCommandLength)
		cmd, err := decodeSpliceCommand(spliceCommandType, cmdData)
		if err != nil {
			return fmt.Errorf("scte35: decoding command type 0x%02X: %w", spliceCommandType, err)
		}
		sis.SpliceCommand = cmd

		descriptorLoopLength := int(r.readUint32(16))
		if descriptorLoopLength > 0 {
			descData := r.readBytes(descriptorLoopLength)
			descs, derr := decodeSpliceDescriptors(descData)
			if derr != nil {
				return derr
			}
			sis.SpliceDescriptors = descs
		}
	}

	return nil
}

// Encode serializes the SpliceInfoSection to binary.
func (sis *SpliceInfoSection) Encode() ([]byte, error) {
	sectionLen := sis.sectionLength()
	totalLen := 3 + sectionLen // table_id(1) + flags+sap+section_length(2) + section data

	w := newBitWriter(totalLen)

	// Header
	w.putUint32(8, tableID)
	w.putBit(false) // section_syntax_indicator
	w.putBit(false) // private_indicator
	w.putUint32(2, sis.SAPType)
	w.putUint32(12, uint32(sectionLen))

	// Fixed fields
	w.putUint32(8, 0) // protocol_version
	w.putBit(false)   // encrypted_packet
	w.putUint32(6, 0) // encryption_algorithm
	w.putUint64(33, sis.PTSAdjustment)
	w.putUint32(8, 0) // cw_index
	w.putUint32(12, sis.Tier)

	// Splice command
	if sis.SpliceCommand != nil {
		w.putUint32(12, uint32(sis.SpliceCommand.commandLength()))
		w.putUint32(8, sis.SpliceCommand.Type())
		cmdBytes, err := sis.SpliceCommand.encode()
		if err != nil {
			return nil, err
		}
		w.putBytes(cmdBytes)
	} else {
		w.putUint32(12, 0)
		w.putUint32(8, SpliceNullType)
	}

	// Descriptors
	descLoopLen := sis.descriptorLoopLength()
	w.putUint32(16, uint32(descLoopLen))
	for _, desc := range sis.SpliceDescriptors {
		descBytes, err := desc.encode()
		if err != nil {
			return nil, err
		}
		w.putBytes(descBytes)
	}

	// CRC32 — compute over everything written so far
	crc := crc32MPEG2(w.bytes()[:totalLen-4])
	w.putUint32(32, crc)

	return w.bytes(), nil
}

func (sis *SpliceInfoSection) sectionLength() int {
	bits := 8  // protocol_version
	bits += 1  // encrypted_packet
	bits += 6  // encryption_algorithm
	bits += 33 // pts_adjustment
	bits += 8  // cw_index
	bits += 12 // tier
	bits += 12 // splice_command_length
	bits += 8  // splice_command_type

	if sis.SpliceCommand != nil {
		bits += sis.SpliceCommand.commandLength() * 8
	}

	bits += 16 // descriptor_loop_length
	bits += sis.descriptorLoopLength() * 8
	bits += 32 // CRC_32

	return bits / 8
}

func (sis *SpliceInfoSection) descriptorLoopLength() int {
	length := 0
	for _, d := range sis.SpliceDescriptors {
		length += 2 + d.descriptorLength() // tag(1) + length(1) + content
	}
	return length
}

func decodeSpliceCommand(cmdType uint32, data []byte) (SpliceCommand, error) {
	var cmd SpliceCommand
	switch cmdType {
	case SpliceNullType:
		cmd = &SpliceNull{}
	case SpliceInsertType:
		cmd = &SpliceInsert{}
	case TimeSignalType:
		cmd = &TimeSignal{}
	default:
		// Unknown command — return a null-like command so we don't panic.
		cmd = &SpliceNull{}
		return cmd, nil
	}
	if err := cmd.decode(data); err != nil {
		return cmd, err
	}
	return cmd, nil
}

func decodeSpliceDescriptors(data []byte) ([]SpliceDescriptor, error) {
	var descs []SpliceDescriptor
	offset := 0
	for offset < len(data) {
		if offset+2 > len(data) {
			break
		}
		tag := uint32(data[offset])
		length := int(data[offset+1])
		end := offset + 2 + length
		if end > len(data) {
			break
		}

		// Check identifier (bytes 2-5 of the descriptor body).
		if length >= 4 {
			identifier := uint32(data[offset+2])<<24 | uint32(data[offset+3])<<16 |
				uint32(data[offset+4])<<8 | uint32(data[offset+5])
			if tag == SegmentationDescriptorTag && identifier == CUEIdentifier {
				sd := &SegmentationDescriptor{}
				if err := sd.decode(data[offset:end]); err != nil {
					return descs, err
				}
				descs = append(descs, sd)
			}
			// Skip unknown descriptor tags/identifiers silently.
		}
		offset = end
	}
	return descs, nil
}
