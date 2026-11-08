package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// lyricsBarSpace is the transparent room above the lyrics' plate where
// their toolbar shows on hover.
const lyricsBarSpace = 50

// toggleFloatWindow shows or hides the floating subtitle window.
func (mw *mainWindow) toggleFloatWindow() {
	if mw.floatWin != nil {
		mw.floatWin.Close()
		return
	}
	mw.openFloatWindow()
}

// toggleLyricsWindow shows or hides the desktop lyrics window.
func (mw *mainWindow) toggleLyricsWindow() {
	if mw.lyricsWin != nil {
		mw.lyricsWin.Close()
		return
	}
	mw.openLyricsWindow()
}

func (mw *mainWindow) openFloatWindow() {
	st := mw.app.Settings()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:        "悬浮字幕",
		Width:        420,
		Height:       300,
		MinWidth:     260,
		MinHeight:    96,
		Frameless:    true,
		Panel:        true,
		Transparent:  true,
		CornerRadius: 18,
		AlwaysOnTop:  st.FloatPinned,
		StateKey:     "float",
		Content:      ui.View(mw.floatView),
	})
	mw.floatWin = win
	mw.app.floatWin = win
	mw.floatLastIdx = -1
	mw.applyOverlaySettings()
	win.OnClosed(func() {
		mw.floatWin = nil
		mw.app.floatWin = nil
		mw.app.InvalidateMain()
	})
	mw.app.InvalidateMain()
}

// applyOverlaySettings gives the open overlays the window settings: the
// floating window's glass plate, pinning and Spaces.
func (mw *mainWindow) applyOverlaySettings() {
	if mw.floatWin == nil {
		return
	}
	st := mw.app.Settings()
	if st.OverlayPlateEnabled {
		mw.floatWin.SetVibrancy(mygo.VibrancyGlassClear)
	} else {
		mw.floatWin.SetVibrancy(mygo.VibrancyNone)
	}
	mw.floatWin.SetHasShadow(st.OverlayPlateEnabled)
	mw.floatWin.SetAlwaysOnTop(st.FloatPinned)
	mw.floatWin.SetVisibleOnAllWorkspaces(st.ShowsOnAllSpaces)
	mw.floatWin.SetActiveAppearance(true)
	mw.floatWin.Invalidate()
}

// floatDensity picks how many lines the floating window shows for its
// height: the current one, three around it, or a scrolling list.
func floatDensity(h float32) int {
	switch {
	case h < 160:
		return 1
	case h < 300:
		return 3
	default:
		return 0
	}
}

// floatView is the floating subtitle panel. Its controls show only while
// the pointer is over it; otherwise it is the subtitles alone.
func (mw *mainWindow) floatView(c *ui.Context) {
	snap := mw.app.snapshot()
	c.SetTheme(ui.DarkTheme())
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	W, H := c.Size()
	s := snap.settings

	root := ui.Box(c).Fill()
	hovered := root.Hovered()
	if s.OverlayPlateEnabled {
		root.Background(ui.RGBA(0, 0, 0, float32(s.FloatingBackgroundOpacity)*0.55))
	} else if hovered {
		root.Border(1, ui.RGBA(255, 255, 255, 0.35)).BorderStyle(ui.BorderDashed).Radius(18)
	}
	root.Children(func() {
		top := float32(14)
		if hovered {
			top = 46
		}
		doc := snap.currentTranscript()
		idx := snap.currentSegmentIndex()
		body := ui.Column(c).Fill().Padding(top, 16, 14, 16)
		body.Children(func() {
			if doc == nil || len(doc.Segments) == 0 {
				msg := "还没有正在播放的视频"
				if snap.currentVideo() != nil {
					msg = "等待字幕…"
				}
				ui.Column(c).Fill().Center().Children(func() {
					ui.Text(c, msg).TextColor(t.TextMuted)
				})
				return
			}
			cur := max(idx, 0)
			segs := doc.Segments
			switch floatDensity(H) {
			case 1:
				ui.Column(c).Fill().Justify(ui.Center).Children(func() {
					mw.floatLine(c, segs[cur], true, false, s, 3)
				})
			case 3:
				ui.Column(c).Fill().Justify(ui.Center).Gap(8).Children(func() {
					if cur > 0 {
						mw.floatLine(c, segs[cur-1], false, true, s, 1)
					}
					mw.floatLine(c, segs[cur], true, false, s, 0)
					if cur+1 < len(segs) {
						mw.floatLine(c, segs[cur+1], false, true, s, 1)
					}
				})
			default:
				if s.AutoFollow && cur != mw.floatLastIdx {
					mw.floatLastIdx = cur
					mw.floatList.ScrollTo(max(cur-1, 0), ui.Start)
				}
				ui.List(c, &mw.floatList, len(segs), func(i int) {
					mw.floatLine(c, segs[i], i == cur, i != cur, s, 0)
				}).Fill().Gap(4)
			}
		})
		if !hovered {
			return
		}
		mw.floatHeader(c, &snap, W)
		if doc != nil && idx >= 0 {
			ui.Text(c, fmt.Sprintf("%d / %d", idx+1, len(doc.Segments))).FontSize(10.5).TextColor(ui.RGBA(255, 255, 255, 0.5)).
				Absolute().Right(14).Bottom(8)
		}
	})
}

