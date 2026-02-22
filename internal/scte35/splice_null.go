package scte35

// SpliceNull is a no-op command used as a heartbeat.
type SpliceNull struct{}

func (cmd *SpliceNull) Type() uint32 { return SpliceNullType }

func (cmd *SpliceNull) decode(_ []byte) error { return nil }

func (cmd *SpliceNull) encode() ([]byte, error) { return nil, nil }

func (cmd *SpliceNull) commandLength() int { return 0 }
