package moq

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/quic-go/quic-go/quicvarint"
)

func TestControlMsgRoundTrip(t *testing.T) {
	t.Parallel()
	payload := []byte("hello")
	var buf bytes.Buffer
	if err := WriteControlMsg(&buf, MsgClientSetup, payload); err != nil {
		t.Fatal(err)
	}

	msgType, got, err := ReadControlMsg(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != MsgClientSetup {
		t.Fatalf("message type = %#x, want %#x", msgType, MsgClientSetup)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

func TestControlMsgEmptyPayload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteControlMsg(&buf, MsgGoAway, nil); err != nil {
		t.Fatal(err)
	}

	msgType, got, err := ReadControlMsg(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != MsgGoAway {
		t.Fatalf("message type = %#x, want %#x", msgType, MsgGoAway)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(got))
	}
}

func TestControlMsgTruncatedType(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	_, _, err := ReadControlMsg(&buf)
	if err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestControlMsgTruncatedLength(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.Write(quicvarint.Append(nil, MsgClientSetup))
	// Only 1 byte of the 2-byte length
	buf.WriteByte(0x00)

	_, _, err := ReadControlMsg(&buf)
	if err == nil {
		t.Fatal("expected error on truncated length")
	}
}

func TestControlMsgTruncatedPayload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.Write(quicvarint.Append(nil, MsgClientSetup))
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], 10)
	buf.Write(lenBuf[:])
	buf.Write([]byte{1, 2, 3}) // only 3 of 10 bytes

	_, _, err := ReadControlMsg(&buf)
	if err == nil {
		t.Fatal("expected error on truncated payload")
	}
}

func buildClientSetupPayload(versions []uint64, path string, maxReqID uint64) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, uint64(len(versions)))
	for _, v := range versions {
		buf = quicvarint.Append(buf, v)
	}

	numParams := 0
	if path != "" {
		numParams++
	}
	if maxReqID > 0 {
		numParams++
	}
	buf = quicvarint.Append(buf, uint64(numParams))

	if path != "" {
		buf = quicvarint.Append(buf, ParamPath)
		buf = appendVarIntBytes(buf, []byte(path))
	}
	if maxReqID > 0 {
		buf = quicvarint.Append(buf, ParamMaxRequestID)
		buf = quicvarint.Append(buf, maxReqID)
	}

	return buf
}

func TestParseClientSetupSingleVersion(t *testing.T) {
	t.Parallel()
	payload := buildClientSetupPayload([]uint64{Version}, "/moq", 0)
	cs, err := ParseClientSetup(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Versions) != 1 || cs.Versions[0] != Version {
		t.Fatalf("versions = %v, want [%#x]", cs.Versions, Version)
	}
	if !cs.HasPath || cs.Path != "/moq" {
		t.Fatalf("path = %q (has=%v), want /moq", cs.Path, cs.HasPath)
	}
}

func TestParseClientSetupMultiVersion(t *testing.T) {
	t.Parallel()
	versions := []uint64{0xff00000e, Version, 0xff000010}
	payload := buildClientSetupPayload(versions, "", 100)
	cs, err := ParseClientSetup(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(cs.Versions))
	}
	if cs.HasPath {
		t.Fatal("expected no path")
	}
	if cs.MaxRequestID != 100 {
		t.Fatalf("maxRequestID = %d, want 100", cs.MaxRequestID)
	}
}

func TestParseClientSetupNoParams(t *testing.T) {
	t.Parallel()
	payload := buildClientSetupPayload([]uint64{Version}, "", 0)
	cs, err := ParseClientSetup(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cs.HasPath {
		t.Fatal("expected no path")
	}
	if cs.MaxRequestID != 0 {
		t.Fatalf("maxRequestID = %d, want 0", cs.MaxRequestID)
	}
}

func TestParseClientSetupTruncated(t *testing.T) {
	t.Parallel()
	// Just a single byte — not enough for num_versions
	_, err := ParseClientSetup([]byte{})
	if err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestSerializeServerSetup(t *testing.T) {
	t.Parallel()
	ss := ServerSetup{SelectedVersion: Version, MaxRequestID: 50}
	payload := SerializeServerSetup(ss)

	r := newBufReader(payload)
	ver, err := r.readVarint()
	if err != nil {
		t.Fatal(err)
	}
	if ver != Version {
		t.Fatalf("version = %#x, want %#x", ver, Version)
	}

	numParams, err := r.readVarint()
	if err != nil {
		t.Fatal(err)
	}
	if numParams != 1 {
		t.Fatalf("numParams = %d, want 1", numParams)
	}

	key, err := r.readVarint()
	if err != nil {
		t.Fatal(err)
	}
	if key != ParamMaxRequestID {
		t.Fatalf("param key = %#x, want %#x", key, ParamMaxRequestID)
	}

	val, err := r.readVarint()
	if err != nil {
		t.Fatal(err)
	}
	if val != 50 {
		t.Fatalf("max request ID = %d, want 50", val)
	}
}

func buildSubscribePayload(reqID uint64, ns []string, trackName string, filterType uint64) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, reqID)
	buf = AppendNamespaceTuple(buf, ns)
	buf = appendVarIntBytes(buf, []byte(trackName))
	buf = append(buf, 128)                  // priority
	buf = append(buf, GroupOrderDescending) // group order
	buf = append(buf, 0)                    // forward
	buf = quicvarint.Append(buf, filterType)

	switch filterType {
	case FilterAbsoluteStart:
		buf = quicvarint.Append(buf, 10) // start group
		buf = quicvarint.Append(buf, 5)  // start object
	case FilterAbsoluteRange:
		buf = quicvarint.Append(buf, 10) // start group
		buf = quicvarint.Append(buf, 5)  // start object
		buf = quicvarint.Append(buf, 20) // end group
	}

	// NumParams = 0
	buf = quicvarint.Append(buf, 0)
	return buf
}

