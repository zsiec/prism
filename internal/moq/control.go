package moq

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/quic-go/quic-go/quicvarint"
)

// MoQ Transport draft-15 message type IDs.
const (
	MsgSubscribe      uint64 = 0x03
	MsgSubscribeOK    uint64 = 0x04
	MsgSubscribeError uint64 = 0x05
	MsgUnsubscribe    uint64 = 0x0a
	MsgGoAway         uint64 = 0x10
	MsgMaxRequestID   uint64 = 0x15
	MsgClientSetup    uint64 = 0x20
	MsgServerSetup    uint64 = 0x21
)

// Version is the MoQ Transport version: draft-15 uses 0xff000000 + draft number.
const Version uint64 = 0xff00000f

// Setup parameter keys (draft-15 §6.2).
const (
	ParamPath         uint64 = 0x01 // odd → length-prefixed byte string
	ParamMaxRequestID uint64 = 0x02 // even → varint value
)

// Subscribe filter types (draft-15 §6.6).
const (
	FilterNextGroupStart uint64 = 0x01
	FilterLatestObject   uint64 = 0x02
	FilterAbsoluteStart  uint64 = 0x03
	FilterAbsoluteRange  uint64 = 0x04
)

// Group order values (draft-15 §6.6).
const (
	GroupOrderDefault    byte = 0x00
	GroupOrderAscending  byte = 0x01
	GroupOrderDescending byte = 0x02
)

// ClientSetup is the first message sent by a MoQ client.
type ClientSetup struct {
	Versions     []uint64
	Path         string
	MaxRequestID uint64
	HasPath      bool
}

// ServerSetup is the response to a ClientSetup.
type ServerSetup struct {
	SelectedVersion uint64
	MaxRequestID    uint64
}

// Subscribe requests delivery of a track.
type Subscribe struct {
	RequestID  uint64
	Namespace  []string
	TrackName  string
	Priority   byte
	GroupOrder byte
	Forward    byte
	FilterType uint64
	StartGroup uint64 // only for AbsoluteStart / AbsoluteRange
	StartObj   uint64 // only for AbsoluteStart / AbsoluteRange
	EndGroup   uint64 // only for AbsoluteRange
}

// SubscribeOK confirms a subscription.
type SubscribeOK struct {
	RequestID     uint64
	TrackAlias    uint64
	Expires       uint64
	GroupOrder    byte
	ContentExists bool
	LargestGroup  uint64 // only when ContentExists
	LargestObj    uint64 // only when ContentExists
}

// SubscribeError rejects a subscription.
type SubscribeError struct {
	RequestID    uint64
	ErrorCode    uint64
	ReasonPhrase string
}

// Unsubscribe cancels a subscription.
type Unsubscribe struct {
	RequestID uint64
}

// MaxRequestIDMsg updates the peer's request ID quota.
type MaxRequestIDMsg struct {
	RequestID uint64
}

// GoAway signals a graceful session shutdown.
type GoAway struct {
	NewSessionURI string
}

// ReadControlMsg reads a MoQ control message from the control stream.
// Wire format: [message_type (varint)] [message_length (uint16 big-endian)] [payload].
func ReadControlMsg(r io.Reader) (uint64, []byte, error) {
	br, ok := r.(io.ByteReader)
	if !ok {
		br = bufio.NewReader(r)
		r = br.(io.Reader)
	}
	msgType, err := quicvarint.Read(br)
	if err != nil {
		return 0, nil, fmt.Errorf("read message type: %w", err)
	}

	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return 0, nil, fmt.Errorf("read message length: %w", err)
	}
	length := binary.BigEndian.Uint16(lenBuf[:])

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, fmt.Errorf("read message payload: %w", err)
		}
	}

	return msgType, payload, nil
}

// WriteControlMsg writes a MoQ control message to the control stream as a
// single Write call to ensure atomicity even without external synchronization.
func WriteControlMsg(w io.Writer, msgType uint64, payload []byte) error {
	var buf []byte
	buf = quicvarint.Append(buf, msgType)

	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(payload)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, payload...)

	_, err := w.Write(buf)
	return err
}

