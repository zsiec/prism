package distribution

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/zsiec/ccx"
	"github.com/zsiec/prism/internal/media"
	"github.com/zsiec/prism/internal/moq"
	"github.com/zsiec/prism/internal/webtransport"
)

// buildClientSetupPayload builds a CLIENT_SETUP payload for testing.
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
		buf = quicvarint.Append(buf, moq.ParamPath)
		buf = quicvarint.Append(buf, uint64(len(path)))
		buf = append(buf, []byte(path)...)
	}
	if maxReqID > 0 {
		buf = quicvarint.Append(buf, moq.ParamMaxRequestID)
		buf = quicvarint.Append(buf, maxReqID)
	}

	return buf
}

// readVarint is a test helper to read a varint from a bytes.Reader.
func readVarint(data []byte, offset int) (uint64, int) {
	val, n, _ := quicvarint.Parse(data[offset:])
	return val, offset + n
}

func TestMoQSessionHandleSetupHappyPath(t *testing.T) {
	t.Parallel()
	// Build a CLIENT_SETUP with our version and PATH
	csPayload := buildClientSetupPayload([]uint64{moq.Version}, "/moq?stream=test", 50)
	var controlBuf bytes.Buffer
	if err := moq.WriteControlMsg(&controlBuf, moq.MsgClientSetup, csPayload); err != nil {
		t.Fatal(err)
	}

	// Use a pipe-like approach: write to controlBuf, read from it
	// MoQSession reads from the control stream
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &controlBuf,
		Writer: responseBuf,
	}

	relay := NewRelay()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "test",
		control:       controlStream,
		controlReader: bufio.NewReader(controlStream),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	pathKey, err := session.handleSetup()
	if err != nil {
		t.Fatal(err)
	}
	if pathKey != "/moq?stream=test" {
		t.Fatalf("pathKey = %q, want /moq?stream=test", pathKey)
	}

	// Verify SERVER_SETUP was written
	msgType, payload, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgServerSetup {
		t.Fatalf("first response type = %#x, want SERVER_SETUP", msgType)
	}

	ver, off := readVarint(payload, 0)
	if ver != moq.Version {
		t.Fatalf("server version = %#x, want %#x", ver, moq.Version)
	}
	_ = off

	// Verify MAX_REQUEST_ID was written
	msgType2, payload2, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType2 != moq.MsgMaxRequestID {
		t.Fatalf("second response type = %#x, want MAX_REQUEST_ID", msgType2)
	}
	maxReq, _ := readVarint(payload2, 0)
	if maxReq != 100 {
		t.Fatalf("max request ID = %d, want 100", maxReq)
	}
}

func TestMoQSessionHandleSetupWrongVersion(t *testing.T) {
	t.Parallel()
	csPayload := buildClientSetupPayload([]uint64{0xff000001}, "", 0)
	var controlBuf bytes.Buffer
	if err := moq.WriteControlMsg(&controlBuf, moq.MsgClientSetup, csPayload); err != nil {
		t.Fatal(err)
	}

	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &controlBuf,
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "test",
		control:       controlStream,
		controlReader: bufio.NewReader(controlStream),
		subscriptions: make(map[string]*moqTrackSub),
	}

	_, err := session.handleSetup()
	if err == nil {
		t.Fatal("expected error for incompatible version")
	}
}

func TestMoQSessionHandleSubscribeVideo(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	// We can't actually start a real write loop (needs real WebTransport session),
	// but we can verify that handleMediaSubscribe creates the subscription and
	// sends SUBSCRIBE_OK. The write loop goroutine will exit immediately when
	// OpenUniStreamSync fails.

	sub := moq.Subscribe{
		RequestID:  1,
		Namespace:  []string{"prism", "live"},
		TrackName:  "video",
		FilterType: moq.FilterNextGroupStart,
	}

	// handleSubscribe calls handleMediaSubscribe which starts a goroutine.
	// Since there's no real WT session, we just verify the control protocol response.
	session.handleSubscribe(context.Background(), sub)

	// Read SUBSCRIBE_OK
	msgType, payload, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgSubscribeOK {
		t.Fatalf("response type = %#x, want SUBSCRIBE_OK", msgType)
	}

	reqID, off := readVarint(payload, 0)
	alias, _ := readVarint(payload, off)
	if reqID != 1 {
		t.Fatalf("requestID = %d, want 1", reqID)
	}
	if alias != 0 {
		t.Fatalf("trackAlias = %d, want 0", alias)
	}

	// Verify subscription was created
	session.mu.RLock()
	videoSub := session.subscriptions["video"]
	session.mu.RUnlock()
	if videoSub == nil {
		t.Fatal("video subscription not created")
	}
	if videoSub.videoCh == nil {
		t.Fatal("video channel not created")
	}
}

