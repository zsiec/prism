package demux

import (
	"errors"
	"fmt"
)

// AV1 OBU type constants as defined in AV1 spec §6.2.2.
const (
	OBUSequenceHeader       = 1
	OBUTemporalDelimiter    = 2
	OBUFrameHeader          = 3
	OBUTileGroup            = 4
	OBUMetadata             = 5
	OBUFrame                = 6
	OBURedundantFrameHeader = 7
	OBUTileList             = 8
	OBUPadding              = 15
)

// AV1 frame type constants as defined in AV1 spec §6.8.2.
const (
	av1FrameKey       = 0
	av1FrameInter     = 1
	av1FrameIntraOnly = 2
	av1FrameSwitch    = 3
)

var (
	errAV1TooShort     = errors.New("AV1 data too short")
	errAV1InvalidOBU   = errors.New("invalid AV1 OBU header")
	errAV1LEB128       = errors.New("invalid LEB128 encoding")
	errAV1SeqHdrParse  = errors.New("failed to parse AV1 sequence header")
	errAV1ForbiddenBit = errors.New("AV1 OBU forbidden bit set")
)

// AV1SequenceHeader holds parameters extracted from an AV1 Sequence Header OBU.
type AV1SequenceHeader struct {
	SeqProfile         int
	SeqLevelIdx        int
	SeqTier            int // 0=Main, 1=High
	BitDepth           int
	Width              int
	Height             int
	ChromaSubsamplingX int
	ChromaSubsamplingY int
	Monochrome         bool
}

// CodecString returns the RFC 6381 codec parameter string for AV1.
// Format: "av01.P.LLT.DD" where P=profile, LL=zero-padded level,
// T=tier(M/H), DD=zero-padded bit depth.
func (s *AV1SequenceHeader) CodecString() string {
	tier := "M"
	if s.SeqTier == 1 {
		tier = "H"
	}
	return fmt.Sprintf("av01.%d.%02d%s.%02d", s.SeqProfile, s.SeqLevelIdx, tier, s.BitDepth)
}

// obuHeader represents a parsed AV1 OBU header.
type obuHeader struct {
	obuType      int
	hasExtension bool
	hasSizeField bool
	temporalID   int
	spatialID    int
	headerLen    int // total header bytes (1 or 2)
	payloadSize  int // only valid when hasSizeField is true
	totalSize    int // headerLen + leb128 size bytes + payloadSize
}

// parseOBUHeader parses an OBU header from the given data. Returns the
// parsed header and total bytes consumed (header + optional size field).
func parseOBUHeader(data []byte) (obuHeader, error) {
	if len(data) < 1 {
		return obuHeader{}, errAV1TooShort
	}

	b := data[0]
	if b&0x80 != 0 {
		return obuHeader{}, errAV1ForbiddenBit
	}

	h := obuHeader{
		obuType:      int((b >> 3) & 0x0F),
		hasExtension: b&0x04 != 0,
		hasSizeField: b&0x02 != 0,
		headerLen:    1,
	}

	pos := 1
	if h.hasExtension {
		if len(data) < 2 {
			return obuHeader{}, errAV1TooShort
		}
		ext := data[1]
		h.temporalID = int((ext >> 5) & 0x07)
		h.spatialID = int((ext >> 3) & 0x03)
		h.headerLen = 2
		pos = 2
	}

	if h.hasSizeField {
		size, n, err := readLEB128(data[pos:])
		if err != nil {
			return obuHeader{}, err
		}
		h.payloadSize = size
		h.totalSize = pos + n + size
	} else {
		// Without size field, OBU extends to end of temporal unit.
		h.payloadSize = len(data) - pos
		h.totalSize = len(data)
	}

	return h, nil
}

// readLEB128 reads a LEB128 encoded unsigned integer from data.
// Returns the value, number of bytes consumed, and any error.
func readLEB128(data []byte) (int, int, error) {
	val := 0
	for i := 0; i < len(data) && i < 8; i++ {
		b := data[i]
		val |= int(b&0x7F) << (i * 7)
		if b&0x80 == 0 {
			return val, i + 1, nil
		}
	}
	return 0, 0, errAV1LEB128
}

