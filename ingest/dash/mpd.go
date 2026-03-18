package dash

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/unki2aut/go-mpd"
)

type mpdInfo struct {
	IsDynamic             bool
	AvailabilityStartTime time.Time
	MinUpdatePeriod       time.Duration
	BaseURL               string
	VideoAdaptations      []adaptationInfo
	AudioAdaptations      []adaptationInfo
}

type adaptationInfo struct {
	MimeType        string
	Representations []representationInfo
}

type representationInfo struct {
	ID              string
	Bandwidth       int
	Width           int
	Height          int
	Codecs          string
	SegmentTemplate segmentTemplate
}

type segmentTemplate struct {
	InitializationPattern string
	MediaPattern          string
	StartNumber           int
	Timescale             int
	Duration              int
}

// parseMPD decodes MPD XML and extracts video/audio adaptation sets with
// their representations and segment templates.
func parseMPD(data []byte) (*mpdInfo, error) {
	var m mpd.MPD
	if err := m.Decode(data); err != nil {
		return nil, fmt.Errorf("decode MPD: %w", err)
	}

	info := &mpdInfo{}

	// Type
	if m.Type != nil && *m.Type == "dynamic" {
		info.IsDynamic = true
	}

	// AvailabilityStartTime
	if m.AvailabilityStartTime != nil {
		info.AvailabilityStartTime = time.Time(*m.AvailabilityStartTime)
	}

	// MinimumUpdatePeriod
	if m.MinimumUpdatePeriod != nil {
		ns, err := m.MinimumUpdatePeriod.ToNanoseconds()
		if err == nil {
			info.MinUpdatePeriod = time.Duration(ns)
		}
	}

	// BaseURL (take first if present)
	if len(m.BaseURL) > 0 {
		info.BaseURL = m.BaseURL[0].Value
	}

	// Walk periods and adaptation sets
	for _, period := range m.Period {
		if period == nil {
			continue
		}
		for _, as := range period.AdaptationSets {
			if as == nil {
				continue
			}
			ai := adaptationInfo{
				MimeType: as.MimeType,
			}
			for _, rep := range as.Representations {
				ri := representationInfo{}
				if rep.ID != nil {
					ri.ID = *rep.ID
				}
				if rep.Bandwidth != nil {
					ri.Bandwidth = int(*rep.Bandwidth)
				}
				if rep.Width != nil {
					ri.Width = int(*rep.Width)
				}
				if rep.Height != nil {
					ri.Height = int(*rep.Height)
				}
				if rep.Codecs != nil {
					ri.Codecs = *rep.Codecs
				} else if as.Codecs != nil {
					ri.Codecs = *as.Codecs
				}

				// SegmentTemplate: check representation level first, then adaptation set level
				st := rep.SegmentTemplate
				if st == nil {
					st = as.SegmentTemplate
				}
				if st != nil {
					ri.SegmentTemplate = extractSegmentTemplate(st)
				}

				ai.Representations = append(ai.Representations, ri)
			}

			if strings.HasPrefix(as.MimeType, "video") {
				info.VideoAdaptations = append(info.VideoAdaptations, ai)
			} else if strings.HasPrefix(as.MimeType, "audio") {
				info.AudioAdaptations = append(info.AudioAdaptations, ai)
			}
		}
	}

	return info, nil
}

func extractSegmentTemplate(st *mpd.SegmentTemplate) segmentTemplate {
	t := segmentTemplate{}
	if st.Initialization != nil {
		t.InitializationPattern = *st.Initialization
	}
	if st.Media != nil {
		t.MediaPattern = *st.Media
	}
	if st.StartNumber != nil {
		t.StartNumber = int(*st.StartNumber)
	}
	if st.Timescale != nil {
		t.Timescale = int(*st.Timescale)
	}
	if st.Duration != nil {
		t.Duration = int(*st.Duration)
	}
	return t
}

// selectRepresentation picks a representation from the given adaptation sets.
// If repID is empty, the highest bandwidth representation is selected.
// If repID is given, an exact match is required.
func selectRepresentation(adaptations []adaptationInfo, repID string) (representationInfo, segmentTemplate, error) {
	if len(adaptations) == 0 {
		return representationInfo{}, segmentTemplate{}, fmt.Errorf("no adaptation sets available")
	}

	if repID == "" {
		// Select highest bandwidth across all adaptations
		var best representationInfo
		found := false
		for _, a := range adaptations {
			for _, r := range a.Representations {
				if !found || r.Bandwidth > best.Bandwidth {
					best = r
					found = true
				}
			}
		}
		if !found {
			return representationInfo{}, segmentTemplate{}, fmt.Errorf("no representations available")
		}
		return best, best.SegmentTemplate, nil
	}

	// Exact match
	for _, a := range adaptations {
		for _, r := range a.Representations {
			if r.ID == repID {
				return r, r.SegmentTemplate, nil
			}
		}
	}
	return representationInfo{}, segmentTemplate{}, fmt.Errorf("representation %q not found", repID)
}

// resolveInitURL replaces $RepresentationID$ in the initialization pattern
// and prepends the base URL.
func resolveInitURL(tmpl segmentTemplate, repID, baseURL string) string {
	s := strings.ReplaceAll(tmpl.InitializationPattern, "$RepresentationID$", repID)
	return baseURL + s
}

// resolveMediaURL replaces $RepresentationID$ and $Number$ in the media
// pattern and prepends the base URL.
func resolveMediaURL(tmpl segmentTemplate, repID string, number int, baseURL string) string {
	s := strings.ReplaceAll(tmpl.MediaPattern, "$RepresentationID$", repID)
	s = strings.ReplaceAll(s, "$Number$", strconv.Itoa(number))
	return baseURL + s
}

// computeSegmentNumber computes the latest fully-available segment number
// (one behind the live edge) based on the current time, availability start
// time, and the segment template parameters.
func computeSegmentNumber(now time.Time, ast time.Time, tmpl segmentTemplate) int {
	if tmpl.Timescale == 0 || tmpl.Duration == 0 {
		return tmpl.StartNumber
	}
	elapsed := now.Sub(ast).Seconds()
	segmentDuration := float64(tmpl.Duration) / float64(tmpl.Timescale)
	if elapsed < segmentDuration {
		return tmpl.StartNumber
	}
	// Total segments elapsed from AST
	totalSegments := int(elapsed / segmentDuration)
	// One behind live edge
	liveSegment := totalSegments - 1
	if liveSegment < 0 {
		liveSegment = 0
	}
	return tmpl.StartNumber + liveSegment
}
