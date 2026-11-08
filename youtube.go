package main

import (
	"net/url"
	"regexp"
	"strings"
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// videoID extracts an 11-character YouTube video ID from a URL or bare ID.
func videoID(fromInput string) string {
	value := strings.TrimSpace(fromInput)
	if videoIDPattern.MatchString(value) {
		return value
	}

	normalized := value
	if !strings.HasPrefix(value, "http") {
		normalized = "https://" + value
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())

	if host == "youtu.be" || strings.HasSuffix(host, ".youtu.be") {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) > 0 {
			return validatedID(parts[0])
		}
		return ""
	}

	if host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") {
		return ""
	}
	if u.Path == "/watch" {
		q := u.Query()
		if v := q.Get("v"); v != "" {
			return validatedID(v)
		}
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 {
		switch parts[0] {
		case "shorts", "embed", "v", "live":
			return validatedID(parts[1])
		}
	}
	return ""
}

func canonicalURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

func validatedID(candidate string) string {
	if len(candidate) > 11 {
		candidate = candidate[:11]
	}
	if videoIDPattern.MatchString(candidate) {
		return candidate
	}
	return ""
}
