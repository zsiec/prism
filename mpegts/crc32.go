package mpegts

import "fmt"

// MPEG-2 CRC32 with polynomial 0x04C11DB7.
var crc32Table [256]uint32

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
		crc32Table[i] = crc
	}
}

func computeCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ crc32Table[byte(crc>>24)^b]
	}
	return crc
}

func verifyCRC32(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("mpegts: data too short for CRC32")
	}
	if computeCRC32(data) != 0 {
		return fmt.Errorf("mpegts: CRC32 mismatch")
	}
	return nil
}
