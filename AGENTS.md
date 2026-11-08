# EchoSub (mygo)

Go port of the Swift EchoSub app (`../EchoSub`), native UI drawn with `github.com/egoist/mygo` v0.7.4. It shares `~/Library/Application Support/EchoSub/` (library.json, credentials.json) with the Swift app.

## Commands

- Build: `go build -o /tmp/echosub .` (Go 1.27.3 from Homebrew)
- Check: `go vet ./... && go test -count=1 -run '^$' ./...`
- Live subtitle tests (network): `go test -run TestFetchTranscriptLive -v`
- Run: `/tmp/echosub` (uses the real library and API keys)
- mygo docs: `~/go/pkg/mod/github.com/egoist/mygo@v0.7.4/docs/` (ui docs in `docs/ui/`)

## UI notes

- Main window (`ui.go`): toolbar, then three panes — video library, video + focus stage + transport, subtitle panel. The library leaves the row first as the window shrinks (floating sheet via toolbar); below 900 DIPs the subtitle panel goes under the video. The video letterboxes (`fitVideo`). The design follows `prototype-glass/` (the v2 prototype).
- Overlays (`overlay.go`): floating subtitles change density with window height; desktop lyrics size their window to the text with `c.MeasureText`, and lock with `SetIgnoreMouseEvents`.
- `Element.Shadow` is a box shadow, not a text shadow; overlay text uses `overlayText` (a blurred black copy) for legibility.
- `Element.Bounds()` was empty for the lyrics plate; measure text instead.
- `Window.CapturePage()` gives PNGs of native UI windows (the web view shows as a hole) — handy for checking layouts, since the shell has no screen-recording permission.
- `prototype-glass/` is the current HTML prototype; `prototype/` is the earlier one (`python3 -m http.server` in either folder). `prototype/data.js` contains real library data — do not commit it.
