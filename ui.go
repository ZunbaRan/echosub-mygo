package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Layout of the main window: a video library on the left, the video with
// its listening stage and transport in the middle, and the subtitle panel
// on the right, sized for sentence-by-sentence practice. The library
// leaves the row first as the window shrinks and opens as a floating sheet
// instead; narrower still, the subtitle panel moves under the video.
const (
	pad          = 10
	gap          = 10
	transportH   = 80
	focusMinH    = 112
	focusCtxH    = 150
	dividerW     = 10
	narrowWidth  = 900
	libMin       = 230
	libMax       = 360
	subMin       = 300
	subMax       = 460
	compactWidth = 560
)

// mainWindow is the main EchoSub window.
type mainWindow struct {
	app    *AppState
	win    *mygo.Window
	player *mygo.WebView
	bridge *PlayerBridge
	tray   *mygo.Tray

	traySig string
	lastW   float32

	urlInput  string
	heroInput string
	addErr    string

	libW           float64
	subW           float64
	libFloat       bool
	libList        ui.ListState
	subList        ui.ListState
	lastFollowIdx  int
	lastAutoScroll time.Time
	followPaused   bool

	search     string
	filter     string
	showHidden bool

	speed       float64
	loopSegment int
	full        bool
	compact     bool
	typing      bool

	deleteID   string
	askDelete  bool
	askClear   bool
	askRefetch bool

	showSettings bool
	set          settingsForm

	loadedVideoID string

	floatWin     *mygo.Window
	floatLastIdx int
	floatList    ui.ListState
	lyricsWin    *mygo.Window
	lyricsLocked bool
}

func newMainWindow(app *AppState) *mainWindow {
	st := app.Settings()
	mw := &mainWindow{
		app:           app,
		libW:          st.LibraryWidth,
		subW:          st.SubPaneWidth,
		lastFollowIdx: -1,
		floatLastIdx:  -1,
		speed:         1,
		loopSegment:   -1,
	}
	mw.bridge = &PlayerBridge{app: app, mw: mw, lastSegment: -1}
	return mw
}

// show creates the main window and the player webview.
func (mw *mainWindow) show() {
	mw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "EchoSub",
		Width:         1280,
		Height:        800,
		MinWidth:      720,
		MinHeight:     520,
		StateKey:      "main",
		Vibrancy:      mygo.VibrancyContent,
		TitleBarStyle: mygo.TitleBarHiddenInset,
		Content:       ui.View(mw.view),
	})
	mw.app.mainWin = mw.win
	mw.win.OnClosed(func() {
		if mw.floatWin != nil {
			mw.floatWin.Close()
		}
		if mw.lyricsWin != nil {
			mw.lyricsWin.Close()
		}
	})

	player, err := mw.win.NewWebView(mygo.WebViewOptions{
		BackgroundColor: "#000000",
		// The player page loads under the app-id origin YouTube embeds
		// need; allow it to call the bound bridge.
		Page: mygo.PageOptions{TrustedOrigins: []string{"https://com.zunbaran.echosub"}},
	})
	if err != nil {
		log.Printf("echosub: cannot create player webview: %v", err)
	} else {
		mw.player = player
		snap := mw.app.snapshot()
		if v := snap.currentVideo(); v != nil {
			mw.loadVideo(v.ID, v.Progress)
		}
	}
}

func (mw *mainWindow) view(c *ui.Context) {
	snap := mw.app.snapshot()
	t := c.Theme()
	W, H := c.Size()
	mw.lastW = W
	mw.typing = false
	var backdrop ui.ImageSource
	if v := snap.currentVideo(); v != nil && v.ThumbnailURL != "" {
		backdrop = mw.app.Thumbnail(v.ThumbnailURL)
	}
	if c.Vibrancy() && backdrop == nil {
		c.Root().Background(t.Background.Alpha(float32(snap.settings.MainWindowBackgroundOpacity)))
	} else {
		c.Root().Background(t.Background)
	}
	if snap.currentID != "" && snap.currentID != mw.loadedVideoID && mw.player != nil {
		mw.loadedVideoID = snap.currentID
		mw.loopSegment = -1
		mw.loadVideo(snap.currentID, snap.playback)
	}
	if mw.full && (mw.player == nil || snap.currentVideo() == nil) {
		mw.full = false
	}

	ui.Box(c).Fill().Children(func() {
		// An ambient backdrop from the video's artwork, blurred, gives the
		// glass something to refract and fills the letterbox around the
		// video, as Music and TV do.
		if backdrop != nil {
			ui.Image(c, backdrop).Absolute().Top(-120).Left(-120).Right(-120).Bottom(-120).
				Fit(ui.Cover).Blur(80).Opacity(0.9)
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(t.Background.Alpha(0.6))
		}
		if mw.full {
			mw.fullView(c, W, H)
		} else {
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
				barH := mw.topBar(c, &snap, t, W)
				mw.body(c, &snap, t, W, H-barH)
			})
			mw.floatingLibrary(c, &snap, t, W, H)
		}
	})

	ui.Modal(c, &mw.showSettings, func() { mw.settingsModal(c, t) })
	mw.dialogs(c, &snap)
	mw.shortcuts(c)
	mw.syncTray(&snap)
}

// libraryFits reports whether the library pane can join the row at this
// width; narrower, it opens as a floating sheet instead.
func (mw *mainWindow) libraryFits(W float32) bool {
	subW := float32(clampF(mw.subW, subMin, subMax))
	if mw.app.Settings().SubPaneHidden || W < narrowWidth {
		subW = -dividerW
	}
	return W-2*pad-subW-dividerW-float32(clampF(mw.libW, libMin, libMax))-dividerW >= compactWidth
}

// libraryInline reports whether the library pane sits in the row.
func (mw *mainWindow) libraryInline(W float32) bool {
	return !mw.app.Settings().LibraryHidden && mw.libraryFits(W)
}

// topBar is the toolbar in the title bar: the library toggle, the
// current video's title, and the window toggles. It returns its height.
func (mw *mainWindow) topBar(c *ui.Context, snap *uiSnapshot, t *ui.Theme, W float32) float32 {
	tb := c.TitleBar()
	h := max(tb.Height, 54)
	labels := W >= 760
	ui.Row(c).Height(h).Shrink(0).Padding(0, 12+tb.Right, 0, 12+tb.Left).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		if iconButton(c, icPanelL, "视频库 ⌃⌘S", mw.libraryInline(W) || mw.libFloat).Clicked() {
			mw.toggleLibrary()
		}
		ui.Column(c).Gap(1).MinWidth(0).Children(func() {
			if v := snap.currentVideo(); v != nil {
				ui.Text(c, v.Title).SingleLine().Ellipsis("…").FontSize(13).Bold()
				meta := v.Channel
				if meta == "" {
					meta = "频道信息获取中…"
				}
				if v.Duration != nil {
					meta += " · " + formatDuration(*v.Duration)
				}
				ui.Text(c, meta).SingleLine().Ellipsis("…").FontSize(11).TextColor(t.TextMuted)
			} else {
				ui.Text(c, "EchoSub").FontSize(13).Bold()
				ui.Text(c, "YouTube 双语字幕精听").FontSize(11).TextColor(t.TextMuted)
			}
		})
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(2).Padding(3).Radius(17).Shrink(0).Material(paneGlass(t, 0)).Children(func() {
			if tbButton(c, icCaptions, "悬浮字幕", "悬浮字幕 ⇧⌘F", mw.floatWin != nil, labels).Clicked() {
				mw.toggleFloatWindow()
			}
			if tbButton(c, icLyrics, "桌面歌词", "桌面歌词 ⇧⌘D", mw.lyricsWin != nil, labels).Clicked() {
				mw.toggleLyricsWindow()
			}
			lockIcon, lockLabel := icUnlock, "锁定桌面歌词 ⇧⌘L"
			if mw.lyricsLocked {
				lockIcon, lockLabel = icLock, "解锁桌面歌词 ⇧⌘L"
			}
			if tbButton(c, lockIcon, "", lockLabel, mw.lyricsLocked, labels).Disabled(mw.lyricsWin == nil).Clicked() {
				mw.setLyricsLocked(!mw.lyricsLocked)
			}
		})
		ui.Row(c).Gap(2).Padding(3).Radius(17).Shrink(0).Material(paneGlass(t, 0)).Children(func() {
			if iconButton(c, icPanelR, "字幕面板 ⌃⌘T", !snap.settings.SubPaneHidden).Clicked() {
				mw.toggleSubPane()
			}
			if iconButton(c, icSettings, "设置 ⌘,", false).Clicked() {
				mw.openSettings()
			}
		})
	})
	return h
}

