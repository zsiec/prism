package scte35

import "fmt"

// MPEG-2 CRC32 with polynomial 0x04C11DB7.
var crcTable [256]uint32

func init() {
	for i := 0; i < 256; i++ {
		crc := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
		crcTable[i] = crc
	}
}

func crc32MPEG2(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ crcTable[byte(crc>>24)^b]
	}
	return crc
}

// verifyCRC32 checks that the last 4 bytes of data are the CRC32 of the preceding bytes.
func verifyCRC32(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("scte35: data too short for CRC verification")
	}
	computed := crc32MPEG2(data[:len(data)-4])
	stored := uint32(data[len(data)-4])<<24 |
		uint32(data[len(data)-3])<<16 |
		uint32(data[len(data)-2])<<8 |
		uint32(data[len(data)-1])
	if computed != stored {
		return fmt.Errorf("scte35: CRC32 mismatch: computed 0x%08X, stored 0x%08X", computed, stored)
	}
	return nil
}
