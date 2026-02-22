package scte35

import (
	"testing"
)

func TestBitReaderSingleBits(t *testing.T) {
	t.Parallel()
	r := newBitReader([]byte{0xA5}) // 10100101
	expected := []bool{true, false, true, false, false, true, false, true}
	for i, want := range expected {
		got := r.readBit()
		if got != want {
			t.Errorf("bit %d: got %v, want %v", i, got, want)
		}
	}
	if r.bitsLeft() != 0 {
		t.Errorf("bitsLeft: got %d, want 0", r.bitsLeft())
	}
}

func TestBitReaderUint32(t *testing.T) {
	t.Parallel()
	r := newBitReader([]byte{0xAB, 0xCD})
	got := r.readUint32(12)
	if got != 0xABC {
		t.Errorf("readUint32(12): got 0x%X, want 0xABC", got)
	}
	got = r.readUint32(4)
	if got != 0xD {
		t.Errorf("readUint32(4): got 0x%X, want 0xD", got)
	}
}

func TestBitReaderUint64(t *testing.T) {
	t.Parallel()
	// 33-bit value: 0x1FFFFFFFF = all ones
	r := newBitReader([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x80})
	got := r.readUint64(33)
	if got != 0x1FFFFFFFF {
		t.Errorf("readUint64(33): got 0x%X, want 0x1FFFFFFFF", got)
	}
}

func TestBitReaderBytes(t *testing.T) {
	t.Parallel()
	r := newBitReader([]byte{0x01, 0x02, 0x03, 0x04})
	r.skip(8)
	got := r.readBytes(2)
	if got[0] != 0x02 || got[1] != 0x03 {
		t.Errorf("readBytes: got %v, want [0x02, 0x03]", got)
	}
}

func TestBitReaderOverflow(t *testing.T) {
	t.Parallel()
	r := newBitReader([]byte{0xFF})
	r.skip(8)
	r.readBit()
	if !r.overflow {
		t.Error("expected overflow after reading past end")
	}
}

func TestBitWriterSingleBits(t *testing.T) {
	t.Parallel()
	w := newBitWriter(1)
	bits := []bool{true, false, true, false, false, true, false, true}
	for _, b := range bits {
		w.putBit(b)
	}
	if w.bytes()[0] != 0xA5 {
		t.Errorf("got 0x%02X, want 0xA5", w.bytes()[0])
	}
}

func TestBitWriterUint32(t *testing.T) {
	t.Parallel()
	w := newBitWriter(2)
	w.putUint32(12, 0xABC)
	w.putUint32(4, 0xD)
	if w.bytes()[0] != 0xAB || w.bytes()[1] != 0xCD {
		t.Errorf("got %02X %02X, want AB CD", w.bytes()[0], w.bytes()[1])
	}
}

func TestBitWriterUint64(t *testing.T) {
	t.Parallel()
	w := newBitWriter(5)
	w.putUint64(33, 0x1FFFFFFFF)
	w.putUint64(7, 0) // pad remaining bits
	expected := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x80}
	for i, want := range expected {
		if w.bytes()[i] != want {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, w.bytes()[i], want)
		}
	}
}

func TestBitWriterBytes(t *testing.T) {
	t.Parallel()
	w := newBitWriter(4)
	w.putUint32(8, 0x01)
	w.putBytes([]byte{0x02, 0x03})
	w.putUint32(8, 0x04)
	expected := []byte{0x01, 0x02, 0x03, 0x04}
	for i, want := range expected {
		if w.bytes()[i] != want {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, w.bytes()[i], want)
		}
	}
}

func TestBitRoundTrip(t *testing.T) {
	t.Parallel()
	w := newBitWriter(8)
	w.putUint32(8, 0xFC)
	w.putBit(false)
	w.putBit(false)
	w.putUint32(2, 3)
	w.putUint32(12, 0x123)
	w.putUint64(33, 900000)
	w.putUint32(7, 0) // padding

	r := newBitReader(w.bytes())
	if got := r.readUint32(8); got != 0xFC {
		t.Errorf("got 0x%X, want 0xFC", got)
	}
	if got := r.readBit(); got != false {
		t.Errorf("got %v, want false", got)
	}
	if got := r.readBit(); got != false {
		t.Errorf("got %v, want false", got)
	}
	if got := r.readUint32(2); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if got := r.readUint32(12); got != 0x123 {
		t.Errorf("got 0x%X, want 0x123", got)
	}
	if got := r.readUint64(33); got != 900000 {
		t.Errorf("got %d, want 900000", got)
	}
}

func TestBitReaderSkip(t *testing.T) {
	t.Parallel()
	r := newBitReader([]byte{0xFF, 0x00, 0xAB})
	r.skip(16)
	if got := r.readUint32(8); got != 0xAB {
		t.Errorf("got 0x%02X, want 0xAB", got)
	}
}
