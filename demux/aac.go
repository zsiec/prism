package demux

import "errors"

// ErrInvalidADTS is returned when the ADTS sync word or header is malformed.
var ErrInvalidADTS = errors.New("invalid ADTS header")

// AAC sample rate index table (ISO 14496-3)
var aacSampleRates = [...]int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350,
}

// AACFrame represents a single AAC audio frame parsed from ADTS.
type AACFrame struct {
	Data       []byte // complete ADTS frame (header + payload)
	SampleRate int
	Channels   int
}

// ParseADTS parses an ADTS byte stream into individual AAC frames.
func ParseADTS(data []byte) ([]AACFrame, error) {
	var frames []AACFrame
	offset := 0

	for offset < len(data) {
		if len(data)-offset < 7 {
			break // not enough for ADTS header
		}

		// Sync word: 0xFFF
		if data[offset] != 0xFF || (data[offset+1]&0xF0) != 0xF0 {
			// Try to find next sync word
			offset++
			continue
		}

		// Parse ADTS header
		hasCRC := (data[offset+1] & 0x01) == 0
		headerSize := 7
		if hasCRC {
			headerSize = 9
		}

		sampleRateIdx := (data[offset+2] >> 2) & 0x0F
		if int(sampleRateIdx) >= len(aacSampleRates) {
			return frames, ErrInvalidADTS
		}

		channelCfg := ((data[offset+2] & 0x01) << 2) | ((data[offset+3] >> 6) & 0x03)

		frameLen := int(data[offset+3]&0x03)<<11 |
			int(data[offset+4])<<3 |
			int(data[offset+5]>>5)

		if frameLen < headerSize || offset+frameLen > len(data) {
			break // truncated
		}

		frames = append(frames, AACFrame{
			Data:       data[offset : offset+frameLen],
			SampleRate: aacSampleRates[sampleRateIdx],
			Channels:   int(channelCfg),
		})

		offset += frameLen
	}

	return frames, nil
}
