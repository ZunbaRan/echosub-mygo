package main

import (
	"context"
	"testing"
	"time"
)

// TestFetchTranscriptLive exercises the real subtitle pipeline against
// YouTube. Run explicitly: go test -run TestFetchTranscriptLive -v
func TestFetchTranscriptLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := FetchTranscript(ctx, "M7lc1UVf-VE", "")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if result.Document == nil || len(result.Document.Segments) == 0 {
		t.Fatal("no segments")
	}
	provider := ""
	if result.Document.Provider != nil {
		provider = string(*result.Document.Provider)
	}
	t.Logf("provider=%s title=%q channel=%q segments=%d lang=%s",
		provider, result.Title, result.Channel, len(result.Document.Segments), result.Document.SourceLanguage)
	for i := 0; i < 3 && i < len(result.Document.Segments); i++ {
		seg := result.Document.Segments[i]
		t.Logf("  [%0.1f-%0.1f] %s", seg.Start, seg.End, seg.Original)
	}
}

func TestFetchYTDLPLive(t *testing.T) {
	if ytdlpExecutable() == "" {
		t.Skip("yt-dlp not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := fetchYTDLP(ctx, "M7lc1UVf-VE")
	if err != nil {
		t.Fatalf("yt-dlp fetch failed: %v", err)
	}
	provider := ""
	if result.Document.Provider != nil {
		provider = string(*result.Document.Provider)
	}
	t.Logf("provider=%s segments=%d", provider, len(result.Document.Segments))
}