// ParseClientSetup parses a CLIENT_SETUP payload.
func ParseClientSetup(data []byte) (ClientSetup, error) {
	r := newBufReader(data)
	var cs ClientSetup

	numVersions, err := r.readVarint()
	if err != nil {
		return cs, &ParseError{Field: "num_versions", Err: err}
	}

	cs.Versions = make([]uint64, numVersions)
	for i := uint64(0); i < numVersions; i++ {
		v, err := r.readVarint()
		if err != nil {
			return cs, &ParseError{Field: "version", Err: err}
		}
		cs.Versions[i] = v
	}

	numParams, err := r.readVarint()
	if err != nil {
		return cs, &ParseError{Field: "num_params", Err: err}
	}

	for i := uint64(0); i < numParams; i++ {
		key, err := r.readVarint()
		if err != nil {
			return cs, &ParseError{Field: "param_key", Err: err}
		}

		if key%2 == 1 {
			// Odd key: length-prefixed byte string
			val, err := r.readVarIntBytes()
			if err != nil {
				return cs, &ParseError{Field: "param_value", Err: err}
			}
			if key == ParamPath {
				cs.Path = string(val)
				cs.HasPath = true
			}
		} else {
			// Even key: varint value
			val, err := r.readVarint()
			if err != nil {
				return cs, &ParseError{Field: "param_value", Err: err}
			}
			if key == ParamMaxRequestID {
				cs.MaxRequestID = val
			}
		}
	}

	return cs, nil
}

// ParseSubscribe parses a SUBSCRIBE payload.
func ParseSubscribe(data []byte) (Subscribe, error) {
	r := newBufReader(data)
	var s Subscribe

	var err error
	s.RequestID, err = r.readVarint()
	if err != nil {
		return s, &ParseError{Field: "request_id", Err: err}
	}

	s.Namespace, err = parseNamespaceTuple(r)
	if err != nil {
		return s, &ParseError{Field: "namespace", Err: err}
	}

	trackNameBytes, err := r.readVarIntBytes()
	if err != nil {
		return s, &ParseError{Field: "track_name", Err: err}
	}
	s.TrackName = string(trackNameBytes)

	priority, err := r.readByte()
	if err != nil {
		return s, &ParseError{Field: "priority", Err: err}
	}
	s.Priority = priority

	groupOrder, err := r.readByte()
	if err != nil {
		return s, &ParseError{Field: "group_order", Err: err}
	}
	s.GroupOrder = groupOrder

	forward, err := r.readByte()
	if err != nil {
		return s, &ParseError{Field: "forward", Err: err}
	}
	s.Forward = forward

	s.FilterType, err = r.readVarint()
	if err != nil {
		return s, &ParseError{Field: "filter_type", Err: err}
	}

	switch s.FilterType {
	case FilterAbsoluteStart:
		s.StartGroup, err = r.readVarint()
		if err != nil {
			return s, fmt.Errorf("read start group: %w", err)
		}
		s.StartObj, err = r.readVarint()
		if err != nil {
			return s, fmt.Errorf("read start object: %w", err)
		}
	case FilterAbsoluteRange:
		s.StartGroup, err = r.readVarint()
		if err != nil {
			return s, fmt.Errorf("read start group: %w", err)
		}
		s.StartObj, err = r.readVarint()
		if err != nil {
			return s, fmt.Errorf("read start object: %w", err)
		}
		s.EndGroup, err = r.readVarint()
		if err != nil {
			return s, fmt.Errorf("read end group: %w", err)
		}
	}

	// Skip remaining params (NumParams + KVPs) — we don't need them.
	return s, nil
}

// ParseUnsubscribe parses an UNSUBSCRIBE payload.
func ParseUnsubscribe(data []byte) (Unsubscribe, error) {
	r := newBufReader(data)
	reqID, err := r.readVarint()
	if err != nil {
		return Unsubscribe{}, &ParseError{Field: "request_id", Err: err}
	}
	return Unsubscribe{RequestID: reqID}, nil
}

