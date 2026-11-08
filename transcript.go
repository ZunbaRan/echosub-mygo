package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TranscriptServiceError describes why subtitles could not be fetched.
type TranscriptServiceError struct{ msg string }

func (e *TranscriptServiceError) Error() string { return e.msg }

func transcriptErr(msg string) error { return &TranscriptServiceError{msg: msg} }

var (
	errInvalidWatchPage = transcriptErr("无法读取 YouTube 视频页面。")
	errNoSubtitles      = transcriptErr("这个视频没有可用字幕。")
	errMalformed        = transcriptErr("YouTube 返回了无法识别的字幕数据。")
)

// transcriptHTTP is the shared client for YouTube requests.
var transcriptHTTP = &http.Client{Timeout: 20 * time.Second}

const ytUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15"

// FetchTranscript tries the subtitle providers in order, like the Swift
// YouTubeTranscriptService: YouTube direct (watch page, then the InnerTube
// Android client), then yt-dlp, then Supadata.
func FetchTranscript(ctx context.Context, videoID string, supadataAPIKey string) (*TranscriptFetchResult, error) {
	result, directErr := fetchDirect(ctx, videoID)
	if directErr == nil {
		return result, nil
	}

	ytdlpResult, ytdlpErr := fetchYTDLP(ctx, videoID)
	if ytdlpErr == nil {
		return ytdlpResult, nil
	}

	supadataResult, supadataErr := fetchSupadata(ctx, videoID, supadataAPIKey)
	if supadataErr == nil {
		return supadataResult, nil
	}

	return nil, transcriptErr("YouTube：" + directErr.Error() + "\n" +
		ytdlpErr.Error() + "\n" + supadataErr.Error())
}

// TranscriptFetchResult carries the document plus video metadata.
type TranscriptFetchResult struct {
	Document *TranscriptDocument
	Title    string
	Channel  string
	Duration *float64
}

func fetchDirect(ctx context.Context, videoID string) (*TranscriptFetchResult, error) {
	watchURL := canonicalURL(videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, watchURL, nil)
	if err != nil {
		return nil, errInvalidWatchPage
	}
	req.Header.Set("Referer", "https://www.youtube.com/")
	req.Header.Set("User-Agent", ytUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return nil, transcriptErr("网络请求失败：" + err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, transcriptErr("网络请求失败：" + err.Error())
	}
	html := string(data)

	apiKey := firstRegexMatch(`"INNERTUBE_API_KEY":"([^"]+)"`, html)
	if embedded, ok := embeddedPlayerResponse(html); ok &&
		embedded.PlayabilityStatus.Status == "OK" &&
		embedded.Captions != nil && len(embedded.Captions.Renderer.CaptionTracks) > 0 {
		result, err := finish(ctx, videoID, embedded)
		if err == nil {
			return enrichMetadata(ctx, videoID, result), nil
		}
		// The web player response still lists caption tracks, but its
		// timedtext URL can return an empty body; retry with the Android
		// client before giving up.
		if apiKey == "" {
			return nil, err
		}
		return fetchPlayer(ctx, videoID, apiKey)
	}
	if apiKey == "" {
		return nil, errInvalidWatchPage
	}
	return fetchPlayer(ctx, videoID, apiKey)
}

func fetchPlayer(ctx context.Context, videoID, apiKey string) (*TranscriptFetchResult, error) {
	endpoint := "https://www.youtube.com/youtubei/v1/player?key=" + url.QueryEscape(apiKey)
	body := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "ANDROID",
				"clientVersion": "20.10.38",
				"hl":            "en",
				"gl":            "US",
			},
		},
		"videoId": videoID,
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errMalformed
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "com.google.android.youtube/20.10.38 (Linux; U; Android 14)")

	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return nil, transcriptErr("网络请求失败：" + err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, transcriptErr("网络请求失败：" + err.Error())
	}
	var response playerResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, errMalformed
	}
	if response.PlayabilityStatus.Status != "OK" {
		reason := response.PlayabilityStatus.Reason
		if reason == "" {
			reason = "该视频当前不可用。"
		}
		return nil, transcriptErr(reason)
	}
	result, err := finish(ctx, videoID, &response)
	if err != nil {
		return nil, err
	}
	return enrichMetadata(ctx, videoID, result), nil
}