// tbButton is a toolbar button with an icon and an optional text label;
// on narrow windows only the icon stays.
func tbButton(c *ui.Context, ic *ui.SVG, text, label string, active, showText bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Height(30).Padding(0, 8).Gap(5).Radius(15)
	if !showText {
		b.Size(30, 30).Padding(0, 0)
	}
	fg := t.Text.Alpha(0.85)
	switch {
	case active:
		b.Background(t.Accent.Alpha(0.18))
		fg = t.Accent
	case b.Pressed():
		b.Background(t.Text.Alpha(0.14))
	case b.Hovered():
		b.Background(t.Text.Alpha(0.08))
	}
	b.Children(func() {
		ui.Icon(c, ic).FontSize(15).TextColor(fg)
		if text != "" && showText {
			ui.Text(c, text).FontSize(12).TextColor(fg).SingleLine()
		}
	})
	return b
}

// body lays out the three panes in the room under the toolbar.
func (mw *mainWindow) body(c *ui.Context, snap *uiSnapshot, t *ui.Theme, W, H float32) {
	s := snap.settings
	subOpen := !s.SubPaneHidden
	hasVideo := snap.currentVideo() != nil
	if W < narrowWidth {
		vw := W - 2*pad
		mw.compact = vw < compactWidth
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, pad, pad, pad).Gap(gap).Children(func() {
			if !hasVideo {
				mw.videoArea(c, snap, t, vw, H-pad)
				return
			}
			subMinPane, gaps := float32(0), float32(gap*2)
			if subOpen {
				subMinPane, gaps = 180, gap*3
			}
			vh := min(vw*9/16, H-pad-gaps-transportH-focusMinH-subMinPane)
			vh = max(vh, 120)
			mw.videoArea(c, snap, t, vw, vh)
			mw.focusStage(c, snap, t, H-pad-gaps-transportH-vh-subMinPane > focusCtxH)
			mw.transport(c, snap, t, vw)
			if subOpen {
				mw.subtitlePane(c, snap, t, 0)
			}
		})
		return
	}
	subW := float32(clampF(mw.subW, subMin, subMax))
	libW := float32(clampF(mw.libW, libMin, libMax))
	libOpen := mw.libraryInline(W)
	centerW := W - 2*pad
	if libOpen {
		centerW -= libW + dividerW
	}
	if subOpen {
		centerW -= subW + dividerW
	}
	mw.compact = centerW < compactWidth
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(0, pad, pad, pad).Children(func() {
		if libOpen {
			mw.libraryPane(c, snap, t, libW)
			mw.divider(c, t, &mw.libW, libMin, libMax, false, func(s *Settings, w float64) { s.LibraryWidth = w })
		}
		ui.Column(c).Width(centerW).Shrink(0).Gap(gap).Children(func() {
			if !hasVideo {
				mw.videoArea(c, snap, t, centerW, H-pad)
				return
			}
			vh := min(centerW*9/16, H-pad-gap*2-transportH-focusMinH)
			mw.videoArea(c, snap, t, centerW, vh)
			mw.focusStage(c, snap, t, H-pad-gap*2-transportH-vh > focusCtxH)
			mw.transport(c, snap, t, centerW)
		})
		if subOpen {
			mw.divider(c, t, &mw.subW, subMin, subMax, true, func(s *Settings, w float64) { s.SubPaneWidth = w })
			mw.subtitlePane(c, snap, t, subW)
		}
	})
}

// divider is the drag handle between two panes; right says the sized pane
// is on the right, so drags run the other way.
func (mw *mainWindow) divider(c *ui.Context, t *ui.Theme, width *float64, lo, hi float64, right bool, persist func(*Settings, float64)) {
	div := ui.Box(c).Width(dividerW).Shrink(0).Cursor(ui.CursorResizeEW).Center().Label("调整面板宽度")
	if dx, _, ok := div.Dragged(); ok && dx != 0 {
		d := float64(dx)
		if right {
			d = -d
		}
		*width = clampF(*width+d, lo, hi)
	}
	if _, _, ended := div.DragEnded(); ended {
		w := *width
		mw.app.UpdateSettings(func(s *Settings) { persist(s, w) })
	}
	div.Children(func() {
		if div.Hovered() {
			ui.Box(c).Width(2).HeightPercent(70).Radius(1).Background(t.Accent.Alpha(0.8))
		}
	})
}

// floatingLibrary is the library as a floating glass sheet, for windows
// too narrow for three panes.
func (mw *mainWindow) floatingLibrary(c *ui.Context, snap *uiSnapshot, t *ui.Theme, W, H float32) {
	if mw.libFloat && mw.libraryInline(W) {
		mw.libFloat = false
	}
	if !mw.libFloat {
		return
	}
	scrim := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(ui.RGBA(0, 0, 0, 0.25))
	if scrim.Clicked() {
		mw.libFloat = false
	}
	ui.Column(c).Absolute().Top(pad+4).Left(pad).Bottom(pad).Width(min(300, W-2*pad)).Radius(20).
		Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.4)).Children(func() {
		mw.libraryPane(c, snap, t, 0)
	})
}