func TestParseSubscribeNextGroupStart(t *testing.T) {
	t.Parallel()
	payload := buildSubscribePayload(1, []string{"prism", "test"}, "video", FilterNextGroupStart)
	s, err := ParseSubscribe(payload)
	if err != nil {
		t.Fatal(err)
	}
	if s.RequestID != 1 {
		t.Fatalf("requestID = %d, want 1", s.RequestID)
	}
	if len(s.Namespace) != 2 || s.Namespace[0] != "prism" || s.Namespace[1] != "test" {
		t.Fatalf("namespace = %v", s.Namespace)
	}
	if s.TrackName != "video" {
		t.Fatalf("trackName = %q", s.TrackName)
	}
	if s.FilterType != FilterNextGroupStart {
		t.Fatalf("filterType = %d, want %d", s.FilterType, FilterNextGroupStart)
	}
	if s.Priority != 128 {
		t.Fatalf("priority = %d, want 128", s.Priority)
	}
	if s.GroupOrder != GroupOrderDescending {
		t.Fatalf("groupOrder = %d, want %d", s.GroupOrder, GroupOrderDescending)
	}
}

func TestParseSubscribeLatestObject(t *testing.T) {
	t.Parallel()
	payload := buildSubscribePayload(2, []string{"prism", "live"}, "audio0", FilterLatestObject)
	s, err := ParseSubscribe(payload)
	if err != nil {
		t.Fatal(err)
	}
	if s.RequestID != 2 {
		t.Fatalf("requestID = %d, want 2", s.RequestID)
	}
	if s.TrackName != "audio0" {
		t.Fatalf("trackName = %q", s.TrackName)
	}
	if s.FilterType != FilterLatestObject {
		t.Fatalf("filterType = %d, want %d", s.FilterType, FilterLatestObject)
	}
}

func TestParseSubscribeAbsoluteStart(t *testing.T) {
	t.Parallel()
	payload := buildSubscribePayload(3, []string{"prism", "test"}, "video", FilterAbsoluteStart)
	s, err := ParseSubscribe(payload)
	if err != nil {
		t.Fatal(err)
	}
	if s.StartGroup != 10 || s.StartObj != 5 {
		t.Fatalf("start location = (%d, %d), want (10, 5)", s.StartGroup, s.StartObj)
	}
}

func TestParseSubscribeAbsoluteRange(t *testing.T) {
	t.Parallel()
	payload := buildSubscribePayload(4, []string{"prism", "test"}, "video", FilterAbsoluteRange)
	s, err := ParseSubscribe(payload)
	if err != nil {
		t.Fatal(err)
	}
	if s.StartGroup != 10 || s.StartObj != 5 || s.EndGroup != 20 {
		t.Fatalf("range = (%d, %d) - %d, want (10, 5) - 20", s.StartGroup, s.StartObj, s.EndGroup)
	}
}

func TestSerializeSubscribeOKNoContent(t *testing.T) {
	t.Parallel()
	sok := SubscribeOK{
		RequestID:  1,
		TrackAlias: 0,
		Expires:    0,
		GroupOrder: GroupOrderDescending,
	}
	payload := SerializeSubscribeOK(sok)
	r := newBufReader(payload)

	reqID, _ := r.readVarint()
	alias, _ := r.readVarint()
	expires, _ := r.readVarint()
	order, _ := r.readByte()
	exists, _ := r.readByte()
	numParams, _ := r.readVarint()

	if reqID != 1 {
		t.Fatalf("requestID = %d", reqID)
	}
	if alias != 0 {
		t.Fatalf("trackAlias = %d", alias)
	}
	if expires != 0 {
		t.Fatalf("expires = %d", expires)
	}
	if order != GroupOrderDescending {
		t.Fatalf("groupOrder = %d", order)
	}
	if exists != 0 {
		t.Fatal("expected ContentExists=0")
	}
	if numParams != 0 {
		t.Fatalf("numParams = %d", numParams)
	}
}

