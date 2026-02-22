package demux

import (
	"errors"
	"fmt"
)

// H.264 NAL unit type constants as defined in ITU-T H.264 Table 7-1.
const (
	NALTypeSlice      = 1
	NALTypeIDR        = 5
	NALTypeSEI        = 6
	NALTypeSPS        = 7
	NALTypePPS        = 8
	NALTypeAUD        = 9
	NALTypeFillerData = 12
)

// SPSInfo holds parameters extracted from an H.264 Sequence Parameter Set,
// including resolution, profile/level identifiers, and HRD timing fields
// needed for pic_timing SEI parsing (timecode extraction).
type SPSInfo struct {
	Width              int
	Height             int
	ProfileIDC         byte
	ConstraintFlags    byte
	LevelIDC           byte
	PicStructPresent   bool
	HRDPresent         bool
	CpbRemovalDelayLen int
	DpbOutputDelayLen  int
	TimeOffsetLen      int
}

// CodecString returns the RFC 6381 codec parameter string (e.g. "avc1.42E01E")
// for use in WebCodecs configuration and MIME types.
func (s SPSInfo) CodecString() string {
	return fmt.Sprintf("avc1.%02X%02X%02X", s.ProfileIDC, s.ConstraintFlags, s.LevelIDC)
}

// Timecode represents a SMPTE 12M timecode extracted from an H.264 pic_timing
// SEI message.
type Timecode struct {
	Hours   int
	Minutes int
	Seconds int
	Frames  int
}

// String formats the timecode as HH:MM:SS:FF.
func (tc Timecode) String() string {
	return fmt.Sprintf("%02d:%02d:%02d:%02d", tc.Hours, tc.Minutes, tc.Seconds, tc.Frames)
}

var errSPSTooShort = errors.New("SPS data too short")