// libraryPane is the video library: link field, filter and the list.
// A width of 0 lets it grow to the floating sheet.
func (mw *mainWindow) libraryPane(c *ui.Context, snap *uiSnapshot, t *ui.Theme, width float32) {
	pane := ui.Column(c).MinWidth(0).Radius(20).Material(paneGlass(t, 0.7))
	if width > 0 {
		pane.Width(width).Shrink(0)
	} else {
		pane.Grow(1)
	}
	pane.Children(func() {
		ui.Column(c).Padding(12, 12, 8, 12).Gap(8).Shrink(0).Children(func() {
			ui.Row(c).Height(34).Padding(0, 4, 0, 10).Gap(6).Radius(17).AlignItems(ui.Center).
				Background(t.Text.Alpha(0.06)).Children(func() {
				ui.Icon(c, icLink).FontSize(13).TextColor(t.TextMuted)
				input := ui.TextInput(c, &mw.urlInput).Placeholder("粘贴 YouTube 链接或视频 ID").
					Grow(1).Label("YouTube 链接").Background(ui.Transparent).Border(0, ui.Transparent).FontSize(12)
				mw.typing = mw.typing || input.Focused()
				if input.Changed() {
					mw.addErr = ""
				}
				if iconButton(c, icClipboard, "从剪贴板粘贴", false).Clicked() {
					mw.urlInput = mygo.Clipboard.ReadText()
					mw.addErr = ""
				}
				add := ui.ButtonBase(c).Label("添加视频").Tooltip("添加（回车）").Size(26, 26).Radius(13).Background(t.Accent)
				add.Children(func() { ui.Icon(c, icPlus).FontSize(14).TextColor(ui.Hex("#FFFFFF")) })
				if add.Clicked() || input.Submitted() {
					mw.addFrom(mw.urlInput)
					if mw.addErr == "" {
						mw.urlInput = ""
					}
				}
			})
			if mw.addErr != "" {
				ui.Text(c, mw.addErr).FontSize(11).TextColor(t.Danger).SingleLine().Ellipsis("…")
			} else if id := videoID(mw.urlInput); id != "" {
				hint := "✓ " + id
				for _, v := range snap.videos {
					if v.ID == id {
						hint = "✓ 已在列表中"
					}
				}
				for _, v := range snap.hidden {
					if v.ID == id {
						hint = "✓ 已隐藏，添加后会恢复"
					}
				}
				ui.Text(c, hint).FontSize(11).TextColor(t.Success).SingleLine().Shrink(0)
			}
			ui.Row(c).Height(28).Padding(0, 10).Gap(6).Radius(14).AlignItems(ui.Center).
				Background(t.Text.Alpha(0.05)).Children(func() {
				ui.Icon(c, icSearch).FontSize(12).TextColor(t.TextMuted)
				filter := ui.TextInput(c, &mw.filter).Placeholder("筛选标题或频道").
					Grow(1).Label("筛选").Background(ui.Transparent).Border(0, ui.Transparent).FontSize(11.5)
				mw.typing = mw.typing || filter.Focused()
			})
		})
		ui.Row(c).Padding(2, 10, 4, 14).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, "视频库").FontSize(12).Bold().TextColor(t.TextMuted)
			ui.Box(c).Grow(1)
			ui.Text(c, fmt.Sprint(len(snap.videos))).FontSize(10.5).Bold().TextColor(t.TextMuted).
				Padding(1, 7).Radius(8).Background(t.Text.Alpha(0.08))
		})
		mw.libraryList(c, snap, t)
		ui.Row(c).Padding(6, 8, 10, 12).Gap(6).AlignItems(ui.Center).Shrink(0).
			BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
			label := fmt.Sprintf("已隐藏 %d", len(snap.hidden))
			if mw.showHidden {
				label = "返回视频库"
			}
			if pillButton(c, icEyeOff, label, "已隐藏的视频", mw.showHidden).Clicked() {
				mw.showHidden = !mw.showHidden
			}
			ui.Box(c).Grow(1)
			if pillButton(c, icTrash, "清空", "清空视频库", false).Disabled(len(snap.videos) == 0).Clicked() {
				mw.askClear = true
			}
		})
	})
}

func (mw *mainWindow) libraryList(c *ui.Context, snap *uiSnapshot, t *ui.Theme) {
	videos := snap.videos
	if mw.showHidden {
		videos = snap.hidden
	}
	q := strings.ToLower(strings.TrimSpace(mw.filter))
	var shown []VideoItem
	for _, v := range videos {
		if q == "" || strings.Contains(strings.ToLower(v.Title+" "+v.Channel), q) {
			shown = append(shown, v)
		}
	}
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		switch {
		case len(videos) == 0 && mw.showHidden:
			stateMessage(c, t, icEyeOff, "没有隐藏的视频", "隐藏只是收起，不会删除字幕和翻译。", t.Text, nil)
		case len(videos) == 0:
			stateMessage(c, t, icVideo, "还没有视频", "在上方粘贴 YouTube 链接", t.Text, nil)
		case len(shown) == 0:
			stateMessage(c, t, icSearch, "没有匹配的视频", "换个关键词试试", t.TextMuted, nil)
		default:
			ui.List(c, &mw.libList, len(shown), func(i int) {
				mw.libraryRow(c, shown[i], shown[i].ID == snap.currentID && !mw.showHidden, snap, t)
			}).Grow(1).Padding(0, 6, 6, 6)
		}
	})
}

// addFrom validates and adds a link, keeping the error next to the field.
func (mw *mainWindow) addFrom(text string) {
	if err := mw.app.AddVideo(text); err != nil {
		mw.addErr = err.Error()
	}
}

func (mw *mainWindow) libraryRow(c *ui.Context, v VideoItem, current bool, snap *uiSnapshot, t *ui.Theme) {
	hidden := mw.showHidden
	menu := func(m *ui.Menu) {
		if hidden {
			if m.Item("恢复到视频库").Chosen() {
				mw.app.UnhideVideo(v.ID)
			}
		} else {
			label := "播放"
			if current {
				label = "从头播放"
			}
			if m.Item(label).Chosen() {
				mw.app.PlayVideoFromBeginning(v.ID)
				mw.playerSeek(0)
				mw.playerPlay()
			}
			if m.Item("在 YouTube 打开").Chosen() {
				_ = mygo.Shell.OpenExternal(v.URL)
			}
			if m.Item("复制链接").Chosen() {
				mygo.Clipboard.WriteText(v.URL)
			}
			if m.Item("重新获取字幕…").Chosen() {
				mw.deleteID = v.ID
				mw.askRefetch = true
			}
			m.Separator()
			if m.Item("隐藏").Chosen() {
				mw.app.HideVideo(v.ID)
			}
		}
		if m.Item("删除…").Chosen() {
			mw.deleteID = v.ID
			mw.askDelete = true
		}
	}
	row := ui.Row(c).Padding(7).Gap(10).Radius(12).AlignItems(ui.Start)
	hovered := row.Hovered()
	if current {
		row.Background(t.Accent.Alpha(0.16)).BorderWidth(1).BorderColor(t.Accent.Alpha(0.45))
	} else if hovered {
		row.Background(t.Text.Alpha(0.06))
	}
	row.ContextMenu(menu)
	if row.Clicked() && !current && !hidden {
		mw.app.SelectVideo(v.ID)
	}
	row.Children(func() {
		ui.Box(c).Width(86).AspectRatio(16.0 / 9.0).Radius(8).Clip().Shrink(0).Background(t.Text.Alpha(0.08)).Center().Children(func() {
			if img := mw.app.Thumbnail(v.ThumbnailURL); img != nil {
				ui.Image(c, img).Fill().Fit(ui.Cover)
			} else {
				ui.Icon(c, icVideo).FontSize(18).TextColor(t.TextMuted)
			}
			if v.Duration != nil && *v.Duration > 0 {
				ui.Text(c, formatDuration(*v.Duration)).FontSize(9.5).Bold().TextColor(ui.Hex("#FFFFFF")).
					Padding(0, 4).Radius(4).Background(ui.RGBA(0, 0, 0, 0.66)).Absolute().Right(3).Bottom(4)
				if v.Progress > 0 {
					ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(3).Background(ui.RGBA(255, 255, 255, 0.25)).Children(func() {
						ui.Box(c).FillHeight().WidthPercent(float32(min(v.Progress / *v.Duration, 1) * 100)).Background(ui.Hex("#FF2A3C"))
					})
				}
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			title := v.Title
			if title == "" {
				title = "YouTube 视频"
			}
			titleEl := ui.Text(c, title).MaxLines(2).Ellipsis("…").FontSize(12).LineHeight(1.3)
			if v.Title == "" {
				titleEl.TextColor(t.TextMuted)
			} else if current {
				titleEl.FontWeight(600)
			}
			channel := v.Channel
			if channel == "" {
				if v.Status == StatusLoading {
					channel = "获取中…"
				} else {
					channel = "—"
				}
			}
			ui.Text(c, channel).SingleLine().Ellipsis("…").FontSize(11).TextColor(t.TextMuted)
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				label := v.Status.Label()
				if doc := snap.transcripts[v.ID]; v.Status == StatusTranslating && doc != nil && len(doc.Segments) > 0 {
					label += fmt.Sprintf(" %d%%", translatedCount(doc)*100/len(doc.Segments))
				}
				ui.Text(c, label).SingleLine().FontSize(10).Bold().TextColor(statusColor(t, v.Status)).
					Padding(1, 6).Radius(7).Background(statusColor(t, v.Status).Alpha(0.14)).Shrink(0)
			})
		})
		if hovered {
			iconButton(c, icMore, "更多操作", false).Shrink(0).PopoverMenuButton(menu)
		}
	})
}

