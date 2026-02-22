package moq

import (
	"encoding/binary"

	"github.com/zsiec/prism/internal/demux"
)

// AnnexBToAVC1 converts Annex B NALUs (4-byte start code prefixed) to AVC1
// format (4-byte big-endian length prefixed). Each NALU in the input slice
// is expected to start with a 4-byte start code (0x00 0x00 0x00 0x01).
func AnnexBToAVC1(nalus [][]byte) []byte {
	var total int
	for _, nalu := range nalus {
		raw := stripStartCode(nalu)
		total += 4 + len(raw)
	}

	out := make([]byte, 0, total)
	for _, nalu := range nalus {
		raw := stripStartCode(nalu)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(raw)))
		out = append(out, lenBuf[:]...)
		out = append(out, raw...)
	}
	return out
}

// stripStartCode removes a 3-byte or 4-byte Annex B start code prefix.
func stripStartCode(nalu []byte) []byte {
	if len(nalu) >= 4 && nalu[0] == 0 && nalu[1] == 0 && nalu[2] == 0 && nalu[3] == 1 {
		return nalu[4:]
	}
	if len(nalu) >= 3 && nalu[0] == 0 && nalu[1] == 0 && nalu[2] == 1 {
		return nalu[3:]
	}
	return nalu
}

// StripADTS removes the ADTS header from a complete ADTS frame, returning
// the raw AAC payload. Returns the input unchanged if it is not a valid
// ADTS frame.
func StripADTS(data []byte) []byte {
	if len(data) < 7 {
		return data
	}
	if data[0] != 0xFF || (data[1]&0xF0) != 0xF0 {
		return data
	}
	headerSize := 7
	if (data[1] & 0x01) == 0 {
		headerSize = 9
	}
	if len(data) <= headerSize {
		return data
	}
	return data[headerSize:]
}

// BuildAVCDecoderConfig builds an AVCDecoderConfigurationRecord
// (ISO 14496-15 §5.2.4.1.1) from raw SPS and PPS NAL data (without
// start codes). The SPS must include the NAL header byte (0x67).
func BuildAVCDecoderConfig(sps, pps []byte) []byte {
	if len(sps) < 4 || len(pps) == 0 {
		return nil
	}

	buf := make([]byte, 0, 11+len(sps)+len(pps))
	buf = append(buf, 1)      // configurationVersion
	buf = append(buf, sps[1]) // AVCProfileIndication
	buf = append(buf, sps[2]) // profile_compatibility
	buf = append(buf, sps[3]) // AVCLevelIndication
	buf = append(buf, 0xFF)   // lengthSizeMinusOne = 3 | reserved 0xFC
	buf = append(buf, 0xE1)   // numOfSequenceParameterSets = 1 | reserved 0xE0

	// SPS
	buf = append(buf, byte(len(sps)>>8), byte(len(sps)))
	buf = append(buf, sps...)

	// PPS
	buf = append(buf, 1) // numOfPictureParameterSets
	buf = append(buf, byte(len(pps)>>8), byte(len(pps)))
	buf = append(buf, pps...)

	return buf
}

// BuildHEVCDecoderConfig builds an HEVCDecoderConfigurationRecord
// (ISO 14496-15 §8.3.3.1.2) from raw VPS, SPS, and PPS NAL data
// (without start codes). The SPS must include the 2-byte NAL header.
func BuildHEVCDecoderConfig(vps, sps, pps []byte) []byte {
	if len(sps) < 4 || len(pps) == 0 || len(vps) == 0 {
		return nil
	}

	info, err := demux.ParseHEVCSPS(sps)
	if err != nil {
		return nil
	}

	buf := make([]byte, 0, 23+5+len(vps)+5+len(sps)+5+len(pps))

	// Fixed 23-byte header
	buf = append(buf, 1) // configurationVersion

	// general_profile_space(2) + general_tier_flag(1) + general_profile_idc(5)
	ptl := info.TierFlag<<5 | info.ProfileIDC
	buf = append(buf, ptl)

	// general_profile_compatibility_flags (4 bytes)
	var pcf [4]byte
	binary.BigEndian.PutUint32(pcf[:], info.ProfileCompatibilityFlags)
	buf = append(buf, pcf[:]...)

	// general_constraint_indicator_flags (6 bytes)
	for i := 5; i >= 0; i-- {
		buf = append(buf, byte(info.ConstraintIndicatorFlags>>(i*8)))
	}

	// general_level_idc
	buf = append(buf, info.LevelIDC)

	// min_spatial_segmentation_idc (12 bits) with 4 reserved bits = 0xF000
	buf = append(buf, 0xF0, 0x00)

	// parallelismType (2 bits) with 6 reserved bits = 0xFC
	buf = append(buf, 0xFC)

	// chromaFormat (2 bits) with 6 reserved bits = 0xFC
	buf = append(buf, 0xFC)

	// bitDepthLumaMinus8 (3 bits) with 5 reserved bits = 0xF8
	buf = append(buf, 0xF8)

	// bitDepthChromaMinus8 (3 bits) with 5 reserved bits = 0xF8
	buf = append(buf, 0xF8)

	// avgFrameRate (16 bits)
	buf = append(buf, 0x00, 0x00)

	// constantFrameRate(2) + numTemporalLayers(3) + temporalIdNested(1) + lengthSizeMinusOne(2)
	// = 0b00_001_1_11 = 0x0F (1 temporal layer, nested, 4-byte NALU lengths)
	buf = append(buf, 0x0F)

	// numOfArrays = 3 (VPS, SPS, PPS)
	buf = append(buf, 3)

	// VPS array (NAL type 32)
	buf = append(buf, 0x20)       // array_completeness(0) | reserved(0) | NAL_unit_type(32)
	buf = append(buf, 0x00, 0x01) // numNalus = 1
	buf = append(buf, byte(len(vps)>>8), byte(len(vps)))
	buf = append(buf, vps...)

	// SPS array (NAL type 33)
	buf = append(buf, 0x21) // NAL_unit_type = 33
	buf = append(buf, 0x00, 0x01)
	buf = append(buf, byte(len(sps)>>8), byte(len(sps)))
	buf = append(buf, sps...)

	// PPS array (NAL type 34)
	buf = append(buf, 0x22) // NAL_unit_type = 34
	buf = append(buf, 0x00, 0x01)
	buf = append(buf, byte(len(pps)>>8), byte(len(pps)))
	buf = append(buf, pps...)

	return buf
}