// av1BitReader is a MSB-first bit reader that tracks errors internally.
// After an overflow, all subsequent reads return zero and the error is
// preserved in the err field.
type av1BitReader struct {
	data []byte
	pos  int
	bit  int
	err  error
}

func newAV1BitReader(data []byte) *av1BitReader {
	return &av1BitReader{data: data}
}

func (br *av1BitReader) readBits(n int) uint {
	if br.err != nil {
		return 0
	}
	var val uint
	for i := 0; i < n; i++ {
		if br.pos >= len(br.data) {
			br.err = errAV1TooShort
			return 0
		}
		val = (val << 1) | uint((br.data[br.pos]>>(7-br.bit))&1)
		br.bit++
		if br.bit == 8 {
			br.bit = 0
			br.pos++
		}
	}
	return val
}

// readUvlc reads an unsigned variable-length code (AV1 spec §4.10.3).
func (br *av1BitReader) readUvlc() uint {
	if br.err != nil {
		return 0
	}
	leadingZeros := 0
	for {
		b := br.readBits(1)
		if br.err != nil {
			return 0
		}
		if b == 1 {
			break
		}
		leadingZeros++
		if leadingZeros >= 32 {
			br.err = errAV1SeqHdrParse
			return 0
		}
	}
	if leadingZeros == 0 {
		return 0
	}
	val := br.readBits(leadingZeros)
	return val + (1 << leadingZeros) - 1
}

// ParseAV1SequenceHeader parses an AV1 Sequence Header OBU (including OBU
// header bytes) and extracts codec parameters. The input must start with
// the OBU header byte.
func ParseAV1SequenceHeader(data []byte) (*AV1SequenceHeader, error) {
	if len(data) < 2 {
		return nil, errAV1TooShort
	}

	h, err := parseOBUHeader(data)
	if err != nil {
		return nil, err
	}

	if h.obuType != OBUSequenceHeader {
		return nil, fmt.Errorf("expected OBU_SEQUENCE_HEADER (type %d), got type %d", OBUSequenceHeader, h.obuType)
	}

	// Payload starts after header + LEB128 size
	payloadStart := h.totalSize - h.payloadSize
	if payloadStart >= len(data) || payloadStart+h.payloadSize > len(data) {
		return nil, errAV1TooShort
	}
	payload := data[payloadStart : payloadStart+h.payloadSize]

	return parseSequenceHeaderPayload(payload)
}

