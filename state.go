package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// AppState is the central state of the EchoSub Go port. It mirrors the
// Swift AppState's responsibilities: library, current video, playback
// time, transcript fetching, translation, and window invalidation.
//
// All mutable state is guarded by mu. The library is copy-on-write:
// mutations replace slices and maps instead of editing them in place,
// so the UI can read snapshots without locking.
type AppState struct {
	mu sync.Mutex

	store       *LibraryStore
	settings    *SettingsStore
	credentials *CredentialStore

	library   *Library
	settingsD Settings
	creds     Credentials

	currentVideoID string
	playbackTime   float64
	playerState    int // YouTube: -1 unstarted, 0 ended, 1 playing, 2 paused, 3 buffering, 5 cued
	playerReady    bool
	lastError      string

	translateCancel map[string]context.CancelFunc

	// windows to invalidate on state changes.
	mainWin   *mygo.Window
	floatWin  *mygo.Window
	lyricsWin *mygo.Window

	thumbnails   map[string]ui.ImageSource
	thumbLoading map[string]bool
}

// NewAppState loads persisted state.
func NewAppState() *AppState {
	store := NewLibraryStore()
	settings := NewSettingsStore()
	credentials := NewCredentialStore()
	lib := store.Load()
	s := &AppState{
		store:           store,
		settings:        settings,
		credentials:     credentials,
		library:         lib,
		settingsD:       settings.Load(),
		creds:           credentials.Load(),
		translateCancel: map[string]context.CancelFunc{},
		thumbnails:      map[string]ui.ImageSource{},
		thumbLoading:    map[string]bool{},
	}
	// Recover videos stuck in "translating" after a restart.
	changed := false
	for i := range lib.Videos {
		if lib.Videos[i].Status == StatusTranslating {
			if lib.Transcripts[lib.Videos[i].ID] == nil {
				lib.Videos[i].Status = StatusIdle
			} else {
				lib.Videos[i].Status = StatusReady
			}
			changed = true
		}
	}
	if changed {
		s.persistLocked()
	}
	if videos := lib.VisibleVideos(); len(videos) > 0 {
		s.currentVideoID = videos[0].ID
		s.playbackTime = videos[0].Progress
	}
	return s
}

// uiSnapshot is an immutable view of the state the UI draws. Slice and
// map headers are shared with the library, which is copy-on-write, so
// reading them after the lock is released is safe.
type uiSnapshot struct {
	videos            []VideoItem
	hidden            []VideoItem
	transcripts       map[string]*TranscriptDocument
	currentID         string
	playback          float64
	playerState       int
	playerReady       bool
	lastError         string
	settings          Settings
	translationKeySet bool
}

func (s *uiSnapshot) currentVideo() *VideoItem {
	if s.currentID == "" {
		return nil
	}
	for i := range s.videos {
		if s.videos[i].ID == s.currentID {
			return &s.videos[i]
		}
	}
	return nil
}

func (s *uiSnapshot) currentTranscript() *TranscriptDocument {
	if s.currentID == "" {
		return nil
	}
	return s.transcripts[s.currentID]
}

func (s *uiSnapshot) currentSegmentIndex() int {
	doc := s.currentTranscript()
	if doc == nil {
		return -1
	}
	idx := -1
	for i, seg := range doc.Segments {
		if seg.Start <= s.playback {
			idx = i
		} else {
			break
		}
	}
	return idx
}

// snapshot returns an immutable view of the current state for the UI.
func (s *AppState) snapshot() uiSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uiSnapshot{
		videos:            s.library.VisibleVideos(),
		hidden:            s.library.HiddenVideos(),
		transcripts:       s.library.Transcripts,
		currentID:         s.currentVideoID,
		playback:          s.playbackTime,
		playerState:       s.playerState,
		playerReady:       s.playerReady,
		lastError:         s.lastError,
		settings:          s.settingsD,
		translationKeySet: s.creds.TranslationAPIKey != "",
	}
}

// AddVideo parses input, adds or selects the video, and fetches subtitles.
func (s *AppState) AddVideo(input string) error {
	id := videoID(input)
	if id == "" {
		return fmt.Errorf("无法识别这个 YouTube 链接。")
	}
	s.mu.Lock()
	if existing := s.library.findVideo(id); existing != nil {
		if s.library.isHidden(id) {
			s.unhideLocked(id)
			s.persistLocked()
		}
		s.mu.Unlock()
		s.SelectVideo(id)
		s.InvalidateAll()
		return nil
	}
	video := newVideoItem(id)
	videos := make([]VideoItem, 0, len(s.library.Videos)+1)
	videos = append(videos, *video)
	videos = append(videos, s.library.Videos...)
	s.library.Videos = videos
	s.currentVideoID = id
	s.playbackTime = 0
	s.lastError = ""
	s.persistLocked()
	s.mu.Unlock()
	s.InvalidateAll()
	go s.fetchTranscript(id)
	return nil
}