type bitReader struct {
	data []byte
	pos  int
	bit  int
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (br *bitReader) readBit() (uint, error) {
	if br.pos >= len(br.data) {
		return 0, errSPSTooShort
	}
	val := uint((br.data[br.pos] >> (7 - br.bit)) & 1)
	br.bit++
	if br.bit == 8 {
		br.bit = 0
		br.pos++
	}
	return val, nil
}

func (br *bitReader) readBits(n int) (uint, error) {
	var val uint
	for i := 0; i < n; i++ {
		b, err := br.readBit()
		if err != nil {
			return 0, err
		}
		val = (val << 1) | b
	}
	return val, nil
}

func (br *bitReader) readUE() (uint, error) {
	zeros := 0
	for {
		b, err := br.readBit()
		if err != nil {
			return 0, err
		}
		if b == 1 {
			break
		}
		zeros++
		if zeros > 31 {
			return 0, errSPSTooShort
		}
	}
	if zeros == 0 {
		return 0, nil
	}
	suffix, err := br.readBits(zeros)
	if err != nil {
		return 0, err
	}
	return (1 << zeros) - 1 + suffix, nil
}

func (br *bitReader) readSE() (int, error) {
	val, err := br.readUE()
	if err != nil {
		return 0, err
	}
	if val%2 == 0 {
		return -int(val / 2), nil
	}
	return int((val + 1) / 2), nil
}

func (br *bitReader) skipScalingList(size int) error {
	lastScale := 8
	nextScale := 8
	for j := 0; j < size; j++ {
		if nextScale != 0 {
			delta, err := br.readSE()
			if err != nil {
				return err
			}
			nextScale = (lastScale + delta + 256) % 256
		}
		if nextScale != 0 {
			lastScale = nextScale
		}
	}
	return nil
}

// ParseSPS parses an H.264 SPS NAL unit to extract resolution, profile/level,
// and VUI/HRD timing parameters. The input should be the raw NAL data
// including the NAL header byte but without the start code.
func ParseSPS(nalu []byte) (SPSInfo, error) {
	if len(nalu) < 4 {
		return SPSInfo{}, errSPSTooShort
	}

	rbsp := removeEmulationPrevention(nalu[1:])
	br := newBitReader(rbsp)

	profileIdc, err := br.readBits(8)
	if err != nil {
		return SPSInfo{}, err
	}

	constraintFlags, err := br.readBits(8)
	if err != nil {
		return SPSInfo{}, err
	}
	levelIdc, err := br.readBits(8)
	if err != nil {
		return SPSInfo{}, err
	}
	if _, err := br.readUE(); err != nil {
		return SPSInfo{}, err
	}

	chromaFormatIdc := uint(1)
	separateColourPlane := false

	if profileIdc == 100 || profileIdc == 110 || profileIdc == 122 ||
		profileIdc == 244 || profileIdc == 44 || profileIdc == 83 ||
		profileIdc == 86 || profileIdc == 118 || profileIdc == 128 ||
		profileIdc == 138 || profileIdc == 139 || profileIdc == 134 {

		chromaFormatIdc, err = br.readUE()
		if err != nil {
			return SPSInfo{}, err
		}
		if chromaFormatIdc == 3 {
			val, err := br.readBits(1)
			if err != nil {
				return SPSInfo{}, err
			}
			separateColourPlane = val == 1
		}
		if _, err := br.readUE(); err != nil {
			return SPSInfo{}, err
		}
		if _, err := br.readUE(); err != nil {
			return SPSInfo{}, err
		}
		if _, err := br.readBits(1); err != nil {
			return SPSInfo{}, err
		}

		seqScalingMatrixPresent, err := br.readBits(1)
		if err != nil {
			return SPSInfo{}, err
		}
		if seqScalingMatrixPresent == 1 {
			limit := 8
			if chromaFormatIdc == 3 {
				limit = 12
			}
			for i := 0; i < limit; i++ {
				flag, err := br.readBits(1)
				if err != nil {
					return SPSInfo{}, err
				}
				if flag == 1 {
					size := 16
					if i >= 6 {
						size = 64
					}
					if err := br.skipScalingList(size); err != nil {
						return SPSInfo{}, err
					}
				}
			}
		}
	}

	if _, err := br.readUE(); err != nil {
		return SPSInfo{}, err
	}

	picOrderCntType, err := br.readUE()
	if err != nil {
		return SPSInfo{}, err
	}

	switch picOrderCntType {
	case 0:
		if _, err := br.readUE(); err != nil {
			return SPSInfo{}, err
		}
	case 1:
		if _, err := br.readBits(1); err != nil {
			return SPSInfo{}, err
		}
		if _, err := br.readSE(); err != nil {
			return SPSInfo{}, err
		}
		if _, err := br.readSE(); err != nil {
			return SPSInfo{}, err
		}
		numRefFrames, err := br.readUE()
		if err != nil {
			return SPSInfo{}, err
		}
		for i := uint(0); i < numRefFrames; i++ {
			if _, err := br.readSE(); err != nil {
				return SPSInfo{}, err
			}
		}
	}

	if _, err := br.readUE(); err != nil {
		return SPSInfo{}, err
	}
	if _, err := br.readBits(1); err != nil {
		return SPSInfo{}, err
	}

	picWidthMbs, err := br.readUE()
	if err != nil {
		return SPSInfo{}, err
	}
	picHeightMapUnits, err := br.readUE()
	if err != nil {
		return SPSInfo{}, err
	}

	frameMbsOnly, err := br.readBits(1)
	if err != nil {
		return SPSInfo{}, err
	}
	if frameMbsOnly == 0 {
		if _, err := br.readBits(1); err != nil {
			return SPSInfo{}, err
		}
	}

	if _, err := br.readBits(1); err != nil {
		return SPSInfo{}, err
	}

	cropLeft, cropRight, cropTop, cropBottom := uint(0), uint(0), uint(0), uint(0)
	frameCroppingFlag, err := br.readBits(1)
	if err != nil {
		return SPSInfo{}, err
	}
	if frameCroppingFlag == 1 {
		cropLeft, err = br.readUE()
		if err != nil {
			return SPSInfo{}, err
		}
		cropRight, err = br.readUE()
		if err != nil {
			return SPSInfo{}, err
		}
		cropTop, err = br.readUE()
		if err != nil {
			return SPSInfo{}, err
		}
		cropBottom, err = br.readUE()
		if err != nil {
			return SPSInfo{}, err
		}
	}

	chromaArrayType := chromaFormatIdc
	if separateColourPlane {
		chromaArrayType = 0
	}
	var subWidthC, subHeightC uint
	switch chromaArrayType {
	case 0:
		subWidthC, subHeightC = 1, 1
	case 1:
		subWidthC, subHeightC = 2, 2
	case 2:
		subWidthC, subHeightC = 2, 1
	case 3:
		subWidthC, subHeightC = 1, 1
	default:
		subWidthC, subHeightC = 2, 2
	}

	cropUnitX := subWidthC
	cropUnitY := subHeightC * (2 - frameMbsOnly)

	width := int((picWidthMbs+1)*16 - cropUnitX*(cropLeft+cropRight))
	heightMul := 2 - frameMbsOnly
	height := int((picHeightMapUnits+1)*16*heightMul - cropUnitY*(cropTop+cropBottom))

	info := SPSInfo{
		Width:           width,
		Height:          height,
		ProfileIDC:      byte(profileIdc),
		ConstraintFlags: byte(constraintFlags),
		LevelIDC:        byte(levelIdc),
	}

	vuiPresent, err := br.readBits(1)
	if err != nil || vuiPresent == 0 {
		return info, nil
	}

	skipVUIField := func(flagBits, dataBits int) {
		f, e := br.readBits(flagBits)
		if e != nil || f == 0 {
			return
		}
		br.readBits(dataBits)
	}

	arPresent, _ := br.readBits(1)
	if arPresent == 1 {
		arIdc, _ := br.readBits(8)
		if arIdc == 255 {
			br.readBits(32)
		}
	}

	skipVUIField(1, 1) // overscan

	videoSignal, _ := br.readBits(1)
	if videoSignal == 1 {
		br.readBits(4) // video_format + video_full_range
		colourDesc, _ := br.readBits(1)
		if colourDesc == 1 {
			br.readBits(24)
		}
	}

	chromaLoc, _ := br.readBits(1)
	if chromaLoc == 1 {
		br.readUE()
		br.readUE()
	}

	timingPresent, _ := br.readBits(1)
	if timingPresent == 1 {
		br.readBits(32) // num_units_in_tick
		br.readBits(32) // time_scale
		br.readBits(1)  // fixed_frame_rate_flag
	}

	parseHRD := func() {
		cpbCnt, _ := br.readUE()
		br.readBits(8) // bit_rate_scale + cpb_size_scale
		for i := uint(0); i <= cpbCnt; i++ {
			br.readUE()
			br.readUE()
			br.readBits(1)
		}
		br.readBits(5) // initial_cpb_removal_delay_length_minus1
		cpbRdLen, _ := br.readBits(5)
		dpbOdLen, _ := br.readBits(5)
		toLen, _ := br.readBits(5)
		info.CpbRemovalDelayLen = int(cpbRdLen) + 1
		info.DpbOutputDelayLen = int(dpbOdLen) + 1
		info.TimeOffsetLen = int(toLen)
		info.HRDPresent = true
	}

	nalHRD, _ := br.readBits(1)
	if nalHRD == 1 {
		parseHRD()
	}

	vclHRD, _ := br.readBits(1)
	if vclHRD == 1 && !info.HRDPresent {
		parseHRD()
	}

	if nalHRD == 1 || vclHRD == 1 {
		br.readBits(1) // low_delay_hrd_flag
	}

	picStructPresent, _ := br.readBits(1)
	info.PicStructPresent = picStructPresent == 1

	return info, nil
}

func removeEmulationPrevention(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 3 &&
			(i+3 >= len(data) || data[i+3] <= 3) {
			out = append(out, 0, 0)
			i += 2
		} else {
			out = append(out, data[i])
		}
	}
	return out
}

