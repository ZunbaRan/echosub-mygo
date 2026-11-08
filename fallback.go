package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file ports the Swift app's two fallback subtitle providers:
// yt-dlp (a local binary) and Supadata (a hosted transcript API). The
// direct YouTube pipeline lives in transcript.go; FetchTranscript there
// chains direct → yt-dlp → Supadata, like YouTubeTranscriptService.swift.

func fetchYTDLP(ctx context.Context, videoID string) (*TranscriptFetchResult, error) {
	exe := ytdlpExecutable()
	if exe == "" {
		return nil, errors.New("未检测到 yt-dlp。可通过 Homebrew 安装：brew install yt-dlp")
	}
	cmd := exec.CommandContext(ctx, exe,
		"--dump-single-json",
		"--skip-download",
		"--no-warnings",
		"--no-playlist",
		"--socket-timeout", "20",
		canonicalURL(videoID),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return nil, errors.New("yt-dlp 未能读取这个视频的字幕。")
		}
		return nil, fmt.Errorf("yt-dlp：%s", detail)
	}

	var info ytdlpInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, errors.New("没有找到可用的字幕轨道。")
	}
	language, format, generated, ok := bestYTDLPTrack(&info)
	if !ok {
		return nil, errors.New("没有找到可用的字幕轨道。")
	}
	return fetchYTDLPTrack(ctx, format.URL, videoID, language, generated, &info)
}

// ytdlpExecutable finds a yt-dlp binary: bundled with the app first, then
// PATH, then the usual install locations (mirrors the Swift provider).
func ytdlpExecutable() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		// <bundle>/Contents/MacOS/EchoSub → <bundle>/Contents/Resources/yt-dlp
		candidates = append(candidates, filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources", "yt-dlp"))
	}
	if path, err := exec.LookPath("yt-dlp"); err == nil {
		candidates = append(candidates, path)
	}
	home, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(home, ".local/bin/yt-dlp"),
		"/opt/homebrew/bin/yt-dlp",
		"/usr/local/bin/yt-dlp",
		"/usr/bin/yt-dlp",
	)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

type ytdlpInfo struct {
	Title             string                   `json:"title"`
	Uploader          string                   `json:"uploader"`
	Channel           string                   `json:"channel"`
	Duration          *float64                 `json:"duration"`
	Subtitles         map[string][]ytdlpFormat `json:"subtitles"`
	AutomaticCaptions map[string][]ytdlpFormat `json:"automatic_captions"`
}

type ytdlpFormat struct {
	Ext string `json:"ext"`
	URL string `json:"url"`
}

func bestYTDLPTrack(info *ytdlpInfo) (language string, format ytdlpFormat, generated bool, ok bool) {
	if lang, f, found := bestYTDLPTrackMap(info.Subtitles); found {
		return lang, f, false, true
	}
	if lang, f, found := bestYTDLPTrackMap(info.AutomaticCaptions); found {
		return lang, f, true, true
	}
	return "", ytdlpFormat{}, false, false
}

func bestYTDLPTrackMap(tracks map[string][]ytdlpFormat) (string, ytdlpFormat, bool) {
	if len(tracks) == 0 {
		return "", ytdlpFormat{}, false
	}
	languages := make([]string, 0, len(tracks))
	for language := range tracks {
		languages = append(languages, language)
	}
	sort.SliceStable(languages, func(i, j int) bool {
		return ytdlpLanguageScore(languages[i]) > ytdlpLanguageScore(languages[j])
	})
	for _, language := range languages {
		for _, format := range tracks[language] {
			if format.Ext == "json3" && format.URL != "" {
				return language, format, true
			}
		}
	}
	return "", ytdlpFormat{}, false
}

func ytdlpLanguageScore(language string) int {
	value := strings.ToLower(language)
	switch {
	case value == "en":
		return 300
	case value == "en-orig":
		return 290
	case strings.HasPrefix(value, "en-"):
		return 280
	default:
		return 0
	}
}

