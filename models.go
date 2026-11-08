package main

import (
	"encoding/json"
	"strings"
	"time"
)

// SubtitleDisplayMode selects which languages the subtitle panel shows.
type SubtitleDisplayMode int

const (
	DisplayOriginal SubtitleDisplayMode = iota
	DisplayTranslated
	DisplayBilingual
)

func (m SubtitleDisplayMode) String() string {
	switch m {
	case DisplayOriginal:
		return "原文"
	case DisplayTranslated:
		return "中文"
	default:
		return "双语"
	}
}

// PlaybackLoopMode cycles through the playlist.
type PlaybackLoopMode int

const (
	LoopNone PlaybackLoopMode = iota
	LoopList
	LoopSingle
)

// TranscriptStatus is the state of a video's subtitles.
type TranscriptStatus string

const (
	StatusIdle        TranscriptStatus = "idle"
	StatusLoading     TranscriptStatus = "loading"
	StatusReady       TranscriptStatus = "ready"
	StatusTranslating TranscriptStatus = "translating"
	StatusNoSubtitles TranscriptStatus = "noSubtitles"
	StatusFailed      TranscriptStatus = "failed"
)

func (s TranscriptStatus) Label() string {
	switch s {
	case StatusIdle:
		return "待获取"
	case StatusLoading:
		return "获取中"
	case StatusReady:
		return "双语"
	case StatusTranslating:
		return "翻译中"
	case StatusNoSubtitles:
		return "无字幕"
	default:
		return "失败"
	}
}

// TranscriptProvider names where the subtitles came from.
type TranscriptProvider string

const (
	ProviderYouTube  TranscriptProvider = "youtube"
	ProviderYTDLP    TranscriptProvider = "ytDLP"
	ProviderSupadata TranscriptProvider = "supadata"
)

// VideoItem is one video in the library.
type VideoItem struct {
	ID           string           `json:"id"`
	URL          string           `json:"url"`
	Title        string           `json:"title"`
	Channel      string           `json:"channel"`
	ThumbnailURL string           `json:"thumbnailURL,omitempty"`
	Duration     *float64         `json:"duration"`
	Progress     float64          `json:"progress"`
	Status       TranscriptStatus `json:"status"`
	LastOpenedAt echoTime         `json:"lastOpenedAt"`
}

const (
	placeholderTitle   = "YouTube 视频"
	placeholderChannel = "YouTube"
)

func newVideoItem(id string) *VideoItem {
	return &VideoItem{
		ID:           id,
		URL:          canonicalURL(id),
		Title:        placeholderTitle,
		Channel:      placeholderChannel,
		ThumbnailURL: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg",
		Progress:     0,
		Status:       StatusIdle,
		LastOpenedAt: nowEchoTime(),
	}
}

// RawCaptionCue is one caption line before grouping.
type RawCaptionCue struct {
	Text     string
	Start    float64
	Duration float64
}

func (c RawCaptionCue) End() float64 { return c.Start + c.Duration }

// SubtitleSegment is a readable subtitle unit with optional translation.
// Glosses are vocabulary lookups owned by the Swift app; they are kept as
// raw JSON so this version round-trips them without understanding them.
type SubtitleSegment struct {
	ID             string          `json:"id"`
	Start          float64         `json:"start"`
	End            float64         `json:"end"`
	Original       string          `json:"original"`
	Translation    *string         `json:"translation"`
	ZhUserOverride *string         `json:"zhUserOverride"`
	Glosses        json.RawMessage `json:"glosses,omitempty"`
}

// EffectiveTranslation prefers the user's override over the model's.
func (s *SubtitleSegment) EffectiveTranslation() string {
	if s.ZhUserOverride != nil {
		if t := strings.TrimSpace(*s.ZhUserOverride); t != "" {
			return t
		}
	}
	if s.Translation != nil {
		if t := strings.TrimSpace(*s.Translation); t != "" {
			return t
		}
	}
	return ""
}

// DisplayTranslation returns the translation to show, or "" when absent.
func (s *SubtitleSegment) DisplayTranslation() string { return s.EffectiveTranslation() }

// TranscriptDocument is the cached subtitle document of one video.
type TranscriptDocument struct {
	VideoID        string              `json:"videoID"`
	SourceLanguage string              `json:"sourceLanguage"`
	IsGenerated    bool                `json:"isGenerated"`
	Segments       []SubtitleSegment   `json:"segments"`
	Provider       *TranscriptProvider `json:"provider,omitempty"`
}

// SegmentEnd returns the time segment idx gives way to the next one.
// Segments can overlap (one ends after the next begins), so the effective
// end is the next segment's start when that comes first.
func (d *TranscriptDocument) SegmentEnd(idx int) float64 {
	if idx < 0 || idx >= len(d.Segments) {
		return 0
	}
	end := d.Segments[idx].End
	if idx+1 < len(d.Segments) && d.Segments[idx+1].Start < end {
		end = d.Segments[idx+1].Start
	}
	return end
}

// SegmentProgress reports how far t has moved through segment idx as 0-1.
func (d *TranscriptDocument) SegmentProgress(idx int, t float64) float32 {
	if idx < 0 || idx >= len(d.Segments) {
		return 0
	}
	seg := d.Segments[idx]
	p := (t - seg.Start) / max(d.SegmentEnd(idx)-seg.Start, 0.05)
	return float32(min(max(p, 0), 1))
}