func TestMoQSessionHandleSubscribeAudio(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	sub := moq.Subscribe{
		RequestID:  2,
		Namespace:  []string{"prism", "live"},
		TrackName:  "audio0",
		FilterType: moq.FilterLatestObject,
	}

	session.handleSubscribe(context.Background(), sub)

	msgType, _, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgSubscribeOK {
		t.Fatalf("response type = %#x, want SUBSCRIBE_OK", msgType)
	}

	session.mu.RLock()
	audioSub := session.subscriptions["audio0"]
	session.mu.RUnlock()
	if audioSub == nil {
		t.Fatal("audio subscription not created")
	}
	if audioSub.audioCh == nil {
		t.Fatal("audio channel not created")
	}
	if audioSub.audioTrackIndex != 0 {
		t.Fatalf("audioTrackIndex = %d, want 0", audioSub.audioTrackIndex)
	}
}

func TestMoQSessionHandleSubscribeCaptions(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	sub := moq.Subscribe{
		RequestID:  3,
		Namespace:  []string{"prism", "live"},
		TrackName:  "captions",
		FilterType: moq.FilterNextGroupStart,
	}

	session.handleSubscribe(context.Background(), sub)

	msgType, _, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgSubscribeOK {
		t.Fatalf("response type = %#x, want SUBSCRIBE_OK", msgType)
	}

	session.mu.RLock()
	capSub := session.subscriptions["captions"]
	session.mu.RUnlock()
	if capSub == nil {
		t.Fatal("caption subscription not created")
	}
}

func TestMoQSessionHandleSubscribeUnknownTrack(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	sub := moq.Subscribe{
		RequestID:  4,
		Namespace:  []string{"prism", "live"},
		TrackName:  "nonexistent",
		FilterType: moq.FilterNextGroupStart,
	}

	session.handleSubscribe(context.Background(), sub)

	msgType, payload, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgSubscribeError {
		t.Fatalf("response type = %#x, want SUBSCRIBE_ERROR", msgType)
	}

	reqID, off := readVarint(payload, 0)
	errCode, _ := readVarint(payload, off)
	if reqID != 4 {
		t.Fatalf("requestID = %d, want 4", reqID)
	}
	if errCode != 404 {
		t.Fatalf("errorCode = %d, want 404", errCode)
	}
}

func TestMoQSessionHandleSubscribeWrongNamespace(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	sub := moq.Subscribe{
		RequestID:  5,
		Namespace:  []string{"other", "stream"},
		TrackName:  "video",
		FilterType: moq.FilterNextGroupStart,
	}

	session.handleSubscribe(context.Background(), sub)

	msgType, _, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgSubscribeError {
		t.Fatalf("response type = %#x, want SUBSCRIBE_ERROR", msgType)
	}
}

func TestMoQSessionHandleSubscribeUnsupportedFilter(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	sub := moq.Subscribe{
		RequestID:  6,
		Namespace:  []string{"prism", "live"},
		TrackName:  "video",
		FilterType: moq.FilterAbsoluteStart,
	}

	session.handleSubscribe(context.Background(), sub)

	msgType, _, err := moq.ReadControlMsg(responseBuf)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != moq.MsgSubscribeError {
		t.Fatalf("response type = %#x, want SUBSCRIBE_ERROR", msgType)
	}
}

func TestMoQSessionUnsubscribe(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	// Subscribe first
	sub := moq.Subscribe{
		RequestID:  7,
		Namespace:  []string{"prism", "live"},
		TrackName:  "video",
		FilterType: moq.FilterNextGroupStart,
	}
	session.handleSubscribe(context.Background(), sub)

	// Drain SUBSCRIBE_OK
	moq.ReadControlMsg(responseBuf)

	// Unsubscribe
	session.handleUnsubscribe(moq.Unsubscribe{RequestID: 7})

	session.mu.RLock()
	_, exists := session.subscriptions["video"]
	session.mu.RUnlock()
	if exists {
		t.Fatal("video subscription should be removed after unsubscribe")
	}
}

func TestMoQSessionTrackAliasSequential(t *testing.T) {
	t.Parallel()
	relay := NewRelay()
	responseBuf := &bytes.Buffer{}
	controlStream := &mockControlStream{
		Reader: &bytes.Buffer{},
		Writer: responseBuf,
	}

	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		control:       controlStream,
		log:           slog.With("session", "test-session"),
		relay:         relay,
		subscriptions: make(map[string]*moqTrackSub),
	}

	tracks := []string{"video", "audio0", "captions"}
	for i, trackName := range tracks {
		sub := moq.Subscribe{
			RequestID:  uint64(i),
			Namespace:  []string{"prism", "live"},
			TrackName:  trackName,
			FilterType: moq.FilterNextGroupStart,
		}
		session.handleSubscribe(context.Background(), sub)

		_, payload, _ := moq.ReadControlMsg(responseBuf)
		_, off := readVarint(payload, 0) // requestID
		alias, _ := readVarint(payload, off)
		if alias != uint64(i) {
			t.Fatalf("track %q alias = %d, want %d", trackName, alias, i)
		}
	}
}