func statusColor(t *ui.Theme, s TranscriptStatus) ui.Color {
	switch s {
	case StatusReady:
		return t.Success
	case StatusTranslating, StatusLoading:
		return t.Accent
	case StatusFailed:
		return t.Danger
	case StatusNoSubtitles:
		return t.Warning
	default:
		return t.TextMuted
	}
}

// fitVideo returns the largest 16:9 size inside w by h.
func fitVideo(w, h float32) (float32, float32) {
	if w*9/16 <= h {
		return w, w * 9 / 16
	}
	return h * 16 / 9, h
}

// videoArea shows the player letterboxed in w by h, or the empty state.
func (mw *mainWindow) videoArea(c *ui.Context, snap *uiSnapshot, t *ui.Theme, w, h float32) {
	ui.Box(c).Width(w).Height(max(h, 120)).Shrink(0).Center().Children(func() {
		if snap.currentVideo() == nil {
			mw.emptyPlayer(c, t)
			return
		}
		pw, ph := fitVideo(w, max(h, 120))
		if mw.player != nil {
			ui.WebView(c, mw.player).Size(pw, ph).AlignSelf(ui.Center).Radius(16).
				Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.35))
		} else {
			ui.Box(c).Size(pw, ph).Radius(16).Background(ui.Hex("#000000")).Center().Children(func() {
				ui.Text(c, "播放器不可用").TextColor(ui.Hex("#8E8E93"))
			})
		}
	})
}

func (mw *mainWindow) emptyPlayer(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Radius(22).Center().Gap(12).Padding(24).
		Material(paneGlass(t, 0.6)).Children(func() {
		ui.Box(c).Size(72, 72).Radius(22).Center().Gradient(ui.Hex("#3FA2FF"), ui.Hex("#0A60FF"), 140).Children(func() {
			ui.Icon(c, icCaptions).FontSize(32).TextColor(ui.Hex("#FFFFFF"))
		})
		ui.Text(c, "把 YouTube 视频变成双语精听材料").FontSize(20).Bold().TextAlign(ui.Center)
		ui.Text(c, "从左侧视频库里选一个，或粘贴新的 YouTube 链接").TextColor(t.TextMuted).FontSize(13).TextAlign(ui.Center)
		ui.Row(c).Height(40).Width(420).Padding(0, 4, 0, 14).Gap(8).Radius(20).AlignItems(ui.Center).
			Background(t.Text.Alpha(0.07)).Children(func() {
			ui.Icon(c, icLink).FontSize(14).TextColor(t.TextMuted)
			input := ui.TextInput(c, &mw.heroInput).Placeholder("粘贴 YouTube 链接，回车添加").
				Grow(1).Label("YouTube 链接").Background(ui.Transparent).Border(0, ui.Transparent)
			mw.typing = mw.typing || input.Focused()
			if iconButton(c, icClipboard, "从剪贴板粘贴", false).Clicked() {
				mw.heroInput = mygo.Clipboard.ReadText()
			}
			add := ui.ButtonBase(c).Label("添加视频").Height(30).Padding(0, 12).Gap(4).Radius(15).Background(t.Accent)
			add.Children(func() {
				ui.Icon(c, icPlus).FontSize(14).TextColor(ui.Hex("#FFFFFF"))
				ui.Text(c, "添加").FontSize(12).Bold().TextColor(ui.Hex("#FFFFFF"))
			})
			if add.Clicked() || input.Submitted() {
				text := mw.heroInput
				if err := mw.app.AddVideo(text); err != nil {
					c.Toast(err.Error())
				} else {
					mw.heroInput = ""
				}
			}
		})
	})
}

// focusStage is the listening area between the video and the transport:
// the current sentence large, its neighbours dim, and how far it got. It
// is transparent so the ambient backdrop shows through, and it grows to
// fill whatever the video leaves; context lines hide when it runs short.
func (mw *mainWindow) focusStage(c *ui.Context, snap *uiSnapshot, t *ui.Theme, showCtx bool) {
	video := snap.currentVideo()
	if video == nil {
		return
	}
	doc := snap.currentTranscript()
	idx := snap.currentSegmentIndex()
	area := ui.Column(c).Grow(1).MinHeight(0).FillWidth().Clip().Padding(2, 8, 2, 8).Justify(ui.Center)
	area.Children(func() {
		ui.Column(c).FillWidth().MaxWidth(980).AlignSelf(ui.Center).Gap(14).Children(func() {
			mw.focusStageContent(c, snap, t, video, doc, idx, showCtx)
		})
	})
}

func (mw *mainWindow) focusStageContent(c *ui.Context, snap *uiSnapshot, t *ui.Theme, video *VideoItem, doc *TranscriptDocument, idx int, showCtx bool) {
	if doc == nil || len(doc.Segments) == 0 {
		msg := "正在获取字幕，视频可以先播放"
		switch video.Status {
		case StatusFailed:
			msg = "字幕获取失败"
		case StatusNoSubtitles:
			msg = "这个视频没有可用字幕"
		}
		ui.Row(c).Center().Gap(8).Children(func() {
			ui.Icon(c, icCaptions).FontSize(15).TextColor(t.TextMuted)
			ui.Text(c, msg).FontSize(13).TextColor(t.TextMuted)
		})
		return
	}
	segs := doc.Segments
	cur := max(idx, 0)
	ctxLine := func(i int) {
		if !showCtx {
			return
		}
		if i < 0 || i >= len(segs) {
			ui.Box(c).Height(17)
			return
		}
		s := segs[i]
		text := s.Original
		if snap.settings.SubtitleMode == DisplayTranslated && s.EffectiveTranslation() != "" {
			text = s.EffectiveTranslation()
		}
		l := ui.Text(c, text).FontSize(13).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MaxWidth(760)
		if l.Clicked() {
			mw.seekToSentence(s.Start)
			mw.playerPlay()
		}
	}
	ctxLine(cur - 1)
	ui.Column(c).MinHeight(0).Gap(0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(0, 0, 6, 0).Children(func() {
			ui.Text(c, fmt.Sprintf("第 %d / %d 句", cur+1, len(segs))).FontSize(11.5).TextColor(t.TextMuted)
			if mw.loopSegment >= 0 {
				ui.Row(c).Gap(4).Padding(1, 7).Radius(8).AlignItems(ui.Center).Background(t.Accent.Alpha(0.16)).Children(func() {
					ui.Icon(c, icLoopOne).FontSize(11).TextColor(t.Accent)
					ui.Text(c, "单句循环中").FontSize(10.5).Bold().TextColor(t.Accent)
				})
			}
			if idx < 0 {
				ui.Text(c, "即将开始").FontSize(10.5).Bold().TextColor(t.TextMuted).
					Padding(1, 7).Radius(8).Background(t.Text.Alpha(0.08))
			}
		})
		seg := segs[cur]
		s := snap.settings
		translation := seg.EffectiveTranslation()
		dim := t.Text
		if idx < 0 {
			dim = t.Text.Alpha(0.55)
		}
		if s.SubtitleMode != DisplayTranslated || translation == "" {
			ui.Text(c, seg.Original).FontSize(18).FontWeight(600).TextColor(dim).MaxLines(3).Ellipsis("…").LineHeight(1.36)
		}
		if s.SubtitleMode != DisplayOriginal {
			switch {
			case translation != "" && s.SubtitleMode == DisplayTranslated:
				ui.Text(c, translation).FontSize(18).FontWeight(600).TextColor(dim).MaxLines(3).Ellipsis("…").LineHeight(1.36)
			case translation != "":
				ui.Text(c, translation).FontSize(15).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…").LineHeight(1.5).Margin(5, 0, 0, 0)
			case video.Status == StatusTranslating:
				ui.Text(c, "翻译中…").FontSize(15).TextColor(t.TextMuted).Margin(5, 0, 0, 0)
			}
		}
		p := doc.SegmentProgress(cur, snap.playback)
		ui.Box(c).FillWidth().Height(2).Radius(1).Margin(10, 0, 0, 0).Background(t.Text.Alpha(0.14)).Children(func() {
			ui.Box(c).FillHeight().WidthPercent(p * 100).Radius(1).Background(t.Accent)
		})
	})
	next := cur + 1
	if idx < 0 && len(segs) > 1 {
		next = 1
	}
	ctxLine(next)
}