// floatHeader is the floating window's toolbar, shown on hover, which
// also drags the window.
func (mw *mainWindow) floatHeader(c *ui.Context, snap *uiSnapshot, W float32) {
	s := snap.settings
	hasDoc := snap.currentTranscript() != nil
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(42).Padding(0, 6, 0, 12).Gap(1).AlignItems(ui.Center).
		Gradient(ui.RGBA(0, 0, 0, 0.42), ui.RGBA(0, 0, 0, 0), 180).DragWindow().Children(func() {
		ui.Icon(c, icGrip).FontSize(13).TextColor(ui.RGBA(255, 255, 255, 0.55))
		if W >= 300 {
			ui.Text(c, "悬浮字幕").FontSize(11).Bold().TextColor(ui.RGBA(255, 255, 255, 0.6)).Margin(0, 0, 0, 6)
		}
		ui.Box(c).Grow(1)
		if iconButton(c, icPrev, "上一句", false).Disabled(!hasDoc).Clicked() {
			mw.prevSentence()
		}
		playIcon := icPlay
		if snap.playerState == 1 {
			playIcon = icPause
		}
		if iconButton(c, playIcon, "播放 / 暂停", false).Clicked() {
			mw.playerToggle()
		}
		if iconButton(c, icNext, "下一句", false).Disabled(!hasDoc).Clicked() {
			mw.nextSentence()
		}
		if W >= 360 {
			if smallTextButton(c, "A−", "减小字号").Clicked() {
				mw.app.UpdateSettings(func(s *Settings) { s.OverlayFontSize = max(s.OverlayFontSize-2, 12) })
			}
			if smallTextButton(c, "A+", "增大字号").Clicked() {
				mw.app.UpdateSettings(func(s *Settings) { s.OverlayFontSize = min(s.OverlayFontSize+2, 36) })
			}
		}
		if smallTextButton(c, s.SubtitleMode.String(), "切换字幕模式").Clicked() {
			mw.app.UpdateSettings(func(s *Settings) { s.SubtitleMode = SubtitleDisplayMode((int(s.SubtitleMode) + 1) % 3) })
		}
		if iconButton(c, icTarget, "自动跟随", s.AutoFollow).Clicked() {
			mw.app.UpdateSettings(func(s *Settings) { s.AutoFollow = !s.AutoFollow })
			mw.floatLastIdx = -1
		}
		if iconButton(c, icPin, "置顶", s.FloatPinned).Clicked() {
			mw.app.UpdateSettings(func(s *Settings) { s.FloatPinned = !s.FloatPinned })
			mw.applyOverlaySettings()
		}
		if iconButton(c, icClose, "关闭", false).Clicked() {
			mw.floatWin.Close()
		}
	})
}

func smallTextButton(c *ui.Context, text, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Height(28).Padding(0, 8).Radius(14)
	if b.Pressed() {
		b.Background(t.Text.Alpha(0.16))
	} else if b.Hovered() {
		b.Background(t.Text.Alpha(0.09))
	}
	b.Children(func() { ui.Text(c, text).FontSize(12).Bold().TextColor(t.Text.Alpha(0.85)) })
	return b
}