// parseSequenceHeaderPayload parses the raw payload of a sequence_header_obu.
func parseSequenceHeaderPayload(payload []byte) (*AV1SequenceHeader, error) {
	br := newAV1BitReader(payload)
	hdr := &AV1SequenceHeader{}

	hdr.SeqProfile = int(br.readBits(3))
	stillPicture := br.readBits(1)
	reducedStillPicture := br.readBits(1)
	_ = stillPicture

	var decoderModelInfoPresent bool
	var bufferDelayLengthMinus1 uint

	if reducedStillPicture == 1 {
		hdr.SeqLevelIdx = int(br.readBits(5))
		hdr.SeqTier = 0
	} else {
		timingInfoPresent := br.readBits(1)

		if timingInfoPresent == 1 {
			// timing_info()
			br.readBits(32) // num_units_in_display_tick
			br.readBits(32) // time_scale
			equalPictureInterval := br.readBits(1)
			if equalPictureInterval == 1 {
				br.readUvlc() // num_ticks_per_picture_minus_1
			}

			dmi := br.readBits(1)
			decoderModelInfoPresent = dmi == 1
			if decoderModelInfoPresent {
				// decoder_model_info()
				bufferDelayLengthMinus1 = br.readBits(5)
				br.readBits(32) // num_units_in_decoding_tick
				br.readBits(5)  // buffer_removal_time_length_minus_1
				br.readBits(5)  // frame_presentation_time_length_minus_1
			}
		}

		initialDisplayDelayPresent := br.readBits(1)
		operatingPointsCntMinus1 := br.readBits(5)

		for i := uint(0); i <= operatingPointsCntMinus1; i++ {
			br.readBits(12) // operating_point_idc
			levelIdx := br.readBits(5)
			var tier uint
			if levelIdx > 7 {
				tier = br.readBits(1)
			}
			if i == 0 {
				hdr.SeqLevelIdx = int(levelIdx)
				hdr.SeqTier = int(tier)
			}
			if decoderModelInfoPresent {
				decoderModelPresentForThisOp := br.readBits(1)
				if decoderModelPresentForThisOp == 1 {
					// operating_parameters_info(): skip decoder/encoder buffer delays and flag
					n := int(bufferDelayLengthMinus1) + 1
					br.readBits(n) // decoder_buffer_delay[i]
					br.readBits(n) // encoder_buffer_delay[i]
					br.readBits(1) // low_delay_mode_flag[i]
				}
			}
			if initialDisplayDelayPresent == 1 {
				flag := br.readBits(1)
				if flag == 1 {
					br.readBits(4) // initial_display_delay_minus_1
				}
			}
		}
	}

	if br.err != nil {
		return nil, fmt.Errorf("%w: %v", errAV1SeqHdrParse, br.err)
	}

	frameWidthBitsMinus1 := br.readBits(4)
	frameHeightBitsMinus1 := br.readBits(4)
	maxFrameWidthMinus1 := br.readBits(int(frameWidthBitsMinus1) + 1)
	maxFrameHeightMinus1 := br.readBits(int(frameHeightBitsMinus1) + 1)
	hdr.Width = int(maxFrameWidthMinus1) + 1
	hdr.Height = int(maxFrameHeightMinus1) + 1

	if reducedStillPicture == 0 {
		frameIDNumbersPresent := br.readBits(1)
		if frameIDNumbersPresent == 1 {
			br.readBits(4) // delta_frame_id_length_minus_2
			br.readBits(3) // additional_frame_id_length_minus_1
		}
	}

	br.readBits(1) // use_128x128_superblock
	br.readBits(1) // enable_filter_intra
	br.readBits(1) // enable_intra_edge_filter

	if reducedStillPicture == 0 {
		br.readBits(1) // enable_interintra_compound
		br.readBits(1) // enable_masked_compound
		br.readBits(1) // enable_warped_motion
		br.readBits(1) // enable_dual_filter
		enableOrderHint := br.readBits(1)

		if enableOrderHint == 1 {
			br.readBits(1) // enable_jnt_comp
			br.readBits(1) // enable_ref_frame_mvs
		}

		seqChooseScreenContentTools := br.readBits(1)
		var seqForceScreenContentTools uint
		if seqChooseScreenContentTools == 0 {
			seqForceScreenContentTools = br.readBits(1)
		} else {
			seqForceScreenContentTools = 2 // SELECT_SCREEN_CONTENT_TOOLS
		}

		if seqForceScreenContentTools > 0 {
			seqChooseIntegerMV := br.readBits(1)
			if seqChooseIntegerMV == 0 {
				br.readBits(1) // seq_force_integer_mv
			}
		}

		if enableOrderHint == 1 {
			br.readBits(3) // order_hint_bits_minus_1
		}
	}

	br.readBits(1) // enable_superres
	br.readBits(1) // enable_cdef
	br.readBits(1) // enable_restoration

	if br.err != nil {
		return nil, fmt.Errorf("%w: %v", errAV1SeqHdrParse, br.err)
	}

	// color_config()
	parseColorConfig(br, hdr)

	if br.err != nil {
		return nil, fmt.Errorf("%w: %v", errAV1SeqHdrParse, br.err)
	}

	return hdr, nil
}