// transport is the glass card under the stage: the scrub row with a mark
// per sentence, then the controls.
func (mw *mainWindow) transport(c *ui.Context, snap *uiSnapshot, t *ui.Theme, w float32) {
	video := snap.currentVideo()
	if video == nil {
		return
	}
	doc := snap.currentTranscript()
	card := ui.Column(c).Height(transportH).Shrink(0).FillWidth().MaxWidth(980).AlignSelf(ui.Center).
		Padding(7, 12, 7, 12).Gap(4).Radius(20).Material(paneGlass(t, 0))
	card.Children(func() {
		dur := 0.0
		if video.Duration != nil {
			dur = *video.Duration
		}
		railW := float64(min(w, 980) - 24 - 44*2 - 20)
		mw.scrubRow(c, snap, t, doc, dur, railW)

		noDoc := doc == nil || len(doc.Segments) == 0
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).Basis(0).AlignItems(ui.Center).Children(func() {
				pos := "暂无字幕"
				if !noDoc {
					pos = fmt.Sprintf("第 %d / %d 句", max(0, snap.currentSegmentIndex())+1, len(doc.Segments))
				}
				ui.Text(c, pos).FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				if iconButton(c, icReplay, "重听本句 R", false).Disabled(noDoc).Clicked() {
					mw.replaySentence()
				}
				if iconButton(c, icPrev, "上一句 ⌥←", false).Disabled(noDoc).Clicked() {
					mw.prevSentence()
				}
				if !mw.compact && iconButton(c, icBack, "后退 5 秒 ←", false).Clicked() {
					mw.playerSkip(-5)
				}
				playIcon := icPlay
				if snap.playerState == 1 {
					playIcon = icPause
				}
				play := ui.ButtonBase(c).Label("播放/暂停").Tooltip("播放 / 暂停 空格").Size(40, 40).Radius(20).
					Material(glass.Glass{Tint: t.Accent, Interactive: true})
				play.Children(func() { ui.Icon(c, playIcon).FontSize(17).TextColor(ui.Hex("#FFFFFF")) })
				if play.Clicked() {
					mw.playerToggle()
				}
				if !mw.compact && iconButton(c, icForward, "前进 5 秒 →", false).Clicked() {
					mw.playerSkip(5)
				}
				if iconButton(c, icNext, "下一句 ⌥→", false).Disabled(noDoc).Clicked() {
					mw.nextSentence()
				}
				if iconButton(c, icLoopOne, "单句循环 L", mw.loopSegment >= 0).Disabled(noDoc).Clicked() {
					mw.toggleSentenceLoop()
				}
			})
			ui.Row(c).Grow(1).Basis(0).Gap(2).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				label := fmt.Sprintf("%g×", mw.speed)
				if mw.speed == 1 {
					label = "1.0×"
				}
				if pillButton(c, nil, label, "播放速度（点击切换）", mw.speed != 1).Clicked() {
					mw.cycleSpeed()
				}
				loopIcon, loopLabel := icRepeat, "循环：关"
				switch snap.settings.PlaybackLoopMode {
				case LoopList:
					loopLabel = "循环：列表"
				case LoopSingle:
					loopIcon, loopLabel = icRepeatOne, "循环：单个视频"
				}
				if iconButton(c, loopIcon, loopLabel, snap.settings.PlaybackLoopMode != LoopNone).Clicked() {
					mw.app.UpdateSettings(func(s *Settings) {
						s.PlaybackLoopMode = PlaybackLoopMode((int(s.PlaybackLoopMode) + 1) % 3)
					})
				}
				if iconButton(c, icExpand, "窗口内全屏 F", false).Clicked() {
					mw.full = true
				}
				if !mw.compact && iconButton(c, icExternal, "在 YouTube 打开", false).Clicked() {
					_ = mygo.Shell.OpenExternal(video.URL)
				}
			})
		})
	})
}

// scrubRow is the seek bar: one tick per sentence, the looped sentence's
// range as a band, and a hover readout of where the pointer lands.
func (mw *mainWindow) scrubRow(c *ui.Context, snap *uiSnapshot, t *ui.Theme, doc *TranscriptDocument, dur, railW float64) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, formatTime(snap.playback)).FontSize(11).TextColor(t.TextMuted).Width(44)
		rail := ui.Box(c).Grow(1).Height(20).Center().Label("播放进度")
		if dur <= 0 || railW <= 0 {
			ui.Box(c).Grow(1)
			return
		}
		px, _, over := rail.PointerPosition()
		_, _, dragged := rail.Dragged()
		if (rail.Clicked() || dragged) && over {
			mw.seekTo(float64(px) / railW * dur)
		}
		rail.Children(func() {
			track := ui.Box(c).FillWidth().Height(4).Radius(2).Background(t.Text.Alpha(0.14))
			p := min(max(snap.playback/dur, 0), 1)
			track.Children(func() {
				if doc != nil {
					for _, s := range doc.Segments {
						if s.Start < dur {
							ui.Box(c).Absolute().LeftPercent(float32(s.Start / dur * 100)).Top(1).Width(1).Height(2).
								Background(t.Text.Alpha(0.4))
						}
					}
					if mw.loopSegment >= 0 && mw.loopSegment < len(doc.Segments) {
						s := doc.Segments[mw.loopSegment]
						ui.Box(c).Absolute().LeftPercent(float32(s.Start / dur * 100)).WidthPercent(float32((s.End - s.Start) / dur * 100)).
							Top(0).Bottom(0).Radius(2).Background(t.Accent.Alpha(0.35))
					}
				}
				ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).WidthPercent(float32(p * 100)).Radius(2).Background(t.Accent)
			})
			ui.Box(c).Absolute().LeftPercent(float32(p * 100)).Width(0).Top(0).Bottom(0).Center().Children(func() {
				ui.Box(c).Size(12, 12).Radius(6).Background(ui.Hex("#FFFFFF")).Shadow(0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.5))
			})
			if over {
				hoverT := float64(px) / railW * dur
				label := formatTime(hoverT)
				if doc != nil {
					for i, s := range doc.Segments {
						if hoverT < s.End {
							label += fmt.Sprintf(" · 第 %d 句", i+1)
							break
						}
					}
				}
				ui.Box(c).Absolute().Left(px).Width(0).Top(-22).Center().Children(func() {
					ui.Text(c, label).FontSize(10.5).Bold().TextColor(ui.Hex("#FFFFFF")).SingleLine().
						Padding(2, 7).Radius(6).Background(ui.RGBA(20, 20, 24, 0.9))
				})
			}
		})
		ui.Text(c, formatTime(dur)).FontSize(11).TextColor(t.TextMuted).Width(44).TextAlign(ui.End)
	})
}

