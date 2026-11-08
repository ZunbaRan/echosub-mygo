package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PlayerBridge receives events from the YouTube player page and forwards
// control commands to it.
type PlayerBridge struct {
	app *AppState
	mw  *mainWindow

	mu             sync.Mutex
	lastProgress   time.Time
	lastInvalidate time.Time
	lastSegment    int
}

// playerEvent is the payload the player page sends through mygo.call.
type playerEvent struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

// Event handles a player page event.
func (p *PlayerBridge) Event(payload string) {
	var evt playerEvent
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		return
	}
	switch evt.Type {
	case "ready":
		p.app.setPlayerReady(true)
		if p.mw.speed != 1 {
			p.mw.setSpeed(p.mw.speed)
		}
	case "time":
		p.onTime(evt.Value)
	case "state":
		state := int(evt.Value)
		p.app.setPlayerState(state)
		if state == 0 {
			// Playback ended: loop modes act here.
			p.mw.onVideoEnded()
		}
	case "error":
		p.app.setPlayerError(int(evt.Value))
	}
}

func (p *PlayerBridge) onTime(seconds float64) {
	now := time.Now()
	p.app.updatePlayback(seconds)

	// Save progress every 10 seconds and when playback pauses.
	if now.Sub(p.lastProgress) > 10*time.Second {
		p.lastProgress = now
		p.app.saveProgress()
	}

	p.mw.keepSentenceLoop(seconds)

	// Invalidate at most every 400ms, and always when the current
	// subtitle segment changes. The desktop lyrics redraw on every tick
	// for their progress bar.
	snap := p.app.snapshot()
	if idx := snap.currentSegmentIndex(); idx != p.lastSegment {
		p.lastSegment = idx
		p.lastInvalidate = now
		p.app.InvalidateAll()
	} else if now.Sub(p.lastInvalidate) > 400*time.Millisecond {
		p.lastInvalidate = now
		p.app.InvalidateMain()
	}
	if p.mw.lyricsWin != nil {
		p.mw.lyricsWin.Invalidate()
	}
}