// floatLine is one subtitle of the floating window. Context lines are
// smaller and dim; maxLines of 1 keeps them to one line, 0 lets them wrap.
func (mw *mainWindow) floatLine(c *ui.Context, seg SubtitleSegment, current, dim bool, settings Settings, maxLines int) {
	enColor, zhColor := subtitleColors(settings)
	fontSize := float32(settings.OverlayFontSize)
	if dim {
		enColor, zhColor = enColor.Alpha(0.45), zhColor.Alpha(0.45)
		fontSize *= 0.8
	}
	outline := !settings.OverlayPlateEnabled || settings.FloatingBackgroundOpacity < 0.25
	line := ui.Column(c).Gap(3).Padding(6, 10).Radius(12)
	if current && settings.OverlayPlateEnabled && maxLines == 0 {
		line.Background(ui.RGBA(255, 255, 255, 0.08))
	}
	if line.Clicked() {
		mw.seekToSentence(seg.Start)
	}
	line.Children(func() {
		text := func(s string, size float32, color ui.Color, bold bool) {
			weight := 400
			if bold {
				weight = 600
			}
			overlayText(c, s, size, color, weight, ui.Start, maxLines, 1.3, outline)
		}
		translation := seg.EffectiveTranslation()
		if settings.SubtitleMode != DisplayTranslated || translation == "" {
			text(seg.Original, fontSize, enColor, current)
		}
		if settings.SubtitleMode != DisplayOriginal && translation != "" {
			text(translation, max(fontSize-3, 11), zhColor, false)
		}
	})
}

// overlayText draws a line of the overlays. Over a plate too faint to
// read on (outline), a blurred black copy under the text stands in for a
// text shadow, which keeps it legible over any background; MyGo's Shadow
// is a box shadow.
func overlayText(c *ui.Context, s string, size float32, color ui.Color, weight int, align ui.Align, maxLines int, lineHeight float32, outline bool) {
	style := func(e ui.Element) ui.Element {
		e = e.FontSize(size).FontWeight(weight).TextAlign(align)
		if lineHeight > 0 {
			e = e.LineHeight(lineHeight)
		}
		switch {
		case maxLines == 1:
			e = e.SingleLine().Ellipsis("…")
		case maxLines > 0:
			e = e.MaxLines(maxLines).Ellipsis("…")
		}
		return e
	}
	if !outline {
		style(ui.Text(c, s)).TextColor(color)
		return
	}
	ui.Box(c).Children(func() {
		style(ui.Text(c, s)).FontWeight(min(weight+200, 900)).TextColor(ui.RGBA(0, 0, 0, 0.85)).
			Absolute().Top(0).Left(0).Right(0).Blur(1.2)
		style(ui.Text(c, s)).TextColor(ui.RGBA(0, 0, 0, 0.9)).Absolute().Top(0).Left(0).Right(0).Blur(12)
		style(ui.Text(c, s)).TextColor(color)
	})
}

func (mw *mainWindow) openLyricsWindow() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:         "桌面歌词",
		Width:         1000,
		Height:        lyricsBarSpace + 120,
		MinWidth:      360,
		MinHeight:     lyricsBarSpace + 60,
		Frameless:     true,
		Transparent:   true,
		AlwaysOnTop:   true,
		Panel:         true,
		DisableShadow: true,
		StateKey:      "lyrics",
		Content:       ui.View(mw.lyricsView),
	})
	win.SetVisibleOnAllWorkspaces(true)
	mw.lyricsWin = win
	mw.app.lyricsWin = win
	mw.lyricsLocked = false
	win.OnClosed(func() {
		mw.lyricsWin = nil
		mw.app.lyricsWin = nil
		mw.lyricsLocked = false
		mw.app.InvalidateMain()
	})
	mw.app.InvalidateMain()
}

// setLyricsLocked lets clicks through the desktop lyrics, or takes them
// back. The tray icon and ⇧⌘L unlock them.
func (mw *mainWindow) setLyricsLocked(locked bool) {
	if mw.lyricsWin == nil {
		return
	}
	mw.lyricsLocked = locked
	mw.lyricsWin.SetIgnoreMouseEvents(locked)
	mw.app.InvalidateAll()
}

