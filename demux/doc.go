// Package demux implements MPEG-TS demuxing with H.264/H.265 video and AAC
// audio parsing. It splits a transport stream into discrete video frames,
// audio frames, closed captions (CEA-608/708), and SCTE-35 splice events.
//
// The central type is [Demuxer], which reads from an [io.Reader] and produces
// parsed frames on typed channels. Codec-specific parsing is provided by
// [ParseAnnexB], [ParseSPS], [ParseADTS], and their HEVC counterparts.
package demux