// SelectVideo switches the current video.
func (s *AppState) SelectVideo(id string) {
	s.mu.Lock()
	if s.library.videoIndex(id) < 0 {
		s.mu.Unlock()
		return
	}
	s.saveProgressLocked()
	s.currentVideoID = id
	if v := s.library.findVideo(id); v != nil {
		s.playbackTime = v.Progress
	}
	s.lastError = ""
	needFetch := s.library.Transcripts[id] == nil
	needTranslate := !needFetch && videoHasUntranslated(s.library.Transcripts[id])
	s.mu.Unlock()

	if needFetch {
		go s.fetchTranscript(id)
	} else if needTranslate {
		go s.translateMissing(id)
	}
	s.InvalidateAll()
}

// PlayVideoFromBeginning restarts a video.
func (s *AppState) PlayVideoFromBeginning(id string) {
	s.mu.Lock()
	if idx := s.library.videoIndex(id); idx >= 0 {
		videos := append([]VideoItem(nil), s.library.Videos...)
		videos[idx].Progress = 0
		s.library.Videos = videos
		s.persistLocked()
	}
	s.mu.Unlock()
	s.SelectVideo(id)
}

// RemoveVideo deletes a video from the library.
func (s *AppState) RemoveVideo(id string) {
	s.mu.Lock()
	if cancel := s.translateCancel[id]; cancel != nil {
		cancel()
		delete(s.translateCancel, id)
	}
	// Copy-on-write the transcript map without this video.
	transcripts := make(map[string]*TranscriptDocument, len(s.library.Transcripts))
	for k, v := range s.library.Transcripts {
		if k != id {
			transcripts[k] = v
		}
	}
	s.library.Transcripts = transcripts

	videos := make([]VideoItem, 0, len(s.library.Videos))
	for _, v := range s.library.Videos {
		if v.ID != id {
			videos = append(videos, v)
		}
	}
	s.library.Videos = videos

	kept := make([]string, 0, len(s.library.HiddenVideoIDs))
	for _, hid := range s.library.HiddenVideoIDs {
		if hid != id {
			kept = append(kept, hid)
		}
	}
	s.library.HiddenVideoIDs = kept
	s.library.hidden = nil
	s.library.initHidden()

	if s.currentVideoID == id {
		visible := s.library.VisibleVideos()
		if len(visible) > 0 {
			s.currentVideoID = visible[0].ID
			s.playbackTime = visible[0].Progress
		} else {
			s.currentVideoID = ""
			s.playbackTime = 0
		}
	}
	s.persistLocked()
	s.mu.Unlock()
	s.InvalidateAll()
}

// HideVideo hides a video from the playlist without deleting it.
func (s *AppState) HideVideo(id string) {
	s.mu.Lock()
	if s.library.isHidden(id) {
		s.mu.Unlock()
		return
	}
	s.library.HiddenVideoIDs = append(append([]string(nil), s.library.HiddenVideoIDs...), id)
	s.library.hidden = nil
	s.library.initHidden()
	if s.currentVideoID == id {
		visible := s.library.VisibleVideos()
		if len(visible) > 0 {
			s.currentVideoID = visible[0].ID
			s.playbackTime = visible[0].Progress
		} else {
			s.currentVideoID = ""
			s.playbackTime = 0
		}
	}
	s.persistLocked()
	s.mu.Unlock()
	s.InvalidateAll()
}

// UnhideVideo puts a hidden video back in the playlist.
func (s *AppState) UnhideVideo(id string) {
	s.mu.Lock()
	s.unhideLocked(id)
	s.persistLocked()
	s.mu.Unlock()
	s.InvalidateAll()
}

func (s *AppState) unhideLocked(id string) {
	kept := make([]string, 0, len(s.library.HiddenVideoIDs))
	for _, hid := range s.library.HiddenVideoIDs {
		if hid != id {
			kept = append(kept, hid)
		}
	}
	s.library.HiddenVideoIDs = kept
	s.library.hidden = nil
	s.library.initHidden()
}

// updatePlayback records the current playback position (player bridge).
func (s *AppState) updatePlayback(seconds float64) {
	s.mu.Lock()
	s.playbackTime = seconds
	s.mu.Unlock()
}

// saveProgress persists the current video's progress.
func (s *AppState) saveProgress() {
	s.mu.Lock()
	s.saveProgressLocked()
	s.mu.Unlock()
}

