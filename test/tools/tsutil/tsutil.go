// Package tsutil provides shared MPEG-TS infrastructure used by the inject-*
// tools and related test utilities.
package tsutil

import "os"

// TSPacketSize is the fixed size of an MPEG-TS packet.
const TSPacketSize = 188

// PESPacket holds one reassembled PES packet and the TS-packet offsets it
// came from.
type PESPacket struct {
	ESData    []byte
	PESHdr    []byte
	TSOffsets []int
}

// FindNALStarts returns the byte offsets immediately after each 3- or 4-byte
// Annex-B start code found in data.
func FindNALStarts(data []byte) []int {
	var starts []int
	for i := 0; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 {
			if i < len(data)-3 && data[i+2] == 0 && data[i+3] == 1 {
				starts = append(starts, i+4)
				i += 3
			} else if data[i+2] == 1 {
				starts = append(starts, i+3)
				i += 2
			}
		}
	}
	return starts
}

// CollectPESPackets walks tsData looking for PES packets on videoPID and
// returns them as a slice of reassembled PESPacket values.
func CollectPESPackets(tsData []byte, videoPID uint16) []PESPacket {
	var packets []PESPacket
	var current *PESPacket

	for off := 0; off+TSPacketSize <= len(tsData); off += TSPacketSize {
		pkt := tsData[off : off+TSPacketSize]
		if pkt[0] != 0x47 {
			continue
		}

		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
		if pid != videoPID {
			continue
		}

		payloadStart := pkt[1]&0x40 != 0
		headerLen := 4
		if pkt[3]&0x20 != 0 {
			adaptLen := int(pkt[4])
			headerLen = 5 + adaptLen
		}
		if headerLen >= TSPacketSize {
			if current != nil {
				current.TSOffsets = append(current.TSOffsets, off)
			}
			continue
		}
		payload := pkt[headerLen:]

		if payloadStart {
			if current != nil {
				packets = append(packets, *current)
			}

			if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
				current = nil
				continue
			}

			pesHeaderDataLen := int(payload[8])
			pesHdrEnd := 9 + pesHeaderDataLen
			if pesHdrEnd > len(payload) {
				current = nil
				continue
			}

			current = &PESPacket{
				PESHdr:    append([]byte(nil), payload[:pesHdrEnd]...),
				ESData:    append([]byte(nil), payload[pesHdrEnd:]...),
				TSOffsets: []int{off},
			}
		} else if current != nil {
			current.ESData = append(current.ESData, payload...)
			current.TSOffsets = append(current.TSOffsets, off)
		}
	}
	if current != nil {
		packets = append(packets, *current)
	}
	return packets
}

// RebuildTS replaces all TS packets for videoPID in origData with freshly
// packetized versions built from pesPackets, preserving non-video packets
// in their original order.
func RebuildTS(origData []byte, pesPackets []PESPacket, videoPID uint16) []byte {
	var newVideoTS []byte
	cc := byte(0)
	for _, pp := range pesPackets {
		pesData := BuildPES(pp.PESHdr, pp.ESData)
		packets := Packetize(pesData, videoPID, &cc)
		newVideoTS = append(newVideoTS, packets...)
	}

	var result []byte
	newVidIdx := 0

	for off := 0; off+TSPacketSize <= len(origData); off += TSPacketSize {
		pkt := origData[off : off+TSPacketSize]
		if pkt[0] != 0x47 {
			result = append(result, pkt...)
			continue
		}
		pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
		if pid != videoPID {
			result = append(result, pkt...)
		} else {
			if newVidIdx+TSPacketSize <= len(newVideoTS) {
				result = append(result, newVideoTS[newVidIdx:newVidIdx+TSPacketSize]...)
				newVidIdx += TSPacketSize
			}
		}
	}

	for newVidIdx+TSPacketSize <= len(newVideoTS) {
		result = append(result, newVideoTS[newVidIdx:newVidIdx+TSPacketSize]...)
		newVidIdx += TSPacketSize
	}

	return result
}

// BuildPES reassembles a PES packet from its header and elementary stream
// data, updating the PES length field.
func BuildPES(pesHdr, esData []byte) []byte {
	pesLen := len(pesHdr) - 6 + len(esData)
	var pes []byte
	pes = append(pes, pesHdr...)
	if pesLen <= 0xFFFF {
		pes[4] = byte(pesLen >> 8)
		pes[5] = byte(pesLen)
	} else {
		pes[4] = 0
		pes[5] = 0
	}
	pes = append(pes, esData...)
	return pes
}

// Packetize splits pesData into 188-byte TS packets on the given PID,
// incrementing the continuity counter cc between packets.
func Packetize(pesData []byte, pid uint16, cc *byte) []byte {
	var result []byte
	offset := 0
	first := true

	for offset < len(pesData) {
		var pkt [TSPacketSize]byte
		pkt[0] = 0x47
		pkt[1] = byte(pid>>8) & 0x1F
		pkt[2] = byte(pid)
		if first {
			pkt[1] |= 0x40
			first = false
		}
		pkt[3] = 0x10 | (*cc & 0x0F)
		*cc = (*cc + 1) & 0x0F

		remaining := len(pesData) - offset
		capacity := TSPacketSize - 4

		if remaining < capacity {
			stuffLen := capacity - remaining
			if stuffLen == 1 {
				pkt[3] |= 0x20
				pkt[4] = 0
				copy(pkt[5:], pesData[offset:])
				offset = len(pesData)
			} else {
				pkt[3] |= 0x20
				pkt[4] = byte(stuffLen - 1)
				if stuffLen > 2 {
					pkt[5] = 0
					for i := 6; i < 4+stuffLen; i++ {
						pkt[i] = 0xFF
					}
				}
				copy(pkt[4+stuffLen:], pesData[offset:])
				offset = len(pesData)
			}
		} else {
			copy(pkt[4:], pesData[offset:offset+capacity])
			offset += capacity
		}

		result = append(result, pkt[:]...)
	}

	return result
}

// EncodeSEIMessage encodes an H.264 SEI message with the given payload type
// and payload bytes, using the multi-byte size encoding when needed.
func EncodeSEIMessage(payloadType int, payload []byte) []byte {
	var out []byte
	pt := payloadType
	for pt >= 255 {
		out = append(out, 0xFF)
		pt -= 255
	}
	out = append(out, byte(pt))

	ps := len(payload)
	for ps >= 255 {
		out = append(out, 0xFF)
		ps -= 255
	}
	out = append(out, byte(ps))
	out = append(out, payload...)
	return out
}

// AddEPB adds emulation prevention bytes per ITU-T H.264 spec:
// inserts 0x03 before any 0x00-0x03 byte that follows two consecutive 0x00
// bytes.
func AddEPB(data []byte) []byte {
	var out []byte
	zeroCount := 0
	for _, b := range data {
		if zeroCount >= 2 && b <= 0x03 {
			out = append(out, 0x03)
			zeroCount = 0
		}
		out = append(out, b)
		if b == 0x00 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	return out
}

// RemoveEPB removes emulation prevention bytes (0x00 0x00 0x03 -> 0x00 0x00).
func RemoveEPB(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if i+2 < len(data) && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x03 &&
			(i+3 >= len(data) || data[i+3] <= 0x03) {
			out = append(out, 0x00, 0x00)
			i += 2
		} else {
			out = append(out, data[i])
		}
	}
	return out
}

// CopyFile copies the file at src to dst, reading the entire file into memory.
func CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// FileExists returns true if the path exists (and is stat-able).
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