// Library is the persisted library, compatible with the Swift app's
// library.json for the fields this version understands; unknown top-level
// payloads are round-tripped untouched.
type Library struct {
	Videos          []VideoItem                    `json:"videos"`
	Transcripts     map[string]*TranscriptDocument `json:"transcripts"`
	BackgroundCards json.RawMessage                `json:"backgroundCards,omitempty"`
	DetailCache     json.RawMessage                `json:"detailCache,omitempty"`
	HiddenVideoIDs  []string                       `json:"hiddenVideoIDs"`
	hidden          map[string]bool
}

func (l *Library) initHidden() {
	if l.hidden == nil {
		l.hidden = make(map[string]bool, len(l.HiddenVideoIDs))
		for _, id := range l.HiddenVideoIDs {
			l.hidden[id] = true
		}
	}
}

func (l *Library) isHidden(id string) bool {
	l.initHidden()
	return l.hidden[id]
}

// VisibleVideos filters hidden videos, newest first as stored.
func (l *Library) VisibleVideos() []VideoItem {
	var out []VideoItem
	for _, v := range l.Videos {
		if !l.isHidden(v.ID) {
			out = append(out, v)
		}
	}
	return out
}

// HiddenVideos returns the hidden videos, in library order.
func (l *Library) HiddenVideos() []VideoItem {
	var out []VideoItem
	for _, v := range l.Videos {
		if l.isHidden(v.ID) {
			out = append(out, v)
		}
	}
	return out
}

func (l *Library) videoIndex(id string) int {
	for i, v := range l.Videos {
		if v.ID == id {
			return i
		}
	}
	return -1
}

func (l *Library) findVideo(id string) *VideoItem {
	for i := range l.Videos {
		if l.Videos[i].ID == id {
			return &l.Videos[i]
		}
	}
	return nil
}

func (l *Library) syncHidden() {
	l.hidden = nil
	l.initHidden()
	l.HiddenVideoIDs = l.HiddenVideoIDs[:0]
	for id := range l.hidden {
		l.HiddenVideoIDs = append(l.HiddenVideoIDs, id)
	}
}

// Settings holds user preferences, stored in a small JSON file next to
// the library.
type Settings struct {
	SubtitleMode                   SubtitleDisplayMode `json:"subtitleMode"`
	AutoFollow                     bool                `json:"autoFollow"`
	PlaybackLoopMode               PlaybackLoopMode    `json:"playbackLoopMode"`
	TranslationBaseURL             string              `json:"translationBaseURL"`
	TranslationModel               string              `json:"translationModel"`
	OverlayFontSize                float64             `json:"overlayFontSize"`
	DesktopEnglishFontSize         float64             `json:"desktopEnglishFontSize"`
	DesktopChineseFontSize         float64             `json:"desktopChineseFontSize"`
	FloatingBackgroundOpacity      float64             `json:"floatingBackgroundOpacity"`
	MainWindowBackgroundOpacity    float64             `json:"mainWindowBackgroundOpacity"`
	DesktopLyricsBackgroundOpacity float64             `json:"desktopLyricsBackgroundOpacity"`
	OverlayPlateEnabled            bool                `json:"overlayPlateEnabled"`
	SubtitlePaletteID              string              `json:"subtitlePaletteID"`
	EnglishColorHex                string              `json:"englishColorHex"`
	ChineseColorHex                string              `json:"chineseColorHex"`
	FloatPinned                    bool                `json:"floatPinned"`
	ShowsOnAllSpaces               bool                `json:"showsOnAllSpaces"`
	LibraryWidth                   float64             `json:"libraryWidth"`
	LibraryHidden                  bool                `json:"libraryHidden"`
	SubPaneWidth                   float64             `json:"subPaneWidth"`
	SubPaneHidden                  bool                `json:"subPaneHidden"`
	LyricsAlignLeft                bool                `json:"lyricsAlignLeft"`
	LyricsProgress                 bool                `json:"lyricsProgress"`
}

func defaultSettings() Settings {
	return Settings{
		SubtitleMode:                   DisplayBilingual,
		AutoFollow:                     true,
		PlaybackLoopMode:               LoopNone,
		TranslationBaseURL:             "https://api.deepseek.com",
		TranslationModel:               "deepseek-chat",
		OverlayFontSize:                20,
		DesktopEnglishFontSize:         27,
		DesktopChineseFontSize:         18,
		FloatingBackgroundOpacity:      0.96,
		MainWindowBackgroundOpacity:    0.55,
		DesktopLyricsBackgroundOpacity: 0.72,
		OverlayPlateEnabled:            true,
		SubtitlePaletteID:              "classic",
		EnglishColorHex:                "#FFFFFF",
		ChineseColorHex:                "#FFD54F",
		FloatPinned:                    true,
		ShowsOnAllSpaces:               true,
		LibraryWidth:                   250,
		SubPaneWidth:                   380,
		LyricsProgress:                 true,
	}
}

// translatedCount is how many segments of doc have a translation.
func translatedCount(doc *TranscriptDocument) int {
	n := 0
	for i := range doc.Segments {
		if doc.Segments[i].EffectiveTranslation() != "" {
			n++
		}
	}
	return n
}

// Credentials holds API keys, stored as credentials.json like the Swift
// app so both versions share them.
type Credentials struct {
	TranslationAPIKey string `json:"translationAPIKey"`
	SupadataAPIKey    string `json:"supadataAPIKey"`
}

// echoTime is a time.Time that marshals as ISO8601 without fractional
// seconds, matching the Swift app's JSONEncoder .iso8601 strategy so both
// apps can read each other's library.json.
type echoTime struct{ time.Time }

func nowEchoTime() echoTime { return echoTime{time.Now()} }

func (t echoTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z") + `"`), nil
}

func (t *echoTime) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05Z"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return nil
}