func finish(ctx context.Context, videoID string, response *playerResponse) (*TranscriptFetchResult, error) {
	if response.Captions == nil || len(response.Captions.Renderer.CaptionTracks) == 0 {
		return nil, errNoSubtitles
	}
	track := bestTrack(response.Captions.Renderer.CaptionTracks)
	if track == nil {
		return nil, errNoSubtitles
	}
	cues, err := fetchCues(ctx, track)
	if err != nil {
		return nil, err
	}
	segments := groupCues(cues)
	if len(segments) == 0 {
		return nil, errNoSubtitles
	}
	provider := ProviderYouTube
	return &TranscriptFetchResult{
		Document: &TranscriptDocument{
			VideoID:        videoID,
			SourceLanguage: track.LanguageCode,
			IsGenerated:    track.Kind == "asr",
			Segments:       segments,
			Provider:       &provider,
		},
		Title:    placeholderIfEmpty(response.VideoDetails.Title),
		Channel:  placeholderIfEmpty(response.VideoDetails.Author),
		Duration: parseSeconds(response.VideoDetails.LengthSeconds),
	}, nil
}

func placeholderIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return placeholderTitle
	}
	return s
}

func parseSeconds(s string) *float64 {
	if s == "" {
		return nil
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return &v
	}
	return nil
}

func fetchCues(ctx context.Context, track *captionTrack) ([]RawCaptionCue, error) {
	u, err := url.Parse(track.BaseURL)
	if err != nil {
		return nil, errMalformed
	}
	q := u.Query()
	q.Del("fmt")
	q.Set("fmt", "json3")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errMalformed
	}
	req.Header.Set("User-Agent", ytUserAgent)
	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return nil, transcriptErr("网络请求失败：" + err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, transcriptErr("网络请求失败：" + err.Error())
	}
	cues, ok := parseCues(data)
	if !ok || len(cues) == 0 {
		return nil, errNoSubtitles
	}
	return cues, nil
}

// --- YouTube InnerTube response shapes ---

type playerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	Captions *struct {
		Renderer struct {
			CaptionTracks []*captionTrack `json:"captionTracks"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
	VideoDetails struct {
		Title         string `json:"title"`
		Author        string `json:"author"`
		LengthSeconds string `json:"lengthSeconds"`
	} `json:"videoDetails"`
}

type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"`
}

func bestTrack(tracks []*captionTrack) *captionTrack {
	sort.SliceStable(tracks, func(i, j int) bool {
		return trackScore(tracks[i]) > trackScore(tracks[j])
	})
	if len(tracks) == 0 {
		return nil
	}
	return tracks[0]
}

func trackScore(t *captionTrack) int {
	score := 0
	if strings.HasPrefix(strings.ToLower(t.LanguageCode), "en") {
		score += 100
	}
	if t.Kind != "asr" {
		score += 20
	}
	return score
}

// embeddedPlayerResponse extracts the ytInitialPlayerResponse JSON object
// from the watch page HTML with a brace-matching scan, as the Swift version.
func embeddedPlayerResponse(html string) (*playerResponse, bool) {
	for _, marker := range []string{
		"var ytInitialPlayerResponse = ",
		"window[\"ytInitialPlayerResponse\"] = ",
		"\"ytInitialPlayerResponse\":",
	} {
		idx := strings.Index(html, marker)
		if idx < 0 {
			continue
		}
		rest := html[idx+len(marker):]
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			continue
		}
		objectStart := idx + len(marker) + start
		depth := 0
		inString := false
		escaped := false
		for i := objectStart; i < len(html); i++ {
			ch := html[i]
			if inString {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
			} else {
				switch ch {
				case '"':
					inString = true
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						var response playerResponse
						if err := json.Unmarshal([]byte(html[objectStart:i+1]), &response); err == nil {
							return &response, true
						}
						return nil, false
					}
				}
			}
		}
	}
	return nil, false
}

// --- timedtext cue parsing ---