func (s *AppState) saveProgressLocked() {
	if s.currentVideoID == "" {
		return
	}
	if idx := s.library.videoIndex(s.currentVideoID); idx >= 0 {
		videos := append([]VideoItem(nil), s.library.Videos...)
		videos[idx].Progress = s.playbackTime
		s.library.Videos = videos
	}
	s.persistLocked()
}

// setPlayerReady marks the player as ready.
func (s *AppState) setPlayerReady(ready bool) {
	s.mu.Lock()
	s.playerReady = ready
	s.mu.Unlock()
}

// setPlayerState records the YouTube player state.
func (s *AppState) setPlayerState(state int) {
	s.mu.Lock()
	s.playerState = state
	s.mu.Unlock()
}

// setPlayerError records a player error.
func (s *AppState) setPlayerError(code int) {
	s.mu.Lock()
	s.lastError = fmt.Sprintf("播放器错误（%d）", code)
	s.mu.Unlock()
	s.InvalidateAll()
}

// Persist writes the library to disk.
func (s *AppState) Persist() {
	s.mu.Lock()
	s.persistLocked()
	s.mu.Unlock()
}

func (s *AppState) persistLocked() {
	s.store.Save(s.library)
}

// UpdateSettings replaces the settings and persists them.
func (s *AppState) UpdateSettings(fn func(*Settings)) {
	s.mu.Lock()
	fn(&s.settingsD)
	s.settings.Save(s.settingsD)
	s.mu.Unlock()
	s.InvalidateAll()
}

// Settings returns a copy of the current settings.
func (s *AppState) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsD
}

// UpdateCredentials replaces the API keys and persists them.
func (s *AppState) UpdateCredentials(fn func(*Credentials)) {
	s.mu.Lock()
	fn(&s.creds)
	s.credentials.Save(s.creds)
	s.mu.Unlock()
}

// Credentials returns a copy of the current credentials.
func (s *AppState) Credentials() Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds
}

// InvalidateAll refreshes every window.
func (s *AppState) InvalidateAll() {
	for _, w := range []*mygo.Window{s.mainWin, s.floatWin, s.lyricsWin} {
		if w != nil {
			w.Invalidate()
		}
	}
}

// InvalidateMain refreshes the main window only.
func (s *AppState) InvalidateMain() {
	if s.mainWin != nil {
		s.mainWin.Invalidate()
	}
}

func videoHasUntranslated(doc *TranscriptDocument) bool {
	if doc == nil {
		return false
	}
	for _, seg := range doc.Segments {
		if seg.EffectiveTranslation() == "" {
			return true
		}
	}
	return false
}

