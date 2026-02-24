package main

import "github.com/zsiec/prism/test/tools/tsutil"

// ptsEntry records the byte offset within a TS file where a PTS, DTS, or PCR
// value lives, along with enough context to decode/re-encode it in place.
type ptsEntry struct {
	offset int  // byte offset in the file
	isPCR  bool // true = 6-byte PCR in adaptation field; false = 5-byte PTS/DTS in PES
}

// scanTimestamps walks the TS data and returns every PTS, DTS, and PCR byte
// location, plus the first and last video PTS values (in 90 kHz ticks).
// These are used to compute the loop offset for seamless looping.
func scanTimestamps(data []byte) (entries []ptsEntry, firstPTS, lastPTS int64) {
	firstPTS = -1

	for off := 0; off+tsutil.TSPacketSize <= len(data); off += tsutil.TSPacketSize {
		pkt := data[off : off+tsutil.TSPacketSize]
		if pkt[0] != 0x47 {
			continue
		}

		hasAdapt := pkt[3]&0x20 != 0
		hasPayload := pkt[3]&0x10 != 0

		payloadOff := 4

		// Check adaptation field for PCR.
		if hasAdapt && payloadOff < tsutil.TSPacketSize {
			afLen := int(pkt[payloadOff])
			if afLen > 0 && payloadOff+1 < tsutil.TSPacketSize {
				afFlags := pkt[payloadOff+1]
				if afFlags&0x10 != 0 && afLen >= 7 { // PCR flag set, need 6 PCR bytes
					entries = append(entries, ptsEntry{offset: off + payloadOff + 2, isPCR: true})
				}
			}
			payloadOff += 1 + afLen
		}

		// Check PES header for PTS/DTS (only on payload-unit-start packets).
		pusi := pkt[1]&0x40 != 0
		if !pusi || !hasPayload || payloadOff >= tsutil.TSPacketSize {
			continue
		}

		payload := pkt[payloadOff:]
		if len(payload) < 14 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
			continue
		}

		streamID := payload[3]
		isMedia := (streamID >= 0xC0 && streamID <= 0xDF) || // audio
			(streamID >= 0xE0 && streamID <= 0xEF) // video
		if !isMedia {
			continue
		}

		if len(payload) < 9 {
			continue
		}
		flags := payload[7]
		hasPTS := flags&0x80 != 0
		hasDTS := flags&0x40 != 0

		if hasPTS && len(payload) >= 14 {
			absOff := off + payloadOff + 9
			entries = append(entries, ptsEntry{offset: absOff, isPCR: false})

			// Track first/last video PTS for loop duration calculation.
			isVideo := streamID >= 0xE0 && streamID <= 0xEF
			if isVideo {
				pts := decodePTS(data[absOff:])
				if firstPTS < 0 || pts < firstPTS {
					firstPTS = pts
				}
				if pts > lastPTS {
					lastPTS = pts
				}
			}
		}
		if hasDTS && len(payload) >= 19 {
			absOff := off + payloadOff + 14
			entries = append(entries, ptsEntry{offset: absOff, isPCR: false})
		}
	}

	return entries, firstPTS, lastPTS
}

// addTimestampOffset adds delta (in 90 kHz ticks) to every recorded PTS/DTS/PCR
// location in the data buffer. Call this once per loop iteration to keep
// timestamps monotonically increasing across file loops.
func addTimestampOffset(data []byte, entries []ptsEntry, delta int64) {
	for _, e := range entries {
		if e.isPCR {
			pcr := decodePCR(data[e.offset:])
			encodePCR(data[e.offset:], pcr+delta)
		} else {
			pts := decodePTS(data[e.offset:])
			encodePTS(data[e.offset:], pts+delta)
		}
	}
}

// decodePTS extracts a 33-bit PTS/DTS from the 5-byte PES timestamp encoding.
func decodePTS(b []byte) int64 {
	return int64(b[0]>>1&0x07)<<30 |
		int64(b[1])<<22 |
		int64(b[2]>>1&0x7F)<<15 |
		int64(b[3])<<7 |
		int64(b[4]>>1&0x7F)
}

// encodePTS writes a 33-bit PTS/DTS into the 5-byte PES timestamp encoding,
// preserving the marker bits and prefix nibble from the original byte.
func encodePTS(b []byte, pts int64) {
	// Preserve the top nibble (contains '0010' or '0011' or '0001' prefix).
	prefix := b[0] & 0xF0
	b[0] = prefix | byte((pts>>29)&0x0E) | 0x01
	b[1] = byte(pts >> 22)
	b[2] = byte((pts>>14)&0xFE) | 0x01
	b[3] = byte(pts >> 7)
	b[4] = byte((pts<<1)&0xFE) | 0x01
}

// decodePCR extracts a 33-bit PCR base (90 kHz) from the 6-byte adaptation
// field encoding. The 9-bit extension is ignored for offset purposes.
func decodePCR(b []byte) int64 {
	return int64(b[0])<<25 |
		int64(b[1])<<17 |
		int64(b[2])<<9 |
		int64(b[3])<<1 |
		int64(b[4]>>7)
}

// encodePCR writes a 33-bit PCR base into the 6-byte encoding, preserving
// the 9-bit extension and reserved bits.
func encodePCR(b []byte, base int64) {
	ext := uint16(b[4]&0x01)<<8 | uint16(b[5])
	b[0] = byte(base >> 25)
	b[1] = byte(base >> 17)
	b[2] = byte(base >> 9)
	b[3] = byte(base >> 1)
	b[4] = byte((base&1)<<7) | 0x7E | byte(ext>>8)
	b[5] = byte(ext)
}