func TestMoQSessionSendVideoNoSub(t *testing.T) {
	t.Parallel()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		subscriptions: make(map[string]*moqTrackSub),
	}

	// Should not panic when no video subscription exists
	frame := &media.VideoFrame{
		PTS:        1000000,
		IsKeyframe: true,
		NALUs:      [][]byte{{0x65, 0x00}},
	}
	session.SendVideo(frame)

	if session.videoSent.Load() != 0 {
		t.Fatal("videoSent should be 0 with no subscription")
	}
}

func TestMoQSessionSendAudioNoSub(t *testing.T) {
	t.Parallel()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		subscriptions: make(map[string]*moqTrackSub),
	}

	frame := &media.AudioFrame{
		PTS:        1000000,
		Data:       []byte{0xFF, 0xF1, 0x00, 0x00, 0x00, 0x00, 0x00},
		TrackIndex: 0,
	}
	session.SendAudio(frame)

	if session.audioSent.Load() != 0 {
		t.Fatal("audioSent should be 0 with no subscription")
	}
}

func TestMoQSessionSendCaptionsNoSub(t *testing.T) {
	t.Parallel()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		subscriptions: make(map[string]*moqTrackSub),
	}

	frame := &ccx.CaptionFrame{PTS: 1000000, Text: "Hello"}
	session.SendCaptions(frame)

	if session.captionSent.Load() != 0 {
		t.Fatal("captionSent should be 0 with no subscription")
	}
}

func TestMoQSessionSendVideoWithSub(t *testing.T) {
	t.Parallel()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		subscriptions: make(map[string]*moqTrackSub),
	}

	// Manually add a video subscription
	session.subscriptions["video"] = &moqTrackSub{
		trackName: "video",
		videoCh:   make(chan *media.VideoFrame, 10),
	}

	frame := &media.VideoFrame{
		PTS:        1000000,
		IsKeyframe: true,
		NALUs:      [][]byte{{0x65, 0x00}},
		GroupID:    1,
	}
	session.SendVideo(frame)

	if session.videoSent.Load() != 1 {
		t.Fatalf("videoSent = %d, want 1", session.videoSent.Load())
	}
}

func TestMoQSessionSendAudioWithSub(t *testing.T) {
	t.Parallel()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		subscriptions: make(map[string]*moqTrackSub),
	}

	session.subscriptions["audio0"] = &moqTrackSub{
		trackName:       "audio0",
		audioCh:         make(chan *media.AudioFrame, 10),
		audioTrackIndex: 0,
	}

	frame := &media.AudioFrame{
		PTS:        1000000,
		Data:       []byte{0xFF, 0xF1, 0x00, 0x00, 0x00, 0x00, 0x00},
		TrackIndex: 0,
	}
	session.SendAudio(frame)

	if session.audioSent.Load() != 1 {
		t.Fatalf("audioSent = %d, want 1", session.audioSent.Load())
	}
}

func TestMoQSessionStats(t *testing.T) {
	t.Parallel()
	session := &MoQSession{
		id:            "test-session",
		streamKey:     "live",
		subscriptions: make(map[string]*moqTrackSub),
	}

	session.videoSent.Store(100)
	session.audioSent.Store(200)
	session.captionSent.Store(5)
	session.videoDropped.Store(3)
	session.bytesSent.Store(50000)
	session.lastVideoTsMS.Store(12345)

	stats := session.Stats()
	if stats.ID != "test-session" {
		t.Fatalf("ID = %q", stats.ID)
	}
	if stats.VideoSent != 100 {
		t.Fatalf("VideoSent = %d", stats.VideoSent)
	}
	if stats.AudioSent != 200 {
		t.Fatalf("AudioSent = %d", stats.AudioSent)
	}
	if stats.CaptionSent != 5 {
		t.Fatalf("CaptionSent = %d", stats.CaptionSent)
	}
	if stats.VideoDropped != 3 {
		t.Fatalf("VideoDropped = %d", stats.VideoDropped)
	}
	if stats.BytesSent != 50000 {
		t.Fatalf("BytesSent = %d", stats.BytesSent)
	}
	if stats.LastVideoTsMS != 12345 {
		t.Fatalf("LastVideoTsMS = %d", stats.LastVideoTsMS)
	}
}

// mockControlStream implements webtransport.Stream for test purposes.
// It uses separate Reader/Writer to simulate the control stream.
type mockControlStream struct {
	Reader *bytes.Buffer
	Writer *bytes.Buffer
}

var _ webtransport.Stream = (*mockControlStream)(nil)

func (m *mockControlStream) Read(p []byte) (int, error)                 { return m.Reader.Read(p) }
func (m *mockControlStream) Write(p []byte) (int, error)                { return m.Writer.Write(p) }
func (m *mockControlStream) Close() error                               { return nil }
func (m *mockControlStream) CancelRead(_ webtransport.StreamErrorCode)  {}
func (m *mockControlStream) CancelWrite(_ webtransport.StreamErrorCode) {}
func (m *mockControlStream) SetDeadline(_ time.Time) error              { return nil }
func (m *mockControlStream) SetReadDeadline(_ time.Time) error          { return nil }
func (m *mockControlStream) SetWriteDeadline(_ time.Time) error         { return nil }
func (m *mockControlStream) StreamID() quic.StreamID                    { return 0 }