// subtitlePane is the right pane: a glass header over the transcript.
// A width of 0 lets it grow, as under the video.
func (mw *mainWindow) subtitlePane(c *ui.Context, snap *uiSnapshot, t *ui.Theme, width float32) {
	pane := ui.Column(c).Radius(20).MinWidth(0).Material(paneGlass(t, 0.7))
	if width > 0 {
		pane.Width(width).Shrink(0)
	} else {
		pane.Grow(1).MinHeight(180)
	}
	pane.Children(func() {
		doc := snap.currentTranscript()
		video := snap.currentVideo()
		follow := snap.settings.AutoFollow && !mw.followPaused
		ui.Column(c).Padding(12, 14, 10, 14).Gap(10).Shrink(0).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "字幕").FontSize(15).Bold()
				mw.statusChip(c, snap, t, video, doc)
				ui.Box(c).Grow(1)
				if pillButton(c, icTarget, "跟随", "自动跟随当前句", follow).Clicked() {
					on := !follow
					mw.followPaused = false
					mw.lastFollowIdx = -1
					mw.app.UpdateSettings(func(s *Settings) { s.AutoFollow = on })
				}
				missing := 0
				if doc != nil {
					missing = len(doc.Segments) - translatedCount(doc)
				}
				iconButton(c, icMore, "更多", false).PopoverMenuButton(func(m *ui.Menu) {
					label := "没有待补全的句子"
					if missing > 0 {
						label = fmt.Sprintf("补全翻译（%d 句）", missing)
					}
					if m.Item(label).Disabled(missing == 0 || !snap.translationKeySet).Chosen() {
						go mw.app.translateMissing(snap.currentID)
					}
					if m.Item("重新获取字幕…").Disabled(video == nil).Chosen() {
						mw.askRefetch = true
					}
					if m.Item("复制全部字幕").Disabled(doc == nil).Chosen() {
						var b strings.Builder
						for _, seg := range doc.Segments {
							b.WriteString(seg.Original + "\n")
							if tr := seg.EffectiveTranslation(); tr != "" {
								b.WriteString(tr + "\n")
							}
							b.WriteString("\n")
						}
						mygo.Clipboard.WriteText(b.String())
						c.Toast("已复制双语字幕")
					}
				})
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				mode := int(snap.settings.SubtitleMode)
				ui.Segmented(c, &mode, "原文", "中文", "双语").Label("字幕模式").Shrink(0).Changed()
				if mode != int(snap.settings.SubtitleMode) {
					m := SubtitleDisplayMode(mode)
					mw.app.UpdateSettings(func(s *Settings) { s.SubtitleMode = m })
				}
				search := ui.TextInput(c, &mw.search).Placeholder("搜索英文或中文").Grow(1).MinWidth(60).Label("搜索字幕")
				mw.typing = mw.typing || search.Focused()
				if q := strings.TrimSpace(mw.search); q != "" && doc != nil {
					n := 0
					for _, seg := range doc.Segments {
						if strings.Contains(strings.ToLower(seg.Original), strings.ToLower(q)) || strings.Contains(strings.ToLower(seg.EffectiveTranslation()), strings.ToLower(q)) {
							n++
						}
					}
					ui.Text(c, fmt.Sprintf("%d 条", n)).FontSize(10.5).TextColor(t.TextMuted).Shrink(0)
				}
			})
			mw.subtitleBanner(c, snap, t, video, doc)
		})
		mw.subtitleList(c, snap, t, doc, video, follow)
		if doc != nil && len(doc.Segments) > 0 && video != nil {
			mw.subtitleFooter(c, snap, t, doc, video)
		}
	})
}

// statusChip is the coloured pill next to "字幕" reporting the current
// video's subtitle state.
func (mw *mainWindow) statusChip(c *ui.Context, snap *uiSnapshot, t *ui.Theme, video *VideoItem, doc *TranscriptDocument) {
	if video == nil {
		return
	}
	color, icon, text := t.TextMuted, icInfo, video.Status.Label()
	n := 0
	if doc != nil {
		n = len(doc.Segments)
	}
	missing := n - translatedCount(doc)
	switch video.Status {
	case StatusTranslating:
		color, icon = t.Accent, icLanguages
		text = fmt.Sprintf("正在翻译 %d / %d", translatedCount(doc), n)
	case StatusLoading:
		color, icon, text = t.Accent, icSource, "获取字幕中"
	case StatusFailed:
		color, icon = t.Danger, icAlert
	case StatusNoSubtitles:
		color, icon = t.TextMuted, icCaptions
	case StatusReady:
		switch {
		case n == 0:
			icon, text = icInfo, "字幕为空"
		case missing == 0:
			color, icon, text = t.Success, icCheckC, "已全部翻译"
		case !snap.translationKeySet || missing == n:
			color, icon, text = t.TextMuted, icInfo, "仅原文"
		default:
			color, icon, text = t.Warning, icAlert, fmt.Sprintf("待补全 %d 句", missing)
		}
	default:
		icon = icInfo
	}
	ui.Row(c).Gap(4).Padding(2, 8).Radius(9).AlignItems(ui.Center).Background(color.Alpha(0.14)).Children(func() {
		ui.Icon(c, icon).FontSize(11).TextColor(color)
		ui.Text(c, text).FontSize(10.5).Bold().TextColor(color).SingleLine()
	})
}

// subtitleBanner renders the translating / no-key / fill-in bars between
// the panel header and the transcript list.
func (mw *mainWindow) subtitleBanner(c *ui.Context, snap *uiSnapshot, t *ui.Theme, video *VideoItem, doc *TranscriptDocument) {
	if video == nil || doc == nil || len(doc.Segments) == 0 {
		return
	}
	done, total := translatedCount(doc), len(doc.Segments)
	missing := total - done
	switch {
	case video.Status == StatusTranslating:
		ui.Column(c).Padding(8, 10, 9, 10).Gap(6).Radius(12).Background(t.Accent.Alpha(0.10)).Children(func() {
			ui.Progress(c, float64(done)/float64(max(total, 1)))
			ui.Text(c, fmt.Sprintf("每批 4 句，译文会一句句出现 · %s", snap.settings.TranslationModel)).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
		})
	case missing > 0 && !snap.translationKeySet:
		ui.Row(c).Padding(8, 8, 8, 10).Gap(8).Radius(12).AlignItems(ui.Center).
			Background(t.Warning.Alpha(0.16)).Children(func() {
			ui.Icon(c, icKey).FontSize(15).TextColor(t.Warning)
			ui.Text(c, "未配置翻译服务，当前只显示英文原文。").FontSize(12).Grow(1).LineHeight(1.35)
			if ui.PrimaryButton(c, "去设置").Clicked() {
				mw.set.tab = 0
				mw.openSettings()
			}
		})
	case missing > 0:
		ui.Row(c).Padding(8, 8, 8, 10).Gap(8).Radius(12).AlignItems(ui.Center).
			Background(t.Warning.Alpha(0.14)).Children(func() {
			ui.Icon(c, icAlert).FontSize(15).TextColor(t.Warning)
			ui.Text(c, fmt.Sprintf("有 %d 句在翻译时被跳过。", missing)).FontSize(12).Grow(1).LineHeight(1.35)
			if ui.Button(c, fmt.Sprintf("补全翻译（%d 句）", missing)).Clicked() {
				go mw.app.translateMissing(snap.currentID)
			}
		})
	}
}