// playerHTML is the page hosting the YouTube iframe, a port of the Swift
// app's YouTubePlayerView HTML.
func playerHTML(videoID string, start float64) string {
	safeID := strings.ReplaceAll(videoID, "'", "")
	appID := "com.zunbaran.echosub"
	appOrigin := "https://" + appID
	return fmt.Sprintf(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="strict-origin-when-cross-origin">
<style>html,body,#player{width:100%%;height:100%%;margin:0;background:#000;overflow:hidden}</style></head>
<body><div id="player"></div><script src="https://www.youtube.com/iframe_api"></script><script>
var player; var ready=false;
function send(type, value) {
  try { mygo.call("PlayerBridge.Event", JSON.stringify({type:type, value:value})); } catch (e) {}
}
function onYouTubeIframeAPIReady(){
  player=new YT.Player('player',{
    videoId:'%s',
    playerVars:{playsinline:1,rel:0,controls:1,fs:0,start:%d,enablejsapi:1,origin:'%s',widget_referrer:'%s/'},
    events:{
      onReady:function(){ready=true;send('ready',1);},
      onStateChange:function(e){send('state',e.data);},
      onError:function(e){send('error',e.data);}
    }
  });
}
setInterval(function(){if(ready&&player&&player.getCurrentTime)send('time',player.getCurrentTime());},250);
</script></body></html>`, safeID, int(start), appOrigin, appOrigin)
}

// playerBaseURL is the origin the player page loads under, as the Swift
// app's bundle-identifier origin trick.
const playerBaseURL = "https://com.zunbaran.echosub/"

// loadVideo loads a video into the player webview.
func (mw *mainWindow) loadVideo(videoID string, start float64) {
	if mw.player == nil {
		return
	}
	mw.player.Page().LoadHTML(playerHTML(videoID, start), playerBaseURL)
}

// playerEval evaluates a script in the player page, ignoring errors.
func (mw *mainWindow) playerEval(script string) {
	if mw.player == nil {
		return
	}
	_, _ = mw.player.Page().Eval(script)
}

func (mw *mainWindow) playerPlay()  { mw.playerEval("player && player.playVideo();") }
func (mw *mainWindow) playerPause() { mw.playerEval("player && player.pauseVideo();") }
func (mw *mainWindow) playerToggle() {
	mw.playerEval("if(player){player.getPlayerState()===1?player.pauseVideo():player.playVideo();}")
}
func (mw *mainWindow) playerSeek(seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	mw.playerEval(fmt.Sprintf("player && player.seekTo(%f, true);", seconds))
}
func (mw *mainWindow) playerSkip(delta float64) {
	mw.playerEval(fmt.Sprintf("player && player.seekTo(Math.max(0,(player.getCurrentTime?player.getCurrentTime():0)+(%f)), true);", delta))
}

var playbackSpeeds = []float64{0.5, 0.75, 1, 1.25, 1.5}

// setSpeed sets the player's playback rate.
func (mw *mainWindow) setSpeed(rate float64) {
	mw.speed = rate
	mw.playerEval(fmt.Sprintf("player && player.setPlaybackRate && player.setPlaybackRate(%g);", rate))
}

// cycleSpeed moves to the next playback rate.
func (mw *mainWindow) cycleSpeed() {
	next := playbackSpeeds[0]
	for i, r := range playbackSpeeds {
		if r == mw.speed {
			next = playbackSpeeds[(i+1)%len(playbackSpeeds)]
		}
	}
	mw.setSpeed(next)
}

// seekTo moves the player and the app's playback time at once, so the
// subtitles follow before the player reports the new time.
func (mw *mainWindow) seekTo(seconds float64) {
	mw.app.updatePlayback(max(seconds, 0))
	mw.playerSeek(seconds)
	mw.app.InvalidateAll()
}

// sentence returns the current transcript's segments, the index of the
// current one (-1 before the first) and the playback time.
func (mw *mainWindow) sentence() ([]SubtitleSegment, int, float64) {
	snap := mw.app.snapshot()
	doc := snap.currentTranscript()
	if doc == nil {
		return nil, -1, snap.playback
	}
	return doc.Segments, snap.currentSegmentIndex(), snap.playback
}

// prevSentence goes to the start of the current sentence, or to the one
// before when the current one has just started, as music players do.
func (mw *mainWindow) prevSentence() {
	segs, idx, now := mw.sentence()
	if len(segs) == 0 {
		return
	}
	target := segs[0].Start
	if idx >= 0 {
		target = segs[idx].Start
		if now-segs[idx].Start < 1.5 && idx > 0 {
			target = segs[idx-1].Start
		}
	}
	mw.seekToSentence(target)
}

func (mw *mainWindow) nextSentence() {
	segs, idx, _ := mw.sentence()
	if len(segs) == 0 {
		return
	}
	mw.seekToSentence(segs[min(idx+1, len(segs)-1)].Start)
}

func (mw *mainWindow) replaySentence() {
	segs, idx, _ := mw.sentence()
	if len(segs) == 0 {
		return
	}
	mw.seekToSentence(segs[max(idx, 0)].Start)
	mw.playerPlay()
}

// seekToSentence seeks and moves the sentence loop with the user.
func (mw *mainWindow) seekToSentence(start float64) {
	mw.seekTo(start)
	if mw.loopSegment >= 0 {
		segs, _, _ := mw.sentence()
		for i, s := range segs {
			if s.Start == start {
				mw.loopSegment = i
			}
		}
	}
}

// toggleSentenceLoop repeats the current sentence until turned off.
func (mw *mainWindow) toggleSentenceLoop() {
	if mw.loopSegment >= 0 {
		mw.loopSegment = -1
		mw.app.InvalidateAll()
		return
	}
	segs, idx, _ := mw.sentence()
	if len(segs) == 0 {
		return
	}
	mw.loopSegment = max(idx, 0)
	mw.playerPlay()
	mw.app.InvalidateAll()
}

// keepSentenceLoop sends playback back to the looped sentence's start as
// it reaches its end. A jump well outside it, as a seek from the
// player's own controls, moves the loop to the sentence played there.
func (mw *mainWindow) keepSentenceLoop(seconds float64) {
	if mw.loopSegment < 0 {
		return
	}
	segs, idx, _ := mw.sentence()
	if mw.loopSegment >= len(segs) {
		mw.loopSegment = -1
		return
	}
	seg := segs[mw.loopSegment]
	end := seg.End
	if mw.loopSegment+1 < len(segs) && segs[mw.loopSegment+1].Start < end {
		end = segs[mw.loopSegment+1].Start
	}
	switch {
	case seconds < seg.Start-1 || seconds > end+2:
		mw.loopSegment = max(idx, 0)
	case seconds >= end-0.05:
		mw.seekTo(seg.Start)
	}
}