func TestSerializeSubscribeOKWithContent(t *testing.T) {
	t.Parallel()
	sok := SubscribeOK{
		RequestID:     2,
		TrackAlias:    5,
		Expires:       30,
		GroupOrder:    GroupOrderAscending,
		ContentExists: true,
		LargestGroup:  42,
		LargestObj:    7,
	}
	payload := SerializeSubscribeOK(sok)
	r := newBufReader(payload)

	reqID, _ := r.readVarint()
	alias, _ := r.readVarint()
	expires, _ := r.readVarint()
	order, _ := r.readByte()
	exists, _ := r.readByte()

	if reqID != 2 || alias != 5 || expires != 30 {
		t.Fatalf("basic fields wrong: %d %d %d", reqID, alias, expires)
	}
	if order != GroupOrderAscending {
		t.Fatalf("groupOrder = %d", order)
	}
	if exists != 1 {
		t.Fatal("expected ContentExists=1")
	}

	largestGroup, _ := r.readVarint()
	largestObj, _ := r.readVarint()
	if largestGroup != 42 || largestObj != 7 {
		t.Fatalf("largest = (%d, %d), want (42, 7)", largestGroup, largestObj)
	}
}

func TestSerializeSubscribeError(t *testing.T) {
	t.Parallel()
	se := SubscribeError{
		RequestID:    3,
		ErrorCode:    404,
		ReasonPhrase: "track not found",
	}
	payload := SerializeSubscribeError(se)
	r := newBufReader(payload)

	reqID, _ := r.readVarint()
	code, _ := r.readVarint()
	reason, _ := r.readVarIntBytes()

	if reqID != 3 {
		t.Fatalf("requestID = %d", reqID)
	}
	if code != 404 {
		t.Fatalf("errorCode = %d", code)
	}
	if string(reason) != "track not found" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestParseUnsubscribe(t *testing.T) {
	t.Parallel()
	var payload []byte
	payload = quicvarint.Append(payload, 42)

	u, err := ParseUnsubscribe(payload)
	if err != nil {
		t.Fatal(err)
	}
	if u.RequestID != 42 {
		t.Fatalf("requestID = %d, want 42", u.RequestID)
	}
}

func TestSerializeGoAway(t *testing.T) {
	t.Parallel()
	ga := GoAway{NewSessionURI: "https://example.com/moq"}
	payload := SerializeGoAway(ga)
	r := newBufReader(payload)

	uri, err := r.readVarIntBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(uri) != "https://example.com/moq" {
		t.Fatalf("URI = %q", uri)
	}
}

func TestSerializeMaxRequestID(t *testing.T) {
	t.Parallel()
	payload := SerializeMaxRequestID(99)
	r := newBufReader(payload)

	val, err := r.readVarint()
	if err != nil {
		t.Fatal(err)
	}
	if val != 99 {
		t.Fatalf("maxRequestID = %d, want 99", val)
	}
}

func TestNamespaceTupleRoundTrip(t *testing.T) {
	t.Parallel()
	parts := []string{"prism", "mystream"}
	encoded := AppendNamespaceTuple(nil, parts)

	r := newBufReader(encoded)
	decoded, err := parseNamespaceTuple(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0] != "prism" || decoded[1] != "mystream" {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestNamespaceTupleEmpty(t *testing.T) {
	t.Parallel()
	encoded := AppendNamespaceTuple(nil, []string{})
	r := newBufReader(encoded)
	decoded, err := parseNamespaceTuple(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded = %v, want empty", decoded)
	}
}

func TestBufReaderEOF(t *testing.T) {
	t.Parallel()
	r := newBufReader([]byte{})

	_, err := r.readVarint()
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("readVarint err = %v, want ErrUnexpectedEOF", err)
	}

	_, err = r.readByte()
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("readByte err = %v, want ErrUnexpectedEOF", err)
	}

	_, err = r.readVarIntBytes()
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("readVarIntBytes err = %v, want ErrUnexpectedEOF", err)
	}
}

func TestVarIntBytesRoundTrip(t *testing.T) {
	t.Parallel()
	data := []byte("test payload")
	encoded := appendVarIntBytes(nil, data)

	r := newBufReader(encoded)
	decoded, err := r.readVarIntBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatalf("decoded = %q, want %q", decoded, data)
	}
}
