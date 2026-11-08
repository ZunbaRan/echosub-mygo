package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"

	"github.com/egoist/mygo"
)

// setupMenus installs the menu bar, whose ⌘ shortcuts work in every
// EchoSub window, and the tray icon, which controls playback and the
// overlays when the main window is away or the lyrics are locked.
func (mw *mainWindow) setupMenus() {
	item := func(label, accel string, fn func()) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Accelerator: accel, Click: func(*mygo.MenuItem, *mygo.Window) { fn() }}
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "EchoSub", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			item("设置…", "CmdOrCtrl+,", mw.showMainSettings),
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "播放", Submenu: []*mygo.MenuItem{
			item("播放 / 暂停", "", mw.playerToggle),
			item("上一句", "", mw.prevSentence),
			item("下一句", "", mw.nextSentence),
			item("重听本句", "", mw.replaySentence),
			item("单句循环", "", mw.toggleSentenceLoop),
			mygo.Separator(),
			item("后退 5 秒", "", func() { mw.playerSkip(-5) }),
			item("前进 5 秒", "", func() { mw.playerSkip(5) }),
			item("播放速度", "", mw.cycleSpeed),
		}},
		{Label: "窗口", Submenu: []*mygo.MenuItem{
			item("显示 / 隐藏悬浮字幕", "Shift+CmdOrCtrl+F", mw.toggleFloatWindow),
			item("显示 / 隐藏桌面歌词", "Shift+CmdOrCtrl+D", mw.toggleLyricsWindow),
			item("锁定 / 解锁桌面歌词", "Shift+CmdOrCtrl+L", func() { mw.setLyricsLocked(!mw.lyricsLocked) }),
			mygo.Separator(),
			item("显示 / 隐藏字幕面板", "Ctrl+Cmd+T", func() { mw.toggleSubPane(); mw.app.InvalidateMain() }),
			item("显示 / 隐藏视频库", "Ctrl+Cmd+S", func() { mw.toggleLibrary(); mw.app.InvalidateMain() }),
			mygo.Separator(),
			{Role: mygo.RoleMinimize},
			{Role: mygo.RoleZoom},
			{Role: mygo.RoleToggleFullScreen},
		}},
	}))

	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "echosub", Icon: trayIconPNG(), IconIsTemplate: true, ToolTip: "EchoSub"})
	if err != nil {
		log.Printf("echosub: tray: %v", err)
		return
	}
	mw.tray = tray
	mw.syncTray(nil)
}

func (mw *mainWindow) showMainSettings() {
	mw.win.Show()
	mw.win.Focus()
	mw.openSettings()
	mw.app.InvalidateMain()
}

// syncTray rebuilds the tray's menu when what it shows changed.
func (mw *mainWindow) syncTray(snap *uiSnapshot) {
	if mw.tray == nil {
		return
	}
	if snap == nil {
		s := mw.app.snapshot()
		snap = &s
	}
	video := snap.currentVideo()
	hasDoc := snap.currentTranscript() != nil
	title := "EchoSub"
	if video != nil {
		title = video.Title
		if r := []rune(title); len(r) > 36 {
			title = string(r[:36]) + "…"
		}
	}
	playing := snap.playerState == 1
	mode := snap.settings.SubtitleMode
	sig := fmt.Sprint(title, playing, hasDoc, mw.floatWin != nil, mw.lyricsWin != nil, mw.lyricsLocked, mode)
	if sig == mw.traySig {
		return
	}
	mw.traySig = sig

	do := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { fn(); mw.app.InvalidateAll() }
	}
	playLabel := "播放"
	if playing {
		playLabel = "暂停"
	}
	items := []*mygo.MenuItem{
		{Label: title, Disabled: true},
		{Label: playLabel, Disabled: video == nil, Click: do(mw.playerToggle)},
		{Label: "上一句", Disabled: !hasDoc, Click: do(mw.prevSentence)},
		{Label: "下一句", Disabled: !hasDoc, Click: do(mw.nextSentence)},
		mygo.Separator(),
		{Label: "悬浮字幕", Type: mygo.MenuItemCheckbox, Checked: mw.floatWin != nil, Click: do(mw.toggleFloatWindow)},
		{Label: "桌面歌词", Type: mygo.MenuItemCheckbox, Checked: mw.lyricsWin != nil, Click: do(mw.toggleLyricsWindow)},
		{Label: "锁定桌面歌词", Type: mygo.MenuItemCheckbox, Checked: mw.lyricsLocked, Disabled: mw.lyricsWin == nil,
			Click: do(func() { mw.setLyricsLocked(!mw.lyricsLocked) })},
		mygo.Separator(),
	}
	for i, label := range []string{"字幕：原文", "字幕：中文", "字幕：双语"} {
		m := SubtitleDisplayMode(i)
		items = append(items, &mygo.MenuItem{Label: label, Type: mygo.MenuItemRadio, Checked: mode == m,
			Click: do(func() { mw.app.UpdateSettings(func(s *Settings) { s.SubtitleMode = m }) })})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "打开 EchoSub 主窗口", Click: do(func() { mw.win.Show(); mw.win.Focus() })},
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleQuit},
	)
	mw.tray.SetMenu(mygo.NewMenu(items))
}

// trayIconPNG draws the tray's template icon, a subtitle frame, black on
// transparent at 32×32 pixels for the 16-point menu bar.
func trayIconPNG() []byte {
	const size = 32
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	ink := color.NRGBA{A: 255}
	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.SetNRGBA(x, y, ink)
			}
		}
	}
	// The frame, with clipped corners for a rounded look.
	fill(5, 6, 27, 9)
	fill(5, 23, 27, 26)
	fill(3, 8, 6, 24)
	fill(26, 8, 29, 24)
	// Two lines of subtitles.
	fill(9, 13, 14, 16)
	fill(16, 13, 23, 16)
	fill(9, 18, 19, 21)
	fill(21, 18, 23, 21)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
