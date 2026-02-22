package mpegts

import (
	"context"
	"errors"
	"io"
)

// Demuxer reads MPEG-TS packets from a reader and produces DemuxerData
// containing parsed PAT, PMT, and PES payloads.
type Demuxer struct {
	ctx           context.Context
	reader        io.Reader
	readBuf       []byte
	pool          *packetPool
	programMap    *programMap
	dataBuffer    []*DemuxerData
	packetsParser PacketsParser
	pktSize       int
	eof           bool
	eofData       []*DemuxerData
}

// NewDemuxer creates a new MPEG-TS demuxer reading from r.
func NewDemuxer(ctx context.Context, r io.Reader, opts ...func(*Demuxer)) *Demuxer {
	pm := newProgramMap()
	d := &Demuxer{
		ctx:        ctx,
		reader:     r,
		pktSize:    packetSize,
		programMap: pm,
		pool:       newPacketPool(pm),
	}
	for _, opt := range opts {
		opt(d)
	}
	d.readBuf = make([]byte, d.pktSize)
	return d
}

// DemuxerOptPacketSize sets the TS packet size (default 188).
func DemuxerOptPacketSize(size int) func(*Demuxer) {
	return func(d *Demuxer) {
		d.pktSize = size
	}
}

// DemuxerOptPacketsParser sets a custom packet parser callback.
func DemuxerOptPacketsParser(p PacketsParser) func(*Demuxer) {
	return func(d *Demuxer) {
		d.packetsParser = p
	}
}

// NextData returns the next parsed unit from the stream. Returns io.EOF
// when all data has been consumed.
func (d *Demuxer) NextData() (*DemuxerData, error) {
	for {
		// Drain buffered results first.
		if len(d.dataBuffer) > 0 {
			data := d.dataBuffer[0]
			d.dataBuffer = d.dataBuffer[1:]
			return data, nil
		}

		// Drain EOF results.
		if d.eof {
			if len(d.eofData) > 0 {
				data := d.eofData[0]
				d.eofData = d.eofData[1:]
				return data, nil
			}
			return nil, io.EOF
		}

		// Check context.
		if d.ctx.Err() != nil {
			return nil, d.ctx.Err()
		}

		// Read next packet.
		_, err := io.ReadFull(d.reader, d.readBuf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				d.eof = true
				d.drainPool()
				continue
			}
			return nil, err
		}

		pkt, err := parsePacket(d.readBuf)
		if err != nil {
			continue // skip corrupt packets
		}

		flushed := d.pool.add(pkt)
		if flushed == nil {
			continue
		}

		results, err := d.processPackets(flushed)
		if err != nil {
			continue // skip corrupt sections
		}
		if len(results) == 0 {
			continue
		}

		// Update program map from PAT results.
		for _, r := range results {
			if r.PAT != nil {
				for _, p := range r.PAT.Programs {
					d.programMap.addPMTPID(p.ProgramMapID)
				}
			}
		}

		d.dataBuffer = results[1:]
		return results[0], nil
	}
}

func (d *Demuxer) drainPool() {
	for _, packets := range d.pool.dump() {
		results, err := d.processPackets(packets)
		if err != nil {
			continue
		}
		// Update program map from PAT results so subsequent PMT
		// PIDs are recognized as PSI during drain.
		for _, r := range results {
			if r.PAT != nil {
				for _, p := range r.PAT.Programs {
					d.programMap.addPMTPID(p.ProgramMapID)
				}
			}
		}
		d.eofData = append(d.eofData, results...)
	}
}

func (d *Demuxer) processPackets(packets []*Packet) ([]*DemuxerData, error) {
	if len(packets) == 0 {
		return nil, nil
	}

	firstPacket := packets[0]
	pid := firstPacket.Header.PID

	// Custom parser callback.
	if d.packetsParser != nil {
		ds, skip, err := d.packetsParser(packets)
		if err != nil {
			return nil, err
		}
		if skip {
			return ds, nil
		}
	}

	// Concatenate payloads.
	var payload []byte
	for _, p := range packets {
		payload = append(payload, p.Payload...)
	}
	if len(payload) == 0 {
		return nil, nil
	}

	// Route to appropriate parser.
	if isPSIPayload(pid, d.programMap) {
		return parsePSI(payload, pid, firstPacket, d.programMap)
	}

	if isPESPayload(payload) {
		pes, err := parsePES(payload)
		if err != nil {
			return nil, err
		}
		return []*DemuxerData{{
			FirstPacket: firstPacket,
			PES:         pes,
		}}, nil
	}

	return nil, nil
}
