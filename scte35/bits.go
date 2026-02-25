package scte35

// bitReader reads bits MSB-first from a byte slice.
type bitReader struct {
	data     []byte
	bitPos   int
	overflow bool
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (r *bitReader) bitsLeft() int {
	total := len(r.data) * 8
	if r.bitPos > total {
		return 0
	}
	return total - r.bitPos
}

func (r *bitReader) readBit() bool {
	if r.bitPos >= len(r.data)*8 {
		r.overflow = true
		return false
	}
	byteIdx := r.bitPos / 8
	bitIdx := 7 - (r.bitPos % 8)
	r.bitPos++
	return (r.data[byteIdx]>>uint(bitIdx))&1 == 1
}

func (r *bitReader) readUint32(n int) uint32 {
	var val uint32
	for i := 0; i < n; i++ {
		val <<= 1
		if r.readBit() {
			val |= 1
		}
	}
	return val
}

func (r *bitReader) readUint64(n int) uint64 {
	var val uint64
	for i := 0; i < n; i++ {
		val <<= 1
		if r.readBit() {
			val |= 1
		}
	}
	return val
}

func (r *bitReader) readBytes(n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(r.readUint32(8))
	}
	return out
}

func (r *bitReader) skip(n int) {
	r.bitPos += n
	if r.bitPos > len(r.data)*8 {
		r.overflow = true
	}
}

// bitWriter writes bits MSB-first into a byte slice.
type bitWriter struct {
	data   []byte
	bitPos int
}

func newBitWriter(size int) *bitWriter {
	return &bitWriter{data: make([]byte, size)}
}

func (w *bitWriter) putBit(v bool) {
	if w.bitPos >= len(w.data)*8 {
		return
	}
	if v {
		byteIdx := w.bitPos / 8
		bitIdx := 7 - (w.bitPos % 8)
		w.data[byteIdx] |= 1 << uint(bitIdx)
	}
	w.bitPos++
}

func (w *bitWriter) putUint32(n int, v uint32) {
	for i := n - 1; i >= 0; i-- {
		w.putBit((v>>uint(i))&1 == 1)
	}
}

func (w *bitWriter) putUint64(n int, v uint64) {
	for i := n - 1; i >= 0; i-- {
		w.putBit((v>>uint(i))&1 == 1)
	}
}

func (w *bitWriter) putBytes(b []byte) {
	for _, v := range b {
		w.putUint32(8, uint32(v))
	}
}

func (w *bitWriter) bytes() []byte {
	return w.data
}
