// Package mpegts implements MPEG-TS demuxing for transport stream parsing.
// It supports PAT/PMT discovery, PES reassembly with PTS/DTS extraction,
// and a custom packet parser callback for intercepting raw packet data.
package mpegts

// Packet is a parsed 188-byte MPEG-TS transport stream packet.
type Packet struct {
	Header  PacketHeader
	Payload []byte
}

// PacketHeader contains the parsed header fields of a transport stream packet.
type PacketHeader struct {
	PID                       uint16
	ContinuityCounter         uint8
	HasAdaptationField        bool
	HasPayload                bool
	PayloadUnitStartIndicator bool
	TransportErrorIndicator   bool
	DiscontinuityIndicator    bool
}

// DemuxerData is the output of the demuxer for each logical unit (PAT, PMT,
// or PES packet). Exactly one of PAT, PMT, or PES will be non-nil.
type DemuxerData struct {
	FirstPacket *Packet
	PAT         *PATData
	PMT         *PMTData
	PES         *PESData
}

// PATData contains the parsed Program Association Table.
type PATData struct {
	Programs []*PATProgram
}

// PATProgram maps a program number to its PMT PID.
type PATProgram struct {
	ProgramMapID  uint16
	ProgramNumber uint16
}

// PMTData contains the parsed Program Map Table.
type PMTData struct {
	ElementaryStreams []*PMTElementaryStream
}

// PMTElementaryStream describes a single elementary stream in a PMT.
type PMTElementaryStream struct {
	ElementaryPID uint16
	StreamType    uint8
}

// PESData contains a reassembled Packetized Elementary Stream.
type PESData struct {
	Data   []byte
	Header *PESHeader
}

// PESHeader contains the parsed PES packet header.
type PESHeader struct {
	OptionalHeader *PESOptionalHeader
	StreamID       uint8
}

// PESOptionalHeader carries optional PES fields including timestamps.
type PESOptionalHeader struct {
	PTS *ClockReference
	DTS *ClockReference
}

// ClockReference holds a 33-bit MPEG-TS timestamp base value (90 kHz clock).
type ClockReference struct {
	Base int64
}

// PacketsParser is a callback invoked with accumulated packets for a PID
// before standard parsing. If skip is true, the demuxer skips its own
// parsing for those packets.
type PacketsParser func(ps []*Packet) (ds []*DemuxerData, skip bool, err error)
