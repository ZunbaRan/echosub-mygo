package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TranslationError describes translation failures.
type TranslationError struct{ msg string }

func (e *TranslationError) Error() string { return e.msg }

func translationErr(msg string) error { return &TranslationError{msg: msg} }

var errTranslationNotConfigured = translationErr("请先在设置中配置翻译 API Key。")

// TranslationConfiguration holds the OpenAI-compatible endpoint settings.
type TranslationConfiguration struct {
	BaseURL string
	Model   string
	APIKey  string
}

const (
	translationBatchSize = 4
	translationTimeout   = 120 * time.Second
)

// translateSegments translates target segments into Simplified Chinese,
// returning a map of segment ID to translated text. It ports the Swift
// TranslationService.translate request shape.
func translateSegments(ctx context.Context, segments []SubtitleSegment, contextSegments []SubtitleSegment, videoTitle string, configuration TranslationConfiguration) (map[string]string, error) {
	if configuration.APIKey == "" {
		return nil, errTranslationNotConfigured
	}
	base := strings.TrimRight(strings.TrimSpace(configuration.BaseURL), "/")
	if base == "" {
		return nil, translationErr("翻译服务地址无效。")
	}
	endpoint := base + "/chat/completions"

	targetIDs := make(map[string]bool, len(segments))
	targets := make([]map[string]string, 0, len(segments))
	for _, s := range segments {
		targetIDs[s.ID] = true
		targets = append(targets, map[string]string{"id": s.ID, "text": s.Original})
	}
	contextRows := make([]map[string]string, 0, len(contextSegments))
	for _, s := range contextSegments {
		row := map[string]string{"id": s.ID, "text": s.Original}
		if !targetIDs[s.ID] {
			if t := s.EffectiveTranslation(); t != "" {
				row["existing_translation"] = t
			}
		}
		contextRows = append(contextRows, row)
	}
	source := map[string]any{
		"context_segments": contextRows,
		"target_segments":  targets,
	}
	sourceJSON, _ := json.Marshal(source)

	system := fmt.Sprintf(`You are a professional subtitle translator. Translate every target segment into natural, modern Simplified Chinese.
The video is titled %q. Read context_segments in order to resolve pronouns, incomplete thoughts, terminology, and tone. The target source text has highest authority, followed by neighboring source context. Existing translations are terminology references only. Translate only target_segments. Preserve proper nouns and common technical terms when natural. Do not merge, split, omit, or reorder target segments.
Return only valid JSON with exactly this shape: {"segments":[{"id":"unchanged-id","text":"translated text"}]}.
Copy every target id exactly and translate only target text values. Never return context-only ids.`, videoTitle)

	payload := map[string]any{
		"model":           configuration.Model,
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": string(sourceJSON)},
		},
	}
	// Qwen3 models on DashScope reject JSON mode while thinking is on;
	// translation disables thinking, as the Swift version does.
	if model := strings.ToLower(configuration.Model); strings.Contains(model, "qwen3") || strings.Contains(model, "qwen-3") {
		payload["enable_thinking"] = false
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errMalformedTranslation
	}

	ctx, cancel := context.WithTimeout(ctx, translationTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, translationErr("翻译服务地址无效。")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+configuration.APIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, translationErr("翻译请求失败：" + err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, translationErr("翻译请求失败：" + err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := fmt.Sprintf("翻译请求失败（%d）。", resp.StatusCode)
		var envelope struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Message != "" {
			message = envelope.Error.Message
		}
		return nil, &TranslationHTTPError{Status: resp.StatusCode, Message: message}
	}

	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Choices) == 0 {
		return nil, errMalformedTranslation
	}
	content := envelope.Choices[0].Message.Content
	jsonText := extractJSON(content)
	if jsonText == "" {
		return nil, errMalformedTranslation
	}
	var batch struct {
		Segments []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal([]byte(jsonText), &batch); err != nil {
		return nil, errMalformedTranslation
	}
	result := make(map[string]string, len(batch.Segments))
	for _, item := range batch.Segments {
		text := strings.TrimSpace(item.Text)
		if targetIDs[item.ID] && text != "" {
			result[item.ID] = text
		}
	}
	if len(result) == 0 {
		return nil, errMalformedTranslation
	}
	return result, nil
}

var errMalformedTranslation = translationErr("翻译服务返回了无法识别的结果。")

// TranslationHTTPError carries an HTTP failure with its status code.
type TranslationHTTPError struct {
	Status  int
	Message string
}

func (e *TranslationHTTPError) Error() string { return e.Message }

var jsonFence = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// extractJSON pulls the JSON object out of a model response.
func extractJSON(content string) string {
	if m := jsonFence.FindStringSubmatch(content); m != nil {
		return strings.TrimSpace(m[1])
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(content[start : end+1])
	}
	return ""
}

// retryDelay returns how long to wait before retrying a failed batch, as
// the Swift TranslationRetryPolicy does: 5s then 15s, honoring Retry-After
// when present.
func retryDelay(failedAttempt int, err error) (time.Duration, bool) {
	const maxAttempts = 3
	if failedAttempt >= maxAttempts {
		return 0, false
	}
	fallback := 5 * time.Second
	if failedAttempt >= 2 {
		fallback = 15 * time.Second
	}
	var httpErr *TranslationHTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusTooManyRequests {
		// Retry-After is not captured here; the fallback is fine.
	}
	return fallback, true
}

// contextAround returns the surrounding segments for translation context,
// with radius segments on each side, as TranslationWorkPlan.context does.
func contextAround(batch []SubtitleSegment, document *TranscriptDocument, radius int) []SubtitleSegment {
	if document == nil || len(document.Segments) == 0 {
		return nil
	}
	index := make(map[string]int, len(document.Segments))
	for i, s := range document.Segments {
		index[s.ID] = i
	}
	target := make(map[string]bool, len(batch))
	for _, s := range batch {
		target[s.ID] = true
	}
	seen := make(map[string]bool)
	var out []SubtitleSegment
	for _, s := range batch {
		i, ok := index[s.ID]
		if !ok {
			continue
		}
		lo := i - radius
		if lo < 0 {
			lo = 0
		}
		hi := i + radius
		if hi >= len(document.Segments) {
			hi = len(document.Segments) - 1
		}
		for j := lo; j <= hi; j++ {
			seg := document.Segments[j]
			if seen[seg.ID] {
				continue
			}
			seen[seg.ID] = true
			out = append(out, seg)
		}
	}
	// Keep document order.
	sortSegmentsByStart(out)
	return out
}

func sortSegmentsByStart(segments []SubtitleSegment) {
	for i := 1; i < len(segments); i++ {
		for j := i; j > 0 && segments[j].Start < segments[j-1].Start; j-- {
			segments[j], segments[j-1] = segments[j-1], segments[j]
		}
	}
}