// SerializeServerSetup serializes a SERVER_SETUP payload.
func SerializeServerSetup(ss ServerSetup) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, ss.SelectedVersion)
	// NumParams = 1 (MAX_REQUEST_ID)
	buf = quicvarint.Append(buf, 1)
	buf = quicvarint.Append(buf, ParamMaxRequestID)
	buf = quicvarint.Append(buf, ss.MaxRequestID)
	return buf
}

// SerializeSubscribeOK serializes a SUBSCRIBE_OK payload.
func SerializeSubscribeOK(sok SubscribeOK) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, sok.RequestID)
	buf = quicvarint.Append(buf, sok.TrackAlias)
	buf = quicvarint.Append(buf, sok.Expires)
	buf = append(buf, sok.GroupOrder)

	if sok.ContentExists {
		buf = append(buf, 1)
		buf = quicvarint.Append(buf, sok.LargestGroup)
		buf = quicvarint.Append(buf, sok.LargestObj)
	} else {
		buf = append(buf, 0)
	}

	// NumParams = 0
	buf = quicvarint.Append(buf, 0)
	return buf
}

// SerializeSubscribeError serializes a SUBSCRIBE_ERROR payload.
func SerializeSubscribeError(se SubscribeError) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, se.RequestID)
	buf = quicvarint.Append(buf, se.ErrorCode)
	buf = appendVarIntBytes(buf, []byte(se.ReasonPhrase))
	return buf
}

// SerializeGoAway serializes a GOAWAY payload.
func SerializeGoAway(ga GoAway) []byte {
	var buf []byte
	buf = appendVarIntBytes(buf, []byte(ga.NewSessionURI))
	return buf
}

// SerializeMaxRequestID serializes a MAX_REQUEST_ID payload.
func SerializeMaxRequestID(reqID uint64) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, reqID)
	return buf
}

// parseNamespaceTuple reads a namespace tuple: [count(i)] [len(i) bytes]...
func parseNamespaceTuple(r *bufReader) ([]string, error) {
	count, err := r.readVarint()
	if err != nil {
		return nil, fmt.Errorf("read tuple count: %w", err)
	}

	parts := make([]string, count)
	for i := uint64(0); i < count; i++ {
		b, err := r.readVarIntBytes()
		if err != nil {
			return nil, fmt.Errorf("read tuple element %d: %w", i, err)
		}
		parts[i] = string(b)
	}
	return parts, nil
}

// AppendNamespaceTuple appends a namespace tuple to buf.
func AppendNamespaceTuple(buf []byte, parts []string) []byte {
	buf = quicvarint.Append(buf, uint64(len(parts)))
	for _, p := range parts {
		buf = appendVarIntBytes(buf, []byte(p))
	}
	return buf
}

// appendVarIntBytes appends a varint-length-prefixed byte string to buf.
func appendVarIntBytes(buf []byte, data []byte) []byte {
	buf = quicvarint.Append(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

// bufReader wraps a byte slice for sequential varint/byte reading.
type bufReader struct {
	data []byte
	pos  int
}

func newBufReader(data []byte) *bufReader {
	return &bufReader{data: data}
}

func (b *bufReader) readVarint() (uint64, error) {
	if b.pos >= len(b.data) {
		return 0, io.ErrUnexpectedEOF
	}
	val, n, err := quicvarint.Parse(b.data[b.pos:])
	if err != nil {
		return 0, err
	}
	b.pos += n
	return val, nil
}

func (b *bufReader) readByte() (byte, error) {
	if b.pos >= len(b.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := b.data[b.pos]
	b.pos++
	return v, nil
}

func (b *bufReader) readVarIntBytes() ([]byte, error) {
	length, err := b.readVarint()
	if err != nil {
		return nil, err
	}
	end := b.pos + int(length)
	if end > len(b.data) {
		return nil, io.ErrUnexpectedEOF
	}
	val := b.data[b.pos:end]
	b.pos = end
	return val, nil
}
