package moq

import (
	"bytes"
	"testing"
)

// MoQ control/setup parser fuzzing. The contract for every parser here: on ANY
// input it must return a value or an error — never panic, hang, or OOM. A real
// SUT meets malformed control frames from a hostile or buggy peer; these parsers
// are the cleartext front door.
//
// Why fuzz HERE and not through the impair relay: MoQ media + control ride
// ENCRYPTED QUIC, so a relay-level cell cannot see (or mutate) MoQ framing — it
// would be refused by the RequiresCleartext guard. The meaningful MoQ-framing
// fuzzing is therefore at these cleartext codec parsers. Run a target with
//
//	go test -run x -fuzz=FuzzParseSubscribe -fuzztime=30s ./moq/
//
// In normal `go test` the seed corpus runs as a regression test (any crasher Go
// finds is written to testdata/fuzz/ and replayed thereafter).

func FuzzParseClientSetup(f *testing.F) {
	f.Add(SerializeClientSetup(ClientSetup{Versions: []uint64{Version}, Path: "live/x", HasPath: true, MaxRequestID: 100}))
	f.Add(SerializeClientSetup(ClientSetup{Versions: []uint64{Version, 0xff000010}}))
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ParseClientSetup(data) })
}

func FuzzParseSubscribe(f *testing.F) {
	f.Add(SerializeSubscribe(Subscribe{RequestID: 0, Namespace: []string{"prism", "x"}, TrackName: "video",
		GroupOrder: GroupOrderDescending, Forward: 1, FilterType: FilterLatestObject}))
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x40, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ParseSubscribe(data) })
}

func FuzzParseUnsubscribe(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ParseUnsubscribe(data) })
}

func FuzzParseServerSetup(f *testing.F) {
	f.Add(SerializeServerSetup(ServerSetup{SelectedVersion: Version, MaxRequestID: 100}))
	f.Add([]byte{})
	f.Add([]byte{0x80, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ParseServerSetup(data) })
}

func FuzzParseSubscribeOK(f *testing.F) {
	f.Add(SerializeSubscribeOK(SubscribeOK{RequestID: 0, TrackAlias: 1, GroupOrder: GroupOrderDescending}))
	f.Add(SerializeSubscribeOK(SubscribeOK{RequestID: 1, TrackAlias: 2, ContentExists: true, LargestGroup: 9}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ParseSubscribeOK(data) })
}

func FuzzReadControlMsg(f *testing.F) {
	var buf bytes.Buffer
	_ = WriteControlMsg(&buf, MsgClientSetup, SerializeClientSetup(ClientSetup{Versions: []uint64{Version}}))
	f.Add(buf.Bytes())
	f.Add([]byte{})
	f.Add([]byte{0x40, 0xff, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) { _, _, _ = ReadControlMsg(bytes.NewReader(data)) })
}