// fetchTranscript downloads subtitles for a video and updates state.
func (s *AppState) fetchTranscript(videoID string) {
	s.mu.Lock()
	supadataKey := s.creds.SupadataAPIKey
	s.mu.Unlock()

	ctx := context.Background()
	result, err := FetchTranscript(ctx, videoID, supadataKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.library.videoIndex(videoID)
	if idx < 0 {
		return
	}
	if err != nil {
		videos := append([]VideoItem(nil), s.library.Videos...)
		videos[idx].Status = StatusFailed
		s.library.Videos = videos
		if s.currentVideoID == videoID {
			s.lastError = err.Error()
		}
		s.persistLocked()
		s.InvalidateAll()
		return
	}

	doc := result.Document
	// Apply metadata to the video item, copy-on-write.
	videos := append([]VideoItem(nil), s.library.Videos...)
	video := &videos[idx]
	if result.Title != placeholderTitle && result.Title != "" {
		video.Title = result.Title
	}
	if result.Channel != placeholderChannel && result.Channel != "" {
		video.Channel = result.Channel
	}
	if result.Duration != nil {
		video.Duration = result.Duration
	}
	s.library.Videos = videos
	// Add the transcript, copy-on-write the map.
	transcripts := make(map[string]*TranscriptDocument, len(s.library.Transcripts)+1)
	for k, v := range s.library.Transcripts {
		transcripts[k] = v
	}
	transcripts[videoID] = doc
	s.library.Transcripts = transcripts

	status := StatusReady
	if videoHasUntranslated(doc) {
		status = StatusTranslating
	}
	videos = append([]VideoItem(nil), s.library.Videos...)
	videos[idx].Status = status
	s.library.Videos = videos
	s.persistLocked()
	s.InvalidateAll()

	if videoHasUntranslated(doc) {
		go s.translateMissing(videoID)
	}
}

// translateMissing translates all untranslated segments in batches.
func (s *AppState) translateMissing(videoID string) {
	s.mu.Lock()
	doc := s.library.Transcripts[videoID]
	if doc == nil {
		s.mu.Unlock()
		return
	}
	var missing []SubtitleSegment
	for _, seg := range doc.Segments {
		if seg.EffectiveTranslation() == "" {
			missing = append(missing, seg)
		}
	}
	if len(missing) == 0 {
		s.mu.Unlock()
		return
	}
	if s.translateCancel[videoID] != nil {
		// Already running.
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.translateCancel[videoID] = cancel
	videoTitle := ""
	if v := s.library.findVideo(videoID); v != nil {
		videoTitle = v.Title
	}
	configuration := TranslationConfiguration{
		BaseURL: s.settingsD.TranslationBaseURL,
		Model:   s.settingsD.TranslationModel,
		APIKey:  s.creds.TranslationAPIKey,
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.translateCancel, videoID)
		s.mu.Unlock()
	}()

	if configuration.APIKey == "" {
		s.finishTranslation(videoID, false)
		return
	}

	for start := 0; start < len(missing); start += translationBatchSize {
		select {
		case <-ctx.Done():
			return
		default:
		}
		end := start + translationBatchSize
		if end > len(missing) {
			end = len(missing)
		}
		batch := missing[start:end]

		// Skip segments that already have translations (e.g. from a
		// previous partial run or a newer fetch).
		var targets []SubtitleSegment
		s.mu.Lock()
		current := s.library.Transcripts[videoID]
		s.mu.Unlock()
		for _, seg := range batch {
			found := false
			if current != nil {
				for _, cs := range current.Segments {
					if cs.ID == seg.ID && cs.EffectiveTranslation() != "" {
						found = true
						break
					}
				}
			}
			if !found {
				targets = append(targets, seg)
			}
		}
		if len(targets) == 0 {
			continue
		}

		contextSegments := contextAround(targets, current, 4)
		translations, err := translateSegments(ctx, targets, contextSegments, videoTitle, configuration)
		if err != nil {
			delay, ok := retryDelay(1, err)
			if ok {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				translations, err = translateSegments(ctx, targets, contextSegments, videoTitle, configuration)
			}
			if err != nil {
				continue
			}
		}

		s.mu.Lock()
		doc := s.library.Transcripts[videoID]
		if doc != nil {
			// Copy-on-write: copy the document and its segments before
			// applying translations.
			updated := *doc
			segments := append([]SubtitleSegment(nil), doc.Segments...)
			changed := false
			for i := range segments {
				if text, ok := translations[segments[i].ID]; ok {
					t := text
					segments[i].Translation = &t
					changed = true
				}
			}
			if changed {
				updated.Segments = segments
				transcripts := make(map[string]*TranscriptDocument, len(s.library.Transcripts))
				for k, v := range s.library.Transcripts {
					transcripts[k] = v
				}
				transcripts[videoID] = &updated
				s.library.Transcripts = transcripts
				s.persistLocked()
			}
		}
		s.mu.Unlock()
		s.InvalidateAll()
	}

	s.finishTranslation(videoID, true)
}

// finishTranslation marks a video ready after translation completes.
func (s *AppState) finishTranslation(videoID string, translated bool) {
	s.mu.Lock()
	if idx := s.library.videoIndex(videoID); idx >= 0 {
		videos := append([]VideoItem(nil), s.library.Videos...)
		videos[idx].Status = StatusReady
		s.library.Videos = videos
	}
	s.persistLocked()
	s.mu.Unlock()
	s.InvalidateAll()
}

// RetryTranscript refetches subtitles for the current video.
func (s *AppState) RetryTranscript() {
	s.mu.Lock()
	id := s.currentVideoID
	if id != "" {
		if idx := s.library.videoIndex(id); idx >= 0 {
			videos := append([]VideoItem(nil), s.library.Videos...)
			videos[idx].Status = StatusLoading
			s.library.Videos = videos
		}
	}
	s.persistLocked()
	s.mu.Unlock()
	s.InvalidateAll()
	if id != "" {
		go s.fetchTranscript(id)
	}
}

// Thumbnail returns the decoded thumbnail for a video, downloading it
// once if needed. The returned image must not be mutated.
func (s *AppState) Thumbnail(url string) ui.ImageSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if img, ok := s.thumbnails[url]; ok {
		return img
	}
	if s.thumbLoading[url] {
		return nil
	}
	s.thumbLoading[url] = true
	go func() {
		img := downloadThumbnail(url)
		s.mu.Lock()
		delete(s.thumbLoading, url)
		if img != nil {
			s.thumbnails[url] = img
		}
		s.mu.Unlock()
		if img != nil {
			s.InvalidateMain()
		}
	}()
	return nil
}
