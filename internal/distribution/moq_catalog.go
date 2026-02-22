package distribution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/quic-go/quic-go/quicvarint"
	"github.com/zsiec/prism/internal/webtransport"
)

// moqCatalog is the top-level catalog structure per draft-ietf-moq-catalogformat-01.
type moqCatalog struct {
	Version                int               `json:"version"`
	StreamingFormat        int               `json:"streamingFormat"`
	StreamingFormatVersion string            `json:"streamingFormatVersion"`
	CommonTrackFields      moqCommonFields   `json:"commonTrackFields"`
	Tracks                 []moqCatalogTrack `json:"tracks"`
}

// moqCommonFields holds fields shared by all tracks in the catalog.
type moqCommonFields struct {
	Namespace string `json:"namespace"`
	Packaging string `json:"packaging"`
}

// moqCatalogTrack describes a single track in the catalog.
type moqCatalogTrack struct {
	Name            string             `json:"name"`
	SelectionParams moqSelectionParams `json:"selectionParams"`
}

// moqSelectionParams holds codec and media parameters for track selection.
type moqSelectionParams struct {
	Codec         string `json:"codec"`
	Width         int    `json:"width,omitempty"`
	Height        int    `json:"height,omitempty"`
	InitData      string `json:"initData,omitempty"`
	SampleRate    int    `json:"samplerate,omitempty"`
	ChannelConfig string `json:"channelConfig,omitempty"`
}

// buildMoQCatalog assembles the catalog JSON for a stream.
func buildMoQCatalog(streamKey string, relay *Relay) ([]byte, error) {
	vi := relay.VideoInfo()
	ai := relay.AudioInfo()

	catalog := moqCatalog{
		Version:                1,
		StreamingFormat:        1,
		StreamingFormatVersion: "0.2",
		CommonTrackFields: moqCommonFields{
			Namespace: fmt.Sprintf("prism/%s", streamKey),
			Packaging: "loc",
		},
	}

	// Video track
	videoParams := moqSelectionParams{
		Codec:  vi.Codec,
		Width:  vi.Width,
		Height: vi.Height,
	}
	if len(vi.DecoderConfig) > 0 {
		videoParams.InitData = base64.StdEncoding.EncodeToString(vi.DecoderConfig)
	}
	catalog.Tracks = append(catalog.Tracks, moqCatalogTrack{
		Name:            "video",
		SelectionParams: videoParams,
	})

	// Audio tracks
	for i := 0; i < relay.AudioTrackCount(); i++ {
		catalog.Tracks = append(catalog.Tracks, moqCatalogTrack{
			Name: fmt.Sprintf("audio%d", i),
			SelectionParams: moqSelectionParams{
				Codec:         ai.Codec,
				SampleRate:    ai.SampleRate,
				ChannelConfig: fmt.Sprintf("%d", ai.Channels),
			},
		})
	}

	// Caption track
	catalog.Tracks = append(catalog.Tracks, moqCatalogTrack{
		Name: "captions",
		SelectionParams: moqSelectionParams{
			Codec: "caption/v2",
		},
	})

	// Stats track (server-side stream stats delivered as JSON)
	catalog.Tracks = append(catalog.Tracks, moqCatalogTrack{
		Name: "stats",
		SelectionParams: moqSelectionParams{
			Codec: "application/json",
		},
	})

	return json.Marshal(catalog)
}

// writeCatalogObject opens a uni-stream and writes the catalog as a single
// MoQ object (subgroup header + object with payload).
func writeCatalogObject(ctx context.Context, session *webtransport.Session, catalogAlias uint64, catalogJSON []byte) error {
	stream, err := session.OpenUniStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("open catalog stream: %w", err)
	}

	// Subgroup header: stream_type, track_alias, group_id=0, subgroup_id=0, publisher_priority=192
	var hdr []byte
	hdr = quicvarint.Append(hdr, moqStreamTypeSubgroupSIDExt)
	hdr = quicvarint.Append(hdr, catalogAlias)
	hdr = quicvarint.Append(hdr, 0) // group ID
	hdr = quicvarint.Append(hdr, 0) // subgroup ID
	hdr = append(hdr, 192)          // publisher priority (low for catalog)

	if _, err := stream.Write(hdr); err != nil {
		stream.Close()
		return fmt.Errorf("write catalog subgroup header: %w", err)
	}

	// Object: object_id=0, extensions_length=0, payload_length, payload
	var obj []byte
	obj = quicvarint.Append(obj, 0)                        // object ID
	obj = quicvarint.Append(obj, 0)                        // extensions length
	obj = quicvarint.Append(obj, uint64(len(catalogJSON))) // payload length
	obj = append(obj, catalogJSON...)

	if _, err := stream.Write(obj); err != nil {
		stream.Close()
		return fmt.Errorf("write catalog object: %w", err)
	}

	stream.Close()
	return nil
}