func fetchYTDLPTrack(ctx context.Context, trackURL, videoID, language string, generated bool, info *ytdlpInfo) (*TranscriptFetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackURL, nil)
	if err != nil {
		return nil, errors.New("没有找到可用的字幕轨道。")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15")
	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp：%s", err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("yt-dlp：%s", err.Error())
	}
	cues, ok := parseCues(data)
	if !ok || len(cues) == 0 {
		return nil, errors.New("yt-dlp：字幕数据格式无法识别。")
	}
	segments := groupCues(cues)
	if len(segments) == 0 {
		return nil, errors.New("没有找到可用的字幕轨道。")
	}
	provider := ProviderYTDLP
	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = placeholderTitle
	}
	channel := strings.TrimSpace(info.Uploader)
	if channel == "" {
		channel = strings.TrimSpace(info.Channel)
	}
	if channel == "" {
		channel = placeholderChannel
	}
	return enrichMetadata(ctx, videoID, &TranscriptFetchResult{
		Document: &TranscriptDocument{
			VideoID:        videoID,
			SourceLanguage: language,
			IsGenerated:    generated,
			Segments:       segments,
			Provider:       &provider,
		},
		Title:    title,
		Channel:  channel,
		Duration: info.Duration,
	}), nil
}

// fetchSupadata ports SupadataTranscriptProvider: a hosted transcript API
// that may answer with a job to poll.
func fetchSupadata(ctx context.Context, videoID, apiKey string) (*TranscriptFetchResult, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, errors.New("Supadata API Key 未配置（设置 → 字幕来源）。")
	}
	endpoint := "https://api.supadata.ai/v1/transcript?url=" +
		url.QueryEscape(canonicalURL(videoID)) + "&lang=en&text=false&mode=native"
	for attempts := 12; attempts > 0; attempts-- {
		result, next, err := supadataOnce(ctx, endpoint, key, videoID)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return enrichMetadata(ctx, videoID, result), nil
		}
		if next == "" {
			return nil, transcriptErr("Supadata：任务没有返回字幕。")
		}
		endpoint = next
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, transcriptErr("Supadata：任务没有返回字幕。")
}

type supadataEnvelope struct {
	Content []struct {
		Text     string  `json:"text"`
		Offset   float64 `json:"offset"`
		Duration float64 `json:"duration"`
	} `json:"content"`
	Lang   string `json:"lang"`
	JobID  string `json:"jobId"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// supadataOnce performs one request. It returns the transcript result, the
// next poll URL when the API answered with a job, or an error.
func supadataOnce(ctx context.Context, endpoint, apiKey, videoID string) (*TranscriptFetchResult, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", transcriptErr("Supadata：请求地址无效。")
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := transcriptHTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("Supadata：%s", err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", fmt.Errorf("Supadata：%s", err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope supadataEnvelope
		detail := ""
		if json.Unmarshal(data, &envelope) == nil && envelope.Error != nil {
			detail = envelope.Error.Message
		}
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return nil, "", transcriptErr(fmt.Sprintf("Supadata：%d · %s", resp.StatusCode, detail))
	}
	var envelope supadataEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, "", errors.New("Supadata：返回格式无法识别。")
	}
	if len(envelope.Content) > 0 {
		cues := make([]RawCaptionCue, 0, len(envelope.Content))
		for _, chunk := range envelope.Content {
			cues = append(cues, RawCaptionCue{
				Text:     chunk.Text,
				Start:    chunk.Offset / 1000,
				Duration: chunk.Duration / 1000,
			})
		}
		segments := groupCues(cues)
		if len(segments) == 0 {
			return nil, "", errors.New("没有找到可用的字幕轨道。")
		}
		provider := ProviderSupadata
		language := envelope.Lang
		if language == "" {
			language = "en"
		}
		return &TranscriptFetchResult{
			Document: &TranscriptDocument{
				VideoID:        videoID,
				SourceLanguage: language,
				IsGenerated:    false,
				Segments:       segments,
				Provider:       &provider,
			},
			Title:    placeholderTitle,
			Channel:  placeholderChannel,
			Duration: nil,
		}, "", nil
	}
	if envelope.JobID != "" {
		return nil, "https://api.supadata.ai/v1/transcript/" + url.PathEscape(envelope.JobID), nil
	}
	if envelope.Error != nil && envelope.Error.Message != "" {
		return nil, "", transcriptErr("Supadata：" + envelope.Error.Message)
	}
	return nil, "", nil
}