// NALUnit represents a parsed H.264 or H.265 NAL unit.
type NALUnit struct {
	Type byte   // NAL type (codec-specific: 5-bit for H.264, 6-bit for H.265)
	Data []byte // raw NAL data including the NAL header byte(s), without start code
}

// parseAnnexBGeneric scans an Annex B byte stream for start codes and extracts
// NAL units. The nalTypeFunc extracts the codec-specific NAL type from the raw
// NAL data. Both 3-byte (0x000001) and 4-byte (0x00000001) start codes are
// recognized. minNALBytes is the minimum NAL data length (1 for H.264, 2 for HEVC).
func parseAnnexBGeneric(data []byte, minNALBytes int, nalTypeFunc func([]byte) byte) []NALUnit {
	var units []NALUnit
	n := len(data)
	if n < 4 {
		return nil
	}

	type scPos struct {
		scStart   int
		dataStart int
	}

	var positions []scPos
	i := 0
	for i < n-2 {
		if data[i] == 0 && data[i+1] == 0 {
			if i < n-3 && data[i+2] == 0 && data[i+3] == 1 {
				positions = append(positions, scPos{scStart: i, dataStart: i + 4})
				i += 4
				continue
			}
			if data[i+2] == 1 {
				positions = append(positions, scPos{scStart: i, dataStart: i + 3})
				i += 3
				continue
			}
		}
		i++
	}

	for idx, pos := range positions {
		if pos.dataStart >= n {
			continue
		}
		end := n
		if idx+1 < len(positions) {
			end = positions[idx+1].scStart
		}
		if pos.dataStart >= end {
			continue
		}

		nalData := data[pos.dataStart:end]
		if len(nalData) < minNALBytes {
			continue
		}

		units = append(units, NALUnit{
			Type: nalTypeFunc(nalData),
			Data: nalData,
		})
	}

	return units
}