// parseCues handles YouTube timedtext in JSON3 ("events") and XML forms.
func parseCues(data []byte) ([]RawCaptionCue, bool) {
	var root struct {
		Events []struct {
			TStartMs    *int64 `json:"tStartMs"`
			DDurationMs *int64 `json:"dDurationMs"`
			Segs        []struct {
				UTF8 string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &root); err == nil && root.Events != nil {
		cues := make([]RawCaptionCue, 0, len(root.Events))
		for _, event := range root.Events {
			var sb strings.Builder
			for _, seg := range event.Segs {
				sb.WriteString(seg.UTF8)
			}
			text := normalizeSubtitle(sb.String())
			if text == "" {
				continue
			}
			start := 0.0
			if event.TStartMs != nil {
				start = float64(*event.TStartMs) / 1000
			}
			duration := 0.0
			if event.DDurationMs != nil {
				duration = float64(*event.DDurationMs) / 1000
			}
			cues = append(cues, RawCaptionCue{Text: text, Start: start, Duration: duration})
		}
		return cues, true
	}
	return parseXMLCues(data)
}

// parseXMLCues handles both <p t="240" d="5760"><s>…</s></p> (format 3)
// and older <text start="1.2" dur="3.4">…</text>.
func parseXMLCues(data []byte) ([]RawCaptionCue, bool) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var cues []RawCaptionCue
	var start, duration float64
	var text strings.Builder
	var collecting bool

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				start = 0
				duration = 0
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "t":
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							start = v / 1000
						}
					case "d":
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							duration = v / 1000
						}
					}
				}
				text.Reset()
				collecting = true
			case "text":
				start, duration = 0, 0
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "start":
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							start = v
						}
					case "dur":
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							duration = v
						}
					}
				}
				text.Reset()
				collecting = true
			}
		case xml.CharData:
			if collecting {
				text.WriteString(string(t))
			}
		case xml.EndElement:
			if collecting && (t.Name.Local == "p" || t.Name.Local == "text") {
				collecting = false
				normalized := normalizeSubtitle(text.String())
				if normalized != "" {
					cues = append(cues, RawCaptionCue{Text: normalized, Start: start, Duration: duration})
				}
			}
		}
	}
	return cues, true
}

// --- subtitle segmentation ---

var (
	reWhitespace       = regexp.MustCompile(`\s+`)
	reSpaceBeforePunct = regexp.MustCompile(`\s+([,.;:!?，。；：！？])`)
	reSentenceEnd      = regexp.MustCompile(`[.!?。！？]["'”’)\]]*$`)
	reClauseEnd        = regexp.MustCompile(`[,;:，；：]["'”’)\]]*$`)
)

