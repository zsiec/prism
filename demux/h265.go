package demux

import (
	"fmt"
	"math/bits"
)

// H.265/HEVC NAL unit type constants as defined in ITU-T H.265 Table 7-1.
const (
	HEVCNALBlaWLP     = 16
	HEVCNALIDRWRadl   = 19
	HEVCNALIDRNlp     = 20
	HEVCNALCraNut     = 21
	HEVCNALVPS        = 32
	HEVCNALSPS        = 33
	HEVCNALPPS        = 34
	HEVCNALAUD        = 35
	HEVCNALFillerData = 38
	HEVCNALSEIPrefix  = 39
)

// HEVCNALType extracts the NAL unit type from the first byte of an HEVC
// 2-byte NAL header: forbidden(1) | type(6) | layerID_high(1).
func HEVCNALType(firstByte byte) byte {
	return (firstByte >> 1) & 0x3F
}

// IsHEVCKeyframe returns true if the NAL type represents an HEVC random access
// point (BLA, IDR, or CRA).
func IsHEVCKeyframe(nalType byte) bool {
	return nalType >= HEVCNALBlaWLP && nalType <= HEVCNALCraNut
}

// IsHEVCVPS returns true if the NAL type is a Video Parameter Set.
func IsHEVCVPS(nalType byte) bool { return nalType == HEVCNALVPS }

// IsHEVCSPS returns true if the NAL type is a Sequence Parameter Set.
func IsHEVCSPS(nalType byte) bool { return nalType == HEVCNALSPS }

// IsHEVCPPS returns true if the NAL type is a Picture Parameter Set.
func IsHEVCPPS(nalType byte) bool { return nalType == HEVCNALPPS }

// ParseAnnexBHEVC parses an Annex B byte stream into NAL units using the
// HEVC 2-byte NAL header for type extraction. Start codes are identical
// to H.264 (00 00 01 or 00 00 00 01).
func ParseAnnexBHEVC(data []byte) []NALUnit {
	return parseAnnexBGeneric(data, 2, func(d []byte) byte { return HEVCNALType(d[0]) })
}

// HEVCSPSInfo holds parameters extracted from an HEVC SPS NAL unit.
type HEVCSPSInfo struct {
	Width      int
	Height     int
	ProfileIDC byte
	TierFlag   byte
	LevelIDC   byte

	ProfileCompatibilityFlags uint32
	ConstraintIndicatorFlags  uint64

	ChromaFormatIdc      byte
	BitDepthLumaMinus8   byte
	BitDepthChromaMinus8 byte
}

// CodecString returns the RFC 6381 codec parameter string (e.g.
// "hev1.1.6.L93.B0") for use in WebCodecs configuration and MIME types.
func (s HEVCSPSInfo) CodecString() string {
	tier := "L"
	if s.TierFlag == 1 {
		tier = "H"
	}

	reversed := bits.Reverse32(s.ProfileCompatibilityFlags)

	// Build constraint bytes (6 bytes from the 48-bit field), trim trailing zeros
	var constraintBytes [6]byte
	for i := 0; i < 6; i++ {
		constraintBytes[i] = byte((s.ConstraintIndicatorFlags >> uint((5-i)*8)) & 0xFF)
	}
	lastNonZero := -1
	for i := 5; i >= 0; i-- {
		if constraintBytes[i] != 0 {
			lastNonZero = i
			break
		}
	}

	codec := fmt.Sprintf("hev1.%d.%X.%s%d", s.ProfileIDC, reversed, tier, s.LevelIDC)
	if lastNonZero >= 0 {
		for i := 0; i <= lastNonZero; i++ {
			codec += fmt.Sprintf(".%X", constraintBytes[i])
		}
	}
	return codec
}