// parseColorConfig parses the color_config() section of a sequence header.
func parseColorConfig(br *av1BitReader, hdr *AV1SequenceHeader) {
	highBitDepth := br.readBits(1)

	if hdr.SeqProfile == 2 && highBitDepth == 1 {
		twelveBit := br.readBits(1)
		if twelveBit == 1 {
			hdr.BitDepth = 12
		} else {
			hdr.BitDepth = 10
		}
	} else if highBitDepth == 1 {
		hdr.BitDepth = 10
	} else {
		hdr.BitDepth = 8
	}

	var monoChrome uint
	if hdr.SeqProfile == 1 {
		monoChrome = 0
	} else {
		monoChrome = br.readBits(1)
	}
	hdr.Monochrome = monoChrome == 1

	colorDescriptionPresent := br.readBits(1)
	var colorPrimaries, transferCharacteristics, matrixCoefficients uint
	if colorDescriptionPresent == 1 {
		colorPrimaries = br.readBits(8)
		transferCharacteristics = br.readBits(8)
		matrixCoefficients = br.readBits(8)
	} else {
		colorPrimaries = 2        // CP_UNSPECIFIED
		transferCharacteristics = 2 // TC_UNSPECIFIED
		matrixCoefficients = 2      // MC_UNSPECIFIED
	}

	if monoChrome == 1 {
		br.readBits(1) // color_range
		hdr.ChromaSubsamplingX = 1
		hdr.ChromaSubsamplingY = 1
		return
	}

	// Check for sRGB/sYCC
	if colorPrimaries == 1 && transferCharacteristics == 13 && matrixCoefficients == 0 {
		// color_range is implicitly 1 (full)
		hdr.ChromaSubsamplingX = 0
		hdr.ChromaSubsamplingY = 0
		return
	}

	br.readBits(1) // color_range

	if hdr.SeqProfile == 0 {
		hdr.ChromaSubsamplingX = 1
		hdr.ChromaSubsamplingY = 1
	} else if hdr.SeqProfile == 1 {
		hdr.ChromaSubsamplingX = 0
		hdr.ChromaSubsamplingY = 0
	} else {
		// Profile 2
		if hdr.BitDepth == 12 {
			sx := br.readBits(1)
			hdr.ChromaSubsamplingX = int(sx)
			if sx == 1 {
				hdr.ChromaSubsamplingY = int(br.readBits(1))
			} else {
				hdr.ChromaSubsamplingY = 0
			}
		} else {
			hdr.ChromaSubsamplingX = 1
			hdr.ChromaSubsamplingY = 0
		}
	}

	if hdr.ChromaSubsamplingX == 1 && hdr.ChromaSubsamplingY == 1 {
		br.readBits(2) // chroma_sample_position
	}

	br.readBits(1) // separate_uv_delta_q
}

// IsAV1Keyframe scans OBU headers in a temporal unit for a key frame.
// It checks OBU_FRAME (type 6) and OBU_FRAME_HEADER (type 3) for
// frame_type == KEY_FRAME (0). Returns false for nil/empty input.
func IsAV1Keyframe(temporalUnit []byte) bool {
	if len(temporalUnit) == 0 {
		return false
	}

	pos := 0
	for pos < len(temporalUnit) {
		h, err := parseOBUHeader(temporalUnit[pos:])
		if err != nil {
			return false
		}

		if h.obuType == OBUFrame || h.obuType == OBUFrameHeader {
			// Payload starts after header bytes and optional LEB128 size
			payloadStart := pos + h.totalSize - h.payloadSize
			if payloadStart >= len(temporalUnit) || h.payloadSize < 1 {
				return false
			}
			// frame_header_obu(): first bit is show_existing_frame
			// If show_existing_frame==1, this is not a new frame.
			// If show_existing_frame==0, next 2 bits are frame_type.
			payload := temporalUnit[payloadStart:]
			if len(payload) < 1 {
				return false
			}
			showExisting := (payload[0] >> 7) & 1
			if showExisting == 1 {
				return false
			}
			if len(payload) < 1 {
				return false
			}
			frameType := (payload[0] >> 5) & 0x03
			return frameType == av1FrameKey
		}

		if h.hasSizeField {
			pos += h.totalSize
		} else {
			// No size field — OBU extends to end
			break
		}
	}
	return false
}

// FindSequenceHeaderOBU scans a temporal unit for an OBU_SEQUENCE_HEADER
// (type 1) and returns the complete OBU bytes (header + payload).
// Returns nil if no sequence header is found.
func FindSequenceHeaderOBU(temporalUnit []byte) []byte {
	if len(temporalUnit) == 0 {
		return nil
	}

	pos := 0
	for pos < len(temporalUnit) {
		h, err := parseOBUHeader(temporalUnit[pos:])
		if err != nil {
			return nil
		}

		if h.obuType == OBUSequenceHeader {
			end := pos + h.totalSize
			if end > len(temporalUnit) {
				end = len(temporalUnit)
			}
			return temporalUnit[pos:end]
		}

		if h.hasSizeField {
			pos += h.totalSize
		} else {
			break
		}
	}
	return nil
}
