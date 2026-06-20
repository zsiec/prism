package moq

import "github.com/quic-go/quic-go/quicvarint"

// This file adds the subscriber (client) half of the control codec, making the
// package symmetric: control.go has the publisher (server) serializers/parsers
// (ServerSetup, SubscribeOK) and the parsers for what a client sends
// (ClientSetup, Subscribe); here are the encoders for what a client SENDS
// (ClientSetup, Subscribe) and the parsers for what it RECEIVES (ServerSetup,
// SubscribeOK). A headless Go MoQ subscriber uses these.

// SerializeClientSetup serializes a CLIENT_SETUP payload. The path parameter
// (the stream key) is sent when HasPath is set; MaxRequestID is always sent.
func SerializeClientSetup(cs ClientSetup) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, uint64(len(cs.Versions)))
	for _, v := range cs.Versions {
		buf = quicvarint.Append(buf, v)
	}
	n := uint64(1) // MaxRequestID
	if cs.HasPath {
		n++
	}
	buf = quicvarint.Append(buf, n)
	if cs.HasPath {
		buf = quicvarint.Append(buf, ParamPath) // odd -> length-prefixed bytes
		buf = appendVarIntBytes(buf, []byte(cs.Path))
	}
	buf = quicvarint.Append(buf, ParamMaxRequestID) // even -> varint
	buf = quicvarint.Append(buf, cs.MaxRequestID)
	return buf
}

// SerializeSubscribe serializes a SUBSCRIBE payload. Only the live filter types
// (FilterNextGroupStart / FilterLatestObject) are emitted — no Absolute
// start/range fields — matching what the Prism server accepts.
func SerializeSubscribe(s Subscribe) []byte {
	var buf []byte
	buf = quicvarint.Append(buf, s.RequestID)
	buf = AppendNamespaceTuple(buf, s.Namespace)
	buf = appendVarIntBytes(buf, []byte(s.TrackName))
	buf = append(buf, s.Priority, s.GroupOrder, s.Forward)
	buf = quicvarint.Append(buf, s.FilterType)
	buf = quicvarint.Append(buf, 0) // num params
	return buf
}

// ParseServerSetup parses a SERVER_SETUP payload (the version the server
// selected and its MAX_REQUEST_ID).
func ParseServerSetup(data []byte) (ServerSetup, error) {
	r := newBufReader(data)
	var ss ServerSetup
	var err error
	if ss.SelectedVersion, err = r.readVarint(); err != nil {
		return ss, &ParseError{Field: "selected_version", Err: err}
	}
	numParams, err := r.readVarint()
	if err != nil {
		return ss, &ParseError{Field: "num_params", Err: err}
	}
	for i := uint64(0); i < numParams; i++ {
		key, err := r.readVarint()
		if err != nil {
			return ss, &ParseError{Field: "param_key", Err: err}
		}
		if key%2 == 1 { // odd -> length-prefixed bytes
			if _, err := r.readVarIntBytes(); err != nil {
				return ss, &ParseError{Field: "param_value", Err: err}
			}
			continue
		}
		val, err := r.readVarint() // even -> varint
		if err != nil {
			return ss, &ParseError{Field: "param_value", Err: err}
		}
		if key == ParamMaxRequestID {
			ss.MaxRequestID = val
		}
	}
	return ss, nil
}

// ParseSubscribeOK parses a SUBSCRIBE_OK payload, yielding the server-assigned
// track alias used to demultiplex incoming data streams.
func ParseSubscribeOK(data []byte) (SubscribeOK, error) {
	r := newBufReader(data)
	var sok SubscribeOK
	var err error
	if sok.RequestID, err = r.readVarint(); err != nil {
		return sok, &ParseError{Field: "request_id", Err: err}
	}
	if sok.TrackAlias, err = r.readVarint(); err != nil {
		return sok, &ParseError{Field: "track_alias", Err: err}
	}
	if sok.Expires, err = r.readVarint(); err != nil {
		return sok, &ParseError{Field: "expires", Err: err}
	}
	if sok.GroupOrder, err = r.readByte(); err != nil {
		return sok, &ParseError{Field: "group_order", Err: err}
	}
	ce, err := r.readByte()
	if err != nil {
		return sok, &ParseError{Field: "content_exists", Err: err}
	}
	sok.ContentExists = ce == 1
	if sok.ContentExists {
		if sok.LargestGroup, err = r.readVarint(); err != nil {
			return sok, &ParseError{Field: "largest_group", Err: err}
		}
		if sok.LargestObj, err = r.readVarint(); err != nil {
			return sok, &ParseError{Field: "largest_obj", Err: err}
		}
	}
	return sok, nil
}