// ParseHEVCSPS parses an HEVC SPS NAL unit to extract resolution and
// profile/tier/level. The input should be the raw NAL data including
// the 2-byte NAL header.
func ParseHEVCSPS(nalu []byte) (HEVCSPSInfo, error) {
	if len(nalu) < 4 {
		return HEVCSPSInfo{}, errSPSTooShort
	}

	// Skip 2-byte NAL header
	rbsp := removeEmulationPrevention(nalu[2:])
	br := newBitReader(rbsp)

	// sps_video_parameter_set_id (4 bits)
	if _, err := br.readBits(4); err != nil {
		return HEVCSPSInfo{}, err
	}

	// sps_max_sub_layers_minus1 (3 bits)
	maxSubLayersMinus1, err := br.readBits(3)
	if err != nil {
		return HEVCSPSInfo{}, err
	}

	// sps_temporal_id_nesting_flag (1 bit)
	if _, err := br.readBits(1); err != nil {
		return HEVCSPSInfo{}, err
	}

	// profile_tier_level
	info := HEVCSPSInfo{}
	if err := parseHEVCProfileTierLevel(br, &info, maxSubLayersMinus1); err != nil {
		return HEVCSPSInfo{}, err
	}

	// sps_seq_parameter_set_id
	if _, err := br.readUE(); err != nil {
		return HEVCSPSInfo{}, err
	}

	// chroma_format_idc
	chromaFormatIdc, err := br.readUE()
	if err != nil {
		return HEVCSPSInfo{}, err
	}
	info.ChromaFormatIdc = byte(chromaFormatIdc)

	if chromaFormatIdc == 3 {
		// separate_colour_plane_flag
		if _, err := br.readBits(1); err != nil {
			return HEVCSPSInfo{}, err
		}
	}

	// pic_width_in_luma_samples
	width, err := br.readUE()
	if err != nil {
		return HEVCSPSInfo{}, err
	}

	// pic_height_in_luma_samples
	height, err := br.readUE()
	if err != nil {
		return HEVCSPSInfo{}, err
	}

	info.Width = int(width)
	info.Height = int(height)

	// conformance_window_flag
	confWindowFlag, err := br.readBits(1)
	if err != nil {
		return info, nil
	}

	if confWindowFlag == 1 {
		left, err := br.readUE()
		if err != nil {
			return info, nil
		}
		right, err := br.readUE()
		if err != nil {
			return info, nil
		}
		top, err := br.readUE()
		if err != nil {
			return info, nil
		}
		bottom, err := br.readUE()
		if err != nil {
			return info, nil
		}

		var subWidthC, subHeightC uint
		switch chromaFormatIdc {
		case 1:
			subWidthC, subHeightC = 2, 2
		case 2:
			subWidthC, subHeightC = 2, 1
		default:
			subWidthC, subHeightC = 1, 1
		}

		info.Width -= int((left + right) * subWidthC)
		info.Height -= int((top + bottom) * subHeightC)
	}

	// bit_depth_luma_minus8
	bdl, err := br.readUE()
	if err != nil {
		return info, nil
	}
	info.BitDepthLumaMinus8 = byte(bdl)

	// bit_depth_chroma_minus8
	bdc, err := br.readUE()
	if err != nil {
		return info, nil
	}
	info.BitDepthChromaMinus8 = byte(bdc)

	return info, nil
}

func parseHEVCProfileTierLevel(br *bitReader, info *HEVCSPSInfo, maxSubLayersMinus1 uint) error {
	// general_profile_space (2 bits)
	if _, err := br.readBits(2); err != nil {
		return err
	}

	// general_tier_flag (1 bit)
	tierFlag, err := br.readBits(1)
	if err != nil {
		return err
	}
	info.TierFlag = byte(tierFlag)

	// general_profile_idc (5 bits)
	profileIDC, err := br.readBits(5)
	if err != nil {
		return err
	}
	info.ProfileIDC = byte(profileIDC)

	// general_profile_compatibility_flags (32 bits)
	hi, err := br.readBits(16)
	if err != nil {
		return err
	}
	lo, err := br.readBits(16)
	if err != nil {
		return err
	}
	info.ProfileCompatibilityFlags = uint32(hi)<<16 | uint32(lo)

	// general_constraint_indicator_flags (48 bits = 6 bytes)
	var cif uint64
	for i := 0; i < 6; i++ {
		b, err := br.readBits(8)
		if err != nil {
			return err
		}
		cif = (cif << 8) | uint64(b)
	}
	info.ConstraintIndicatorFlags = cif

	// general_level_idc (8 bits)
	levelIDC, err := br.readBits(8)
	if err != nil {
		return err
	}
	info.LevelIDC = byte(levelIDC)

	// Skip sub-layer profile/level data
	if maxSubLayersMinus1 > 0 {
		var subLayerProfilePresent [8]bool
		var subLayerLevelPresent [8]bool
		for i := uint(0); i < maxSubLayersMinus1; i++ {
			pp, err := br.readBits(1)
			if err != nil {
				return err
			}
			subLayerProfilePresent[i] = pp == 1
			lp, err := br.readBits(1)
			if err != nil {
				return err
			}
			subLayerLevelPresent[i] = lp == 1
		}
		// reserved bits for alignment when maxSubLayersMinus1 < 8
		if maxSubLayersMinus1 < 8 {
			for i := maxSubLayersMinus1; i < 8; i++ {
				if _, err := br.readBits(2); err != nil {
					return err
				}
			}
		}
		for i := uint(0); i < maxSubLayersMinus1; i++ {
			if subLayerProfilePresent[i] {
				// sub_layer profile: 2+1+5+32+48 = 88 bits
				if _, err := br.readBits(32); err != nil {
					return err
				}
				if _, err := br.readBits(32); err != nil {
					return err
				}
				if _, err := br.readBits(24); err != nil {
					return err
				}
			}
			if subLayerLevelPresent[i] {
				if _, err := br.readBits(8); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