func (mw *mainWindow) subtitleList(c *ui.Context, snap *uiSnapshot, t *ui.Theme, doc *TranscriptDocument, video *VideoItem, follow bool) {
	retry := func() {
		if ui.Button(c, "重试").Clicked() {
			mw.app.RetryTranscript()
		}
		if ui.Button(c, "在 YouTube 打开").Clicked() {
			_ = mygo.Shell.OpenExternal(video.URL)
		}
	}
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		switch {
		case video == nil:
			stateMessage(c, t, icCaptions, "暂无字幕", "添加或选择一个视频后，字幕会显示在这里", t.Text, nil)
			return
		case video.Status == StatusLoading:
			stateMessage(c, t, icRefresh, "正在获取字幕…", "依次尝试 YouTube 直连、本机 yt-dlp、Supadata。视频可以先播放。", t.Text, nil)
			return
		case video.Status == StatusFailed:
			detail := snap.lastError
			if detail == "" {
				detail = "三种来源都没有拿到字幕。"
			}
			stateMessage(c, t, icAlert, "字幕获取失败", detail, t.Danger, retry)
			return
		case video.Status == StatusNoSubtitles:
			stateMessage(c, t, icCaptions, "这个视频没有可用字幕", "视频仍可正常播放。", t.Text, retry)
			return
		case doc == nil || len(doc.Segments) == 0:
			stateMessage(c, t, icCaptions, "字幕为空", "", t.TextMuted, retry)
			return
		}

		segments := doc.Segments
		currentIdx := snap.currentSegmentIndex()
		rows := make([]int, 0, len(segments))
		q := strings.ToLower(strings.TrimSpace(mw.search))
		for i, seg := range segments {
			if q == "" || strings.Contains(strings.ToLower(seg.Original), q) || strings.Contains(strings.ToLower(seg.EffectiveTranslation()), q) {
				rows = append(rows, i)
			}
		}
		if len(rows) == 0 {
			stateMessage(c, t, icSearch, "没有找到「"+mw.search+"」", "", t.TextMuted, nil)
			return
		}
		searching := q != ""
		if follow && !searching && currentIdx >= 0 && currentIdx != mw.lastFollowIdx {
			mw.lastFollowIdx = currentIdx
			mw.lastAutoScroll = time.Now()
			mw.subList.ScrollTo(currentIdx, ui.Center)
		}
		ui.List(c, &mw.subList, len(rows), func(i int) {
			mw.subtitleRow(c, snap, segments, rows[i], rows[i] == currentIdx, rows[i] == mw.loopSegment, snap.settings, t)
		}).Grow(1).Padding(2, 8, 8, 8)

		// A scroll by the user away from the current line pauses following
		// until they come back to it.
		first, last := mw.subList.Visible()
		away := currentIdx >= 0 && !searching && (currentIdx < first || currentIdx > last)
		if follow && away && time.Since(mw.lastAutoScroll) > time.Second {
			mw.followPaused = true
		}
		if mw.followPaused && !searching {
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(12).Center().Children(func() {
				back := ui.ButtonBase(c).Label("回到当前句").Height(32).Padding(0, 14).Gap(6).Radius(16).
					Background(t.Accent).Shadow(0, 6, 16, 0, t.Accent.Alpha(0.45))
				back.Children(func() {
					ui.Icon(c, icTarget).FontSize(14).TextColor(ui.Hex("#FFFFFF"))
					ui.Text(c, "回到当前句").FontSize(12).Bold().TextColor(ui.Hex("#FFFFFF"))
				})
				if back.Clicked() {
					mw.followPaused = false
					mw.lastFollowIdx = -1
				}
			})
		}
	})
}

func (mw *mainWindow) subtitleFooter(c *ui.Context, snap *uiSnapshot, t *ui.Theme, doc *TranscriptDocument, video *VideoItem) {
	total, done := len(doc.Segments), translatedCount(doc)
	source := "YouTube"
	if doc.Provider != nil {
		switch *doc.Provider {
		case ProviderYTDLP:
			source = "yt-dlp"
		case ProviderSupadata:
			source = "Supadata"
		}
	}
	kind := "人工字幕"
	if doc.IsGenerated {
		kind = "自动字幕"
	}
	ui.Row(c).Shrink(0).Padding(8, 14, 10, 14).Gap(10).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
		item := func(ic *ui.SVG, text string) {
			if ic != nil {
				ui.Icon(c, ic).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Text(c, text).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
		item(icSource, source)
		ui.Text(c, kind).FontSize(11).TextColor(t.TextMuted).SingleLine()
		item(nil, fmt.Sprintf("%d 句", total))
		if done > 0 {
			ui.Box(c).Grow(1)
			item(icLanguages, snap.settings.TranslationModel)
		}
	})
}

// subtitleRow is a line of the transcript. The main window keeps the
// theme's colors so lines read in light and dark mode; the palette is for
// the overlays, over their dark plates.
func (mw *mainWindow) subtitleRow(c *ui.Context, snap *uiSnapshot, segments []SubtitleSegment, segIdx int, current, looping bool, settings Settings, t *ui.Theme) {
	seg := segments[segIdx]
	row := ui.Row(c).Padding(8, 10, 8, 6).Gap(8).Radius(12).AlignItems(ui.Start)
	hovered := row.Hovered()
	if current {
		row.Background(t.Accent.Alpha(0.10)).BorderWidth(1).BorderColor(t.Accent.Alpha(0.45))
	} else if hovered {
		row.Background(t.Text.Alpha(0.05))
	}
	if row.Clicked() {
		mw.seekToSentence(seg.Start)
		mw.playerPlay()
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("从这里播放").Chosen() {
			mw.seekToSentence(seg.Start)
			mw.playerPlay()
		}
		if m.Item("单句循环这一句").Chosen() {
			mw.seekTo(seg.Start)
			mw.loopSegment = -1
			mw.toggleSentenceLoop()
		}
		if m.Item("复制这一句").Chosen() {
			text := seg.Original
			if tr := seg.EffectiveTranslation(); tr != "" {
				text += "\n" + tr
			}
			mygo.Clipboard.WriteText(text)
		}
	})
	textColor, transColor := t.Text.Alpha(0.75), t.TextMuted
	if current {
		textColor, transColor = t.Text, t.Accent
	}
	row.Children(func() {
		ui.Box(c).Width(36).Shrink(0).Padding(2, 0, 0, 0).Children(func() {
			if hovered {
				ui.Icon(c, icPlay).FontSize(11).TextColor(t.Accent)
			} else {
				ui.Text(c, formatTime(seg.Start)).FontSize(10).TextColor(t.TextMuted)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			translation := seg.EffectiveTranslation()
			if settings.SubtitleMode != DisplayTranslated || translation == "" {
				original := ui.Text(c, seg.Original).FontSize(13.5).TextColor(textColor).LineHeight(1.35)
				if current {
					original.FontWeight(600)
				}
			}
			if settings.SubtitleMode != DisplayOriginal {
				if translation != "" {
					ui.Text(c, translation).FontSize(12.5).TextColor(transColor).LineHeight(1.4)
				} else if snap.currentVideo() != nil && snap.currentVideo().Status == StatusTranslating {
					ui.Text(c, "翻译中…").FontSize(12).TextColor(t.TextMuted)
				}
			}
		})
		if looping {
			ui.Row(c).Gap(3).Padding(1, 6).Radius(7).AlignSelf(ui.End).AlignItems(ui.Center).Shrink(0).
				Background(t.Accent.Alpha(0.16)).Children(func() {
				ui.Icon(c, icLoopOne).FontSize(9).TextColor(t.Accent)
				ui.Text(c, "循环").FontSize(9.5).Bold().TextColor(t.Accent)
			})
		} else if hovered {
			ui.Column(c).Shrink(0).Gap(2).Children(func() {
				if iconButton(c, icLoopOne, "单句循环这一句", false).Clicked() {
					mw.seekTo(seg.Start)
					mw.loopSegment = -1
					mw.toggleSentenceLoop()
				}
				if iconButton(c, icCopy, "复制这一句", false).Clicked() {
					text := seg.Original
					if tr := seg.EffectiveTranslation(); tr != "" {
						text += "\n" + tr
					}
					mygo.Clipboard.WriteText(text)
				}
			})
		}
		if current && seg.End > seg.Start {
			p := float32(0)
			if doc := snap.currentTranscript(); doc != nil {
				p = doc.SegmentProgress(segIdx, snap.playback)
			}
			ui.Box(c).Absolute().Left(10).Right(10).Bottom(0).Height(2).Children(func() {
				ui.Box(c).FillHeight().WidthPercent(p * 100).Radius(1).Background(t.Accent.Alpha(0.8))
			})
		}
	})
}

