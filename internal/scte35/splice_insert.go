package scte35

// SpliceInsert signals a splice point in the stream.
type SpliceInsert struct {
	SpliceEventID              uint32
	SpliceEventCancelIndicator bool
	OutOfNetworkIndicator      bool
	SpliceImmediateFlag        bool
	BreakDuration              *BreakDuration
	UniqueProgramID            uint32
	AvailNum                   uint32
	AvailsExpected             uint32
}

func (cmd *SpliceInsert) Type() uint32 { return SpliceInsertType }

func (cmd *SpliceInsert) decode(data []byte) error {
	r := newBitReader(data)
	cmd.SpliceEventID = r.readUint32(32)
	cmd.SpliceEventCancelIndicator = r.readBit()
	r.skip(7) // reserved

	if !cmd.SpliceEventCancelIndicator {
		cmd.OutOfNetworkIndicator = r.readBit()
		programSpliceFlag := r.readBit()
		durationFlag := r.readBit()
		cmd.SpliceImmediateFlag = r.readBit()
		r.skip(4) // reserved

		if programSpliceFlag {
			if !cmd.SpliceImmediateFlag {
				timeSpecifiedFlag := r.readBit()
				if timeSpecifiedFlag {
					r.skip(6)  // reserved
					r.skip(33) // pts_time (not stored)
				} else {
					r.skip(7) // reserved
				}
			}
		} else {
			componentCount := int(r.readUint32(8))
			for i := 0; i < componentCount; i++ {
				r.skip(8) // component_tag
				if !cmd.SpliceImmediateFlag {
					tsf := r.readBit()
					if tsf {
						r.skip(6)  // reserved
						r.skip(33) // pts_time
					} else {
						r.skip(7) // reserved
					}
				}
			}
		}

		if durationFlag {
			cmd.BreakDuration = &BreakDuration{}
			cmd.BreakDuration.AutoReturn = r.readBit()
			r.skip(6) // reserved
			cmd.BreakDuration.Duration = r.readUint64(33)
		}
	}
	cmd.UniqueProgramID = r.readUint32(16)
	cmd.AvailNum = r.readUint32(8)
	cmd.AvailsExpected = r.readUint32(8)
	return nil
}

func (cmd *SpliceInsert) encode() ([]byte, error) {
	length := cmd.commandLength()
	w := newBitWriter(length)

	w.putUint32(32, cmd.SpliceEventID)
	w.putBit(cmd.SpliceEventCancelIndicator)
	w.putUint32(7, 0x7F) // reserved

	if !cmd.SpliceEventCancelIndicator {
		w.putBit(cmd.OutOfNetworkIndicator)
		w.putBit(false) // program_splice_flag = 0 (component mode with 0 components)
		w.putBit(cmd.BreakDuration != nil)
		w.putBit(cmd.SpliceImmediateFlag)
		w.putUint32(4, 0x0F) // reserved

		w.putUint32(8, 0) // component_count = 0

		if cmd.BreakDuration != nil {
			w.putBit(cmd.BreakDuration.AutoReturn)
			w.putUint32(6, 0x3F) // reserved
			w.putUint64(33, cmd.BreakDuration.Duration)
		}
		w.putUint32(16, cmd.UniqueProgramID)
		w.putUint32(8, cmd.AvailNum)
		w.putUint32(8, cmd.AvailsExpected)
	}

	return w.bytes(), nil
}

func (cmd *SpliceInsert) commandLength() int {
	bits := 32 + 1 + 7 // event_id + cancel + reserved

	if !cmd.SpliceEventCancelIndicator {
		bits += 1 + 1 + 1 + 1 + 4 // out_of_network + program_splice + duration_flag + immediate + reserved
		bits += 8                 // component_count (program_splice_flag=0)

		if cmd.BreakDuration != nil {
			bits += 1 + 6 + 33 // auto_return + reserved + duration
		}
		bits += 16 + 8 + 8 // unique_program_id + avail_num + avails_expected
	}
	return bits / 8
}