// ParseAnnexB parses H.264 Annex B byte stream into individual NAL units.
// It recognizes both 3-byte (0x000001) and 4-byte (0x00000001) start codes.
func ParseAnnexB(data []byte) []NALUnit {
	return parseAnnexBGeneric(data, 1, func(d []byte) byte { return d[0] & 0x1F })
}

// IsKeyframe returns true if the NAL type is an IDR slice (type 5).
func IsKeyframe(nalType byte) bool {
	return nalType == NALTypeIDR
}

// IsSPS returns true if the NAL type is SPS (type 7).
func IsSPS(nalType byte) bool {
	return nalType == NALTypeSPS
}

// IsPPS returns true if the NAL type is PPS (type 8).
func IsPPS(nalType byte) bool {
	return nalType == NALTypePPS
}

// ParsePicTimingSEI extracts a SMPTE 12M timecode from an H.264 pic_timing
// SEI message. Returns the timecode and true if extraction succeeded, or a
// zero value and false if the SEI doesn't contain valid clock timestamps.
// Requires HRD parameters from the SPS for correct bitstream parsing.
func ParsePicTimingSEI(seiNALU []byte, sps SPSInfo) (Timecode, bool) {
	if len(seiNALU) < 2 {
		return Timecode{}, false
	}
	if !sps.PicStructPresent || !sps.HRDPresent {
		return Timecode{}, false
	}

	rbsp := removeEmulationPrevention(seiNALU[1:])
	i := 0
	for i < len(rbsp) {
		if rbsp[i] == 0x80 {
			break
		}

		payloadType := 0
		for i < len(rbsp) && rbsp[i] == 0xFF {
			payloadType += 255
			i++
		}
		if i >= len(rbsp) {
			break
		}
		payloadType += int(rbsp[i])
		i++

		payloadSize := 0
		for i < len(rbsp) && rbsp[i] == 0xFF {
			payloadSize += 255
			i++
		}
		if i >= len(rbsp) {
			break
		}
		payloadSize += int(rbsp[i])
		i++

		if i+payloadSize > len(rbsp) {
			break
		}

		if payloadType == 1 {
			tc, ok := parsePicTimingPayload(rbsp[i:i+payloadSize], sps)
			if ok {
				return tc, true
			}
		}
		i += payloadSize
	}

	return Timecode{}, false
}

func parsePicTimingPayload(payload []byte, sps SPSInfo) (Timecode, bool) {
	br := newBitReader(payload)

	br.readBits(sps.CpbRemovalDelayLen)
	br.readBits(sps.DpbOutputDelayLen)

	picStruct, err := br.readBits(4)
	if err != nil {
		return Timecode{}, false
	}

	numClockTS := 1
	switch picStruct {
	case 3, 4:
		numClockTS = 2
	case 5, 6, 7, 8:
		numClockTS = 3
	}

	for c := 0; c < numClockTS; c++ {
		clockTSFlag, err := br.readBits(1)
		if err != nil {
			return Timecode{}, false
		}
		if clockTSFlag == 0 {
			continue
		}

		br.readBits(2) // ct_type
		br.readBits(1) // nuit_field_based_flag
		br.readBits(5) // counting_type
		fullTSFlag, _ := br.readBits(1)
		br.readBits(1) // discontinuity_flag
		br.readBits(1) // cnt_dropped_flag
		nFrames, _ := br.readBits(8)

		var secs, mins, hours uint
		if fullTSFlag == 1 {
			secs, _ = br.readBits(6)
			mins, _ = br.readBits(6)
			hours, _ = br.readBits(5)
		} else {
			secFlag, _ := br.readBits(1)
			if secFlag == 1 {
				secs, _ = br.readBits(6)
				minFlag, _ := br.readBits(1)
				if minFlag == 1 {
					mins, _ = br.readBits(6)
					hrFlag, _ := br.readBits(1)
					if hrFlag == 1 {
						hours, _ = br.readBits(5)
					}
				}
			}
		}

		if sps.TimeOffsetLen > 0 {
			br.readBits(sps.TimeOffsetLen)
		}

		return Timecode{
			Hours:   int(hours),
			Minutes: int(mins),
			Seconds: int(secs),
			Frames:  int(nFrames),
		}, true
	}

	return Timecode{}, false
}