// stateMessage fills the pane with a message and an optional action.
func stateMessage(c *ui.Context, t *ui.Theme, ic *ui.SVG, title, detail string, color ui.Color, actions func()) {
	ui.Column(c).Grow(1).Center().Gap(10).Padding(24).Children(func() {
		if ic != nil {
			ui.Box(c).Size(48, 48).Radius(24).Center().Background(t.Text.Alpha(0.07)).Children(func() {
				ui.Icon(c, ic).FontSize(22).TextColor(color)
			})
		}
		ui.Text(c, title).FontSize(14).Bold().TextColor(color).TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center).LineHeight(1.45)
		}
		if actions != nil {
			ui.Row(c).Gap(8).Wrap().Justify(ui.Center).Children(actions)
		}
	})
}

// pillButton is a small rounded button with an optional icon and text;
// active tints it with the accent.
func pillButton(c *ui.Context, ic *ui.SVG, text, label string, active bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Height(28).Padding(0, 10).Gap(5).Radius(14)
	fg := t.Text.Alpha(0.75)
	switch {
	case active:
		b.Background(t.Accent.Alpha(0.2))
		fg = t.Accent
	case b.Hovered():
		b.Background(t.Text.Alpha(0.12))
	default:
		b.Background(t.Text.Alpha(0.07))
	}
	b.Children(func() {
		if ic != nil {
			ui.Icon(c, ic).FontSize(14).TextColor(fg)
		}
		if text != "" {
			ui.Text(c, text).FontSize(12).Bold().TextColor(fg).SingleLine()
		}
	})
	return b
}

// fullView is the in-window full screen: the video over black, as large
// as the window lets it, with a button and Escape to leave.
func (mw *mainWindow) fullView(c *ui.Context, W, H float32) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(ui.Hex("#000000")).Center().Children(func() {
		pw, ph := fitVideo(W, H)
		ui.WebView(c, mw.player).Size(pw, ph).AlignSelf(ui.Center)
		ui.Text(c, "窗口内全屏 · 按 Esc 或 F 退出").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.8)).
			Padding(6, 12).Radius(14).Background(ui.RGBA(0, 0, 0, 0.45)).
			Absolute().Top(22).Left(0).Right(0).Margin(0, ui.Auto)
		exit := ui.ButtonBase(c).Label("退出全屏").Tooltip("退出全屏 Esc").Size(48, 48).Radius(24).
			Absolute().Right(22).Bottom(22).Material(glass.Glass{Tint: ui.RGBA(0, 0, 0, 0.3), Interactive: true})
		exit.Children(func() { ui.Icon(c, icShrink).FontSize(20).TextColor(ui.Hex("#FFFFFF")) })
		if exit.Clicked() {
			mw.full = false
		}
	})
}

// dialogs asks before anything that cannot be undone.
func (mw *mainWindow) dialogs(c *ui.Context, snap *uiSnapshot) {
	title := "这个视频"
	for _, v := range append(append([]VideoItem(nil), snap.videos...), snap.hidden...) {
		if v.ID == mw.deleteID {
			title = "「" + v.Title + "」"
		}
	}
	if ui.AlertDialog(c, &mw.askDelete, "删除"+title+"？",
		"将永久删除这个视频的信息、原字幕和中文翻译，无法恢复。只想暂时收起的话，可以用「隐藏」。",
		"取消", "删除") == 1 {
		mw.app.RemoveVideo(mw.deleteID)
	}
	if ui.AlertDialog(c, &mw.askClear, "清空视频库？",
		fmt.Sprintf("将永久删除库里的 %d 个视频，以及它们的字幕和翻译。已隐藏的视频不受影响。", len(snap.videos)),
		"取消", "全部删除") == 1 {
		for _, v := range snap.videos {
			mw.app.RemoveVideo(v.ID)
		}
	}
	if ui.AlertDialog(c, &mw.askRefetch, "重新获取字幕？",
		"会重新下载英文字幕并重新翻译，当前的中文翻译会被替换。", "取消", "重新获取") == 1 {
		mw.app.RetryTranscript()
	}
}

// shortcuts handles the keys of the main window. Keys typing text are left
// to a focused text field; the menu bar has the ⌘ shortcuts of the windows.
func (mw *mainWindow) shortcuts(c *ui.Context) {
	if mw.full && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(0, ui.KeyF)) {
		mw.full = false
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		mw.app.UpdateSettings(func(s *Settings) { s.SubPaneHidden = false })
		mw.app.InvalidateMain()
	}
	if mw.typing || mw.showSettings {
		return
	}
	switch {
	case c.Shortcut(0, ui.KeySpace):
		mw.playerToggle()
	case c.Shortcut(ui.Alt, ui.KeyLeft):
		mw.prevSentence()
	case c.Shortcut(ui.Alt, ui.KeyRight):
		mw.nextSentence()
	case c.Shortcut(0, ui.KeyLeft):
		mw.playerSkip(-5)
	case c.Shortcut(0, ui.KeyRight):
		mw.playerSkip(5)
	case c.Shortcut(0, ui.KeyR):
		mw.replaySentence()
	case c.Shortcut(0, ui.KeyL):
		mw.toggleSentenceLoop()
	case c.Shortcut(0, ui.KeyF):
		mw.full = !mw.full
	}
}

// toggleLibrary hides an open library, or brings it back — inline when
// it fits, as a floating sheet when it does not.
func (mw *mainWindow) toggleLibrary() {
	s := mw.app.Settings()
	switch {
	case mw.libFloat:
		mw.libFloat = false
	case !s.LibraryHidden && mw.libraryInline(mw.lastW):
		mw.app.UpdateSettings(func(s *Settings) { s.LibraryHidden = true })
		return
	case mw.libraryFits(mw.lastW):
		mw.app.UpdateSettings(func(s *Settings) { s.LibraryHidden = false })
		return
	default:
		mw.libFloat = true
	}
	mw.app.InvalidateMain()
}

func (mw *mainWindow) toggleSubPane() {
	mw.app.UpdateSettings(func(s *Settings) { s.SubPaneHidden = !s.SubPaneHidden })
}

func clampF(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

func formatTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatDuration(seconds float64) string { return formatTime(seconds) }

// downloadThumbnail fetches and decodes a playlist thumbnail.
func downloadThumbnail(rawURL string) ui.ImageSource {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil
	}
	img, err := ui.DecodeImage(data)
	if err != nil {
		return nil
	}
	return img
}

// onVideoEnded handles playback reaching the end, honoring loop settings.
func (mw *mainWindow) onVideoEnded() {
	snap := mw.app.snapshot()
	switch snap.settings.PlaybackLoopMode {
	case LoopSingle:
		mw.playerSeek(0)
		mw.playerPlay()
	case LoopList:
		videos := snap.videos
		if len(videos) == 0 {
			return
		}
		idx := -1
		for i, v := range videos {
			if v.ID == snap.currentID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return
		}
		next := videos[(idx+1)%len(videos)]
		if next.ID != snap.currentID {
			mw.app.SelectVideo(next.ID)
		}
	}
}