// lyricsView is the desktop lyrics: the current line, large, on a plate
// whose height follows the text, with a toolbar above it on hover.
func (mw *mainWindow) lyricsView(c *ui.Context) {
	snap := mw.app.snapshot()
	c.SetTheme(ui.DarkTheme())
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	W, H := c.Size()
	s := snap.settings

	root := ui.Box(c).Fill()
	hovered := root.Hovered() && !mw.lyricsLocked
	root.Children(func() {
		align := ui.Center
		if s.LyricsAlignLeft {
			align = ui.Start
		}
		padX := float32(40)
		if s.LyricsAlignLeft {
			padX = 28
		}
		plate := ui.Column(c).Absolute().Top(lyricsBarSpace).Left(0).Right(0).Bottom(0).Gap(6).Radius(22).
			AlignItems(align).Justify(ui.Center).Padding(16, padX, 18, padX).Clip().
			Background(ui.RGBA(0, 0, 0, float32(s.DesktopLyricsBackgroundOpacity)))
		if hovered {
			plate.Border(1, ui.RGBA(255, 255, 255, 0.28))
		}
		if !mw.lyricsLocked {
			plate.DragWindow()
		}
		// The window takes the height the text needs at its width.
		if want := lyricsBarSpace + mw.lyricsTextHeight(c, &snap, W-2*padX); want-H > 1 || H-want > 1 {
			mw.lyricsWin.SetSize(int(W+0.5), int(want+0.5))
		}
		plate.Children(func() {
			doc := snap.currentTranscript()
			idx := snap.currentSegmentIndex()
			if doc == nil || idx < 0 || idx >= len(doc.Segments) {
				ui.Text(c, "EchoSub 桌面歌词").FontSize(20).TextColor(t.TextMuted)
				return
			}
			seg := doc.Segments[idx]
			enColor, zhColor := subtitleColors(s)
			outline := s.DesktopLyricsBackgroundOpacity < 0.25
			textAlign := ui.Center
			if s.LyricsAlignLeft {
				textAlign = ui.Start
			}
			translation := seg.EffectiveTranslation()
			if s.SubtitleMode != DisplayTranslated || translation == "" {
				overlayText(c, seg.Original, float32(s.DesktopEnglishFontSize), enColor, 700, textAlign, 4, 0, outline)
			}
			if s.SubtitleMode != DisplayOriginal && translation != "" {
				overlayText(c, translation, float32(s.DesktopChineseFontSize), zhColor, 600, textAlign, 3, 0, outline)
			}
			if s.LyricsProgress {
				p := doc.SegmentProgress(idx, snap.playback)
				ui.Box(c).Absolute().Left(0).Bottom(0).Height(3).WidthPercent(p*100).
					Gradient(t.Accent.Alpha(0.2), ui.Hex("#6CB8FF"), 90)
			}
		})
		if hovered {
			mw.lyricsToolbar(c, &snap, W)
		}
	})
}

// lyricsTextHeight is the plate's height for the current line wrapped at
// width: its padding and the English and Chinese lines, at most as many
// lines as the plate shows.
func (mw *mainWindow) lyricsTextHeight(c *ui.Context, snap *uiSnapshot, width float32) float32 {
	const padY = 16 + 18
	s := snap.settings
	doc := snap.currentTranscript()
	idx := snap.currentSegmentIndex()
	if doc == nil || idx < 0 || idx >= len(doc.Segments) {
		_, h := c.MeasureText(width, ui.Span{Text: "EchoSub 桌面歌词", Size: 20})
		return padY + h
	}
	seg := doc.Segments[idx]
	measure := func(text string, size float32, weight, maxLines int) float32 {
		_, h := c.MeasureText(width, ui.Span{Text: text, Size: size, Weight: weight})
		_, one := c.MeasureText(0, ui.Span{Text: "Ag", Size: size, Weight: weight})
		return min(h, one*float32(maxLines))
	}
	h := float32(padY)
	translation := seg.EffectiveTranslation()
	lines := 0
	if s.SubtitleMode != DisplayTranslated || translation == "" {
		h += measure(seg.Original, float32(s.DesktopEnglishFontSize), 700, 4)
		lines++
	}
	if s.SubtitleMode != DisplayOriginal && translation != "" {
		h += measure(translation, float32(s.DesktopChineseFontSize), 600, 3)
		lines++
	}
	if lines == 2 {
		h += 6
	}
	return h + 2
}