// normalizeSubtitle cleans caption text as the Swift SubtitleSegmenter does.
func normalizeSubtitle(input string) string {
	s := strings.ReplaceAll(input, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&apos;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, ">>", "")
	s = reWhitespace.ReplaceAllString(s, " ")
	s = reSpaceBeforePunct.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// segmentLimits mirrors SubtitleSegmenter.Limits.
type segmentLimits struct {
	minimumDuration   float64
	idealDuration     float64
	maximumDuration   float64
	maximumCharacters int
}

var defaultLimits = segmentLimits{
	minimumDuration:   2,
	idealDuration:     5.5,
	maximumDuration:   8,
	maximumCharacters: 150,
}

// groupCues merges raw caption cues into readable subtitle segments.
func groupCues(cues []RawCaptionCue) []SubtitleSegment {
	clean := make([]RawCaptionCue, 0, len(cues))
	for _, cue := range cues {
		if strings.TrimSpace(cue.Text) != "" {
			clean = append(clean, cue)
		}
	}
	sort.SliceStable(clean, func(i, j int) bool { return clean[i].Start < clean[j].Start })
	if len(clean) == 0 {
		return nil
	}

	limits := defaultLimits
	var result []SubtitleSegment
	var texts []string
	start, end := clean[0].Start, clean[0].End()

	flush := func() {
		text := normalizeSubtitle(strings.Join(texts, " "))
		texts = texts[:0]
		if text == "" {
			return
		}
		result = append(result, SubtitleSegment{
			ID:       fmt.Sprintf("segment-%d-%d", len(result), int(math.Round(start*1000))),
			Start:    start,
			End:      math.Max(end, start+0.2),
			Original: text,
		})
	}

	for _, cue := range clean {
		if len(texts) == 0 {
			start = cue.Start
			end = cue.End()
		}
		texts = append(texts, normalizeSubtitle(cue.Text))
		if cue.End() > end {
			end = cue.End()
		}
		merged := normalizeSubtitle(strings.Join(texts, " "))
		elapsed := end - start
		sentenceEnd := reSentenceEnd.MatchString(merged)
		clauseEnd := reClauseEnd.MatchString(merged)
		shouldFlush := (sentenceEnd && elapsed >= limits.minimumDuration) ||
			(clauseEnd && elapsed >= limits.idealDuration) ||
			elapsed >= limits.maximumDuration ||
			len([]rune(merged)) >= limits.maximumCharacters
		if shouldFlush {
			flush()
		}
	}
	flush()
	return result
}

// --- metadata enrichment ---

// enrichMetadata fills in title/channel/duration from oEmbed or the watch
// page when the InnerTube response lacked them, as the Swift version does.
func enrichMetadata(ctx context.Context, videoID string, result *TranscriptFetchResult) *TranscriptFetchResult {
	if result.Title != placeholderTitle && result.Title != "" {
		return result
	}
	snapshot, err := fetchPageMetadata(ctx, videoID)
	if err != nil {
		return result
	}
	if isPlaceholderTitle(result.Title) && snapshot.Title != "" {
		result.Title = snapshot.Title
	}
	if (result.Channel == "" || result.Channel == placeholderChannel) && snapshot.Channel != "" {
		result.Channel = snapshot.Channel
	}
	if result.Duration == nil {
		result.Duration = snapshot.Duration
	}
	return result
}

func isPlaceholderTitle(title string) bool {
	t := strings.TrimSpace(title)
	return t == "" || t == placeholderTitle
}

type pageMetadata struct {
	Title    string
	Channel  string
	Duration *float64
}

func fetchPageMetadata(ctx context.Context, videoID string) (pageMetadata, error) {
	// oEmbed first: cheap and reliable for title/author.
	if meta, ok := fetchOEmbed(ctx, videoID); ok {
		return meta, nil
	}
	// Fall back to scraping the watch page.
	watchURL := canonicalURL(videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, watchURL, nil)
	if err != nil {
		return pageMetadata{}, err
	}
	req.Header.Set("Referer", "https://www.youtube.com/")
	req.Header.Set("User-Agent", ytUserAgent)
	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return pageMetadata{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return pageMetadata{}, err
	}
	return parseWatchPage(string(data)), nil
}

func fetchOEmbed(ctx context.Context, videoID string) (pageMetadata, bool) {
	oembed := "https://www.youtube.com/oembed?url=" + url.QueryEscape(canonicalURL(videoID)) + "&format=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oembed, nil)
	if err != nil {
		return pageMetadata{}, false
	}
	req.Header.Set("User-Agent", ytUserAgent)
	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return pageMetadata{}, false
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return pageMetadata{}, false
	}
	var oembedJSON map[string]any
	if err := json.Unmarshal(data, &oembedJSON); err != nil {
		return pageMetadata{}, false
	}
	title, _ := oembedJSON["title"].(string)
	title = strings.TrimSpace(title)
	if isPlaceholderTitle(title) {
		return pageMetadata{}, false
	}
	channel, _ := oembedJSON["author_name"].(string)
	return pageMetadata{Title: title, Channel: strings.TrimSpace(channel)}, true
}

var (
	reOGTitle1      = regexp.MustCompile(`(?s)<meta[^>]+(?:property|name)="(?:og:title|title)"[^>]+content="([^"]+)"`)
	reOGTitle2      = regexp.MustCompile(`(?s)<meta[^>]+content="([^"]+)"[^>]+(?:property|name)="(?:og:title|title)"`)
	reHTMLTitle     = regexp.MustCompile(`(?s)<title>(.+?)</title>`)
	reChannel1      = regexp.MustCompile(`<link itemprop="name" content="([^"]+)">`)
	reChannel2      = regexp.MustCompile(`"author":"([^"]+)"`)
	reLengthSeconds = regexp.MustCompile(`"lengthSeconds":"(\d+)"`)
)

func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
		"&lt;", "<",
		"&gt;", ">",
	)
	return r.Replace(s)
}

func parseWatchPage(html string) pageMetadata {
	var meta pageMetadata
	title := ""
	if m := reOGTitle1.FindStringSubmatch(html); m != nil {
		title = m[1]
	} else if m := reOGTitle2.FindStringSubmatch(html); m != nil {
		title = m[1]
	} else if m := reHTMLTitle.FindStringSubmatch(html); m != nil {
		title = m[1]
	}
	if title != "" {
		title = htmlUnescape(title)
		title = strings.TrimSuffix(title, " - YouTube")
		title = strings.TrimSpace(title)
		meta.Title = title
	}
	if m := reChannel1.FindStringSubmatch(html); m != nil {
		meta.Channel = htmlUnescape(m[1])
	} else if m := reChannel2.FindStringSubmatch(html); m != nil {
		meta.Channel = htmlUnescape(m[1])
	}
	if m := reLengthSeconds.FindStringSubmatch(html); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			meta.Duration = &v
		}
	}
	return meta
}

var errUnused = errors.New("unused")

func firstRegexMatch(pattern, text string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
