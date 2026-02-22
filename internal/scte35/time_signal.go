package scte35

// TimeSignal provides a time-synchronized data delivery mechanism.
type TimeSignal struct {
	SpliceTime SpliceTime
}

func (cmd *TimeSignal) Type() uint32 { return TimeSignalType }

func (cmd *TimeSignal) decode(data []byte) error {
	r := newBitReader(data)
	timeSpecifiedFlag := r.readBit()
	if timeSpecifiedFlag {
		r.skip(6) // reserved
		pts := r.readUint64(33)
		cmd.SpliceTime.PTSTime = &pts
	} else {
		r.skip(7) // reserved
	}
	return nil
}

func (cmd *TimeSignal) encode() ([]byte, error) {
	if cmd.SpliceTime.PTSTime != nil {
		w := newBitWriter(5)
		w.putBit(true)
		w.putUint32(6, 0x3F) // reserved
		w.putUint64(33, *cmd.SpliceTime.PTSTime)
		return w.bytes(), nil
	}
	w := newBitWriter(1)
	w.putBit(false)
	w.putUint32(7, 0x7F) // reserved
	return w.bytes(), nil
}

func (cmd *TimeSignal) commandLength() int {
	if cmd.SpliceTime.PTSTime != nil {
		return 5
	}
	return 1
}
