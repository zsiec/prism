// Package moq implements the wire-protocol codec for MoQ Transport
// (draft-ietf-moq-transport-15), including control message parsing and
// serialization, media format conversion (Annex B → AVC1, ADTS stripping,
// decoder configuration records), and typed error definitions.
//
// This package contains no session or relay logic; those higher-level
// concerns live in [github.com/zsiec/prism/internal/distribution].
package moq