func (mw *mainWindow) lyricsToolbar(c *ui.Context, snap *uiSnapshot, W float32) {
	s := snap.settings
	hasDoc := snap.currentTranscript() != nil
	sep := func() { ui.Box(c).Size(1, 16).Background(ui.RGBA(255, 255, 255, 0.18)).Margin(0, 4) }
	bump := func(delta float64, zh bool) func() {
		return func() {
			mw.app.UpdateSettings(func(s *Settings) {
				if zh {
					s.DesktopChineseFontSize = min(max(s.DesktopChineseFontSize+delta, 12), 48)
				} else {
					s.DesktopEnglishFontSize = min(max(s.DesktopEnglishFontSize+delta, 12), 48)
				}
			})
		}
	}
	ui.Row(c).Absolute().Top(4).Left(0).Right(0).Margin(0, ui.Auto).Height(40).Padding(0, 6).Gap(1).Radius(20).AlignItems(ui.Center).
		Background(ui.RGBA(30, 30, 34, 0.72)).Border(1, ui.RGBA(255, 255, 255, 0.14)).Children(func() {
		if iconButton(c, icPrev, "上一句", false).Disabled(!hasDoc).Clicked() {
			mw.prevSentence()
		}
		playIcon := icPlay
		if snap.playerState == 1 {
			playIcon = icPause
		}
		if iconButton(c, playIcon, "播放 / 暂停", false).Clicked() {
			mw.playerToggle()
		}
		if iconButton(c, icNext, "下一句", false).Disabled(!hasDoc).Clicked() {
			mw.nextSentence()
		}
		sep()
		if W >= 560 {
			for _, b := range []struct {
				text, label string
				fn          func()
			}{
				{"A−", "英文字号 −", bump(-2, false)},
				{"A+", "英文字号 +", bump(2, false)},
				{"中−", "中文字号 −", bump(-2, true)},
				{"中+", "中文字号 +", bump(2, true)},
			} {
				if smallTextButton(c, b.text, b.label).Clicked() {
					b.fn()
				}
			}
			sep()
			alignIcon := icAlignC
			if s.LyricsAlignLeft {
				alignIcon = icAlignL
			}
			if iconButton(c, alignIcon, "对齐：居中 / 左对齐", false).Clicked() {
				mw.app.UpdateSettings(func(s *Settings) { s.LyricsAlignLeft = !s.LyricsAlignLeft })
			}
		} else {
			iconButton(c, icMore, "更多", false).PopoverMenuButton(func(m *ui.Menu) {
				if m.Item("英文字号 +").Chosen() {
					bump(2, false)()
				}
				if m.Item("英文字号 −").Chosen() {
					bump(-2, false)()
				}
				if m.Item("中文字号 +").Chosen() {
					bump(2, true)()
				}
				if m.Item("中文字号 −").Chosen() {
					bump(-2, true)()
				}
				m.Separator()
				align := "左对齐"
				if s.LyricsAlignLeft {
					align = "居中"
				}
				if m.Item("切换为" + align).Chosen() {
					mw.app.UpdateSettings(func(s *Settings) { s.LyricsAlignLeft = !s.LyricsAlignLeft })
				}
			})
			sep()
		}
		ui.Icon(c, icContrast).FontSize(14).TextColor(ui.RGBA(255, 255, 255, 0.6)).Margin(0, 4)
		opacity := s.DesktopLyricsBackgroundOpacity
		if ui.Slider(c, &opacity, 0, 1).Width(84).Label("背景透明度").Changed() {
			v := opacity
			mw.app.UpdateSettings(func(s *Settings) { s.DesktopLyricsBackgroundOpacity = v })
		}
		sep()
		if iconButton(c, icLock, "锁定：鼠标穿透 ⇧⌘L", false).Clicked() {
			mw.setLyricsLocked(true)
		}
		if iconButton(c, icClose, "关闭", false).Clicked() {
			mw.lyricsWin.Close()
		}
	})
}
