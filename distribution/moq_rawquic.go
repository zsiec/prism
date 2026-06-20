package distribution

import (
	"context"
	"fmt"

	"github.com/quic-go/quic-go"
	"github.com/zsiec/prism/webtransport"
)

// This file lets a plain QUIC connection drive the same MoQSession the browser's
// WebTransport path uses, by adapting quic-go's stream types to the small
// webtransport stream interfaces the session expects. The browser keeps
// WebTransport; a headless Go subscriber speaks MoQ over raw QUIC. Both ride the
// same QUIC loss recovery, so an impairment relay in the UDP path exercises the
// media resilience identically.

// quicSendStream adapts a quic.SendStream to webtransport.SendStream (the only
// difference is the CancelWrite error-code type).
type quicSendStream struct{ quic.SendStream }

func (s quicSendStream) CancelWrite(code webtransport.StreamErrorCode) {
	s.SendStream.CancelWrite(quic.StreamErrorCode(code))
}

var _ webtransport.SendStream = quicSendStream{}

// quicStream adapts a quic.Stream to webtransport.Stream.
type quicStream struct{ quic.Stream }

func (s quicStream) CancelWrite(code webtransport.StreamErrorCode) {
	s.Stream.CancelWrite(quic.StreamErrorCode(code))
}
func (s quicStream) CancelRead(code webtransport.StreamErrorCode) {
	s.Stream.CancelRead(quic.StreamErrorCode(code))
}

var _ webtransport.Stream = quicStream{}

// quicMoQConn adapts a quic.Connection to the moqConn seam MoQSession needs.
type quicMoQConn struct{ conn quic.Connection }

func (c quicMoQConn) OpenUniStreamSync(ctx context.Context) (webtransport.SendStream, error) {
	s, err := c.conn.OpenUniStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return quicSendStream{s}, nil
}

func (c quicMoQConn) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	return c.conn.ReceiveDatagram(ctx)
}

var _ moqConn = quicMoQConn{}

// RunRawQUICSession serves one MoQ subscriber over a raw QUIC connection (no
// WebTransport/HTTP3): it accepts the client's control stream, runs the MoQ
// setup, attaches the session to the stream's relay so it receives the media
// fan-out, and blocks until the connection ends. It is the harness-facing
// counterpart to the browser's WebTransport path — the same MoQSession logic
// over a different transport — enabling a headless, automatable glass-to-glass
// test through an impairment relay.
func RunRawQUICSession(ctx context.Context, conn quic.Connection, streamKey string, relay *Relay, statsProvider StatsProviderFunc) error {
	control, err := conn.AcceptStream(ctx) // the client opens the control bidi stream first
	if err != nil {
		return fmt.Errorf("accept control stream: %w", err)
	}
	sess := NewMoQSession(MoQSessionConfig{
		ID:            fmt.Sprintf("rawquic-%s-%s", streamKey, conn.RemoteAddr()),
		Session:       quicMoQConn{conn: conn},
		Control:       quicStream{Stream: control},
		StreamKey:     streamKey,
		Relay:         relay,
		StatsProvider: statsProvider,
	})
	if _, err := sess.handleSetup(); err != nil {
		return fmt.Errorf("moq setup: %w", err)
	}
	if relay != nil {
		relay.AddViewer(sess)
		defer relay.RemoveViewer(sess.ID())
	}
	return sess.Run(ctx)
}
