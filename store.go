package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// echoDir is the shared Application Support directory, the same one the
// Swift app uses, so both versions see the same library.
func echoDir() string {
	base, err := os.UserHomeDir()
	if err != nil {
		base = "."
	}
	dir := filepath.Join(base, "Library", "Application Support", "EchoSub")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// LibraryStore persists the library as library.json.
type LibraryStore struct {
	mu   sync.Mutex
	path string
}

func NewLibraryStore() *LibraryStore {
	return &LibraryStore{path: filepath.Join(echoDir(), "library.json")}
}

func (s *LibraryStore) Load() *Library {
	s.mu.Lock()
	defer s.mu.Unlock()
	lib := &Library{Transcripts: map[string]*TranscriptDocument{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return lib
	}
	if err := json.Unmarshal(data, lib); err != nil {
		return &Library{Transcripts: map[string]*TranscriptDocument{}}
	}
	if lib.Transcripts == nil {
		lib.Transcripts = map[string]*TranscriptDocument{}
	}
	lib.initHidden()
	// Intersect hidden IDs with existing videos, as the Swift version does.
	known := make(map[string]bool)
	for _, v := range lib.Videos {
		known[v.ID] = true
	}
	var kept []string
	for _, id := range lib.HiddenVideoIDs {
		if known[id] {
			kept = append(kept, id)
		}
	}
	lib.HiddenVideoIDs = kept
	lib.hidden = nil
	lib.initHidden()
	return lib
}

func (s *LibraryStore) Save(lib *Library) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lib.syncHidden()
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// SettingsStore persists settings as settings.json.
type SettingsStore struct {
	mu   sync.Mutex
	path string
}

func NewSettingsStore() *SettingsStore {
	return &SettingsStore{path: filepath.Join(echoDir(), "settings.json")}
}

func (s *SettingsStore) Load() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := defaultSettings()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return settings
	}
	_ = json.Unmarshal(data, &settings)
	return settings
}

func (s *SettingsStore) Save(settings Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path+".tmp", data, 0o644)
	_ = os.Rename(s.path+".tmp", s.path)
}

// CredentialStore persists API keys as credentials.json.
type CredentialStore struct {
	mu   sync.Mutex
	path string
}

func NewCredentialStore() *CredentialStore {
	return &CredentialStore{path: filepath.Join(echoDir(), "credentials.json")}
}

func (s *CredentialStore) Load() Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	var creds Credentials
	data, err := os.ReadFile(s.path)
	if err != nil {
		return creds
	}
	_ = json.Unmarshal(data, &creds)
	return creds
}

func (s *CredentialStore) Save(creds Credentials) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path+".tmp", data, 0o600)
	_ = os.Rename(s.path+".tmp", s.path)
}
