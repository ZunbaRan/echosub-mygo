package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

const repeatShapes = `<path d="m17 2 4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14"/><path d="m7 22-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/>`

var (
	icPlay      = icon(`<polygon points="7 4 20 12 7 20 7 4" fill="currentColor"/>`)
	icPause     = icon(`<rect x="14" y="4" width="4" height="16" rx="1" fill="currentColor"/><rect x="6" y="4" width="4" height="16" rx="1" fill="currentColor"/>`)
	icBack      = icon(`<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/>`)
	icForward   = icon(`<path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/>`)
	icRepeat    = icon(repeatShapes)
	icRepeatOne = icon(repeatShapes + `<path d="M11 10h1v4"/>`)
	icCaptions  = icon(`<rect width="18" height="14" x="3" y="5" rx="2"/><path d="M7 15h4M15 15h2M7 11h2M13 11h4"/>`)
	icLyrics    = icon(`<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>`)
	icExpand    = icon(`<path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/>`)
	icExternal  = icon(`<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>`)
	icSettings  = icon(`<path d="M21 4h-7M10 4H3M21 12h-9M8 12H3M21 20h-5M12 20H3M14 2v4M8 10v4M16 18v4"/>`)
	icPlus      = icon(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	icClipboard = icon(`<rect width="8" height="4" x="8" y="2" rx="1"/><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/>`)
	icRefresh   = icon(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)
	icTrash     = icon(`<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>`)
	icLink      = icon(`<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`)
	icClose     = icon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	icVideo     = icon(`<path d="m16 13 5.2 3.1a.5.5 0 0 0 .8-.4V8.3a.5.5 0 0 0-.8-.4L16 11"/><rect x="2" y="6" width="14" height="12" rx="2"/>`)
	icShrink    = icon(`<path d="m14 10 7-7"/><path d="M20 10h-6V4"/><path d="m3 21 7-7"/><path d="M4 14h6v6"/>`)
	icSearch    = icon(`<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`)
	icMore      = icon(`<circle cx="5" cy="12" r="1.2" fill="currentColor"/><circle cx="12" cy="12" r="1.2" fill="currentColor"/><circle cx="19" cy="12" r="1.2" fill="currentColor"/>`)
	icTarget    = icon(`<circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="3"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2"/>`)
	icPanelR    = icon(`<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M15 4v16"/>`)
	icPanelL    = icon(`<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16"/>`)
	icLogo      = icon(`<rect x="3" y="5" width="18" height="14" rx="4"/><path d="M8 11h4M8 14.5h8"/>`)
	icPrev      = icon(`<path d="M19 20 9 12l10-8z" fill="currentColor"/><path d="M5 19V5"/>`)
	icNext      = icon(`<path d="m5 4 10 8-10 8z" fill="currentColor"/><path d="M19 5v14"/>`)
	icLoopOne   = icon(repeatShapes + `<path d="M11 10h1v4"/>`)
	icReplay    = icon(`<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5 5.5 5.5 0 0 1-5.5 5.5H11"/>`)
	icCheck     = icon(`<path d="M20 6 9 17l-5-5"/>`)
	icCheckC    = icon(`<circle cx="12" cy="12" r="9"/><path d="m9 12 2 2 4-4"/>`)
	icInfo      = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 16v-4M12 8h.01"/>`)
	icKey       = icon(`<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3"/>`)
	icCopy      = icon(`<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`)
	icUnlock    = icon(`<rect width="16" height="11" x="4" y="11" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.7-1.5"/>`)
	icPin       = icon(`<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>`)
	icLock      = icon(`<rect width="16" height="11" x="4" y="11" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>`)
	icAlignC    = icon(`<path d="M4 6h16M7 12h10M5 18h14"/>`)
	icAlignL    = icon(`<path d="M4 6h16M4 12h10M4 18h14"/>`)
	icEyeOff    = icon(`<path d="M10.7 5.1A10 10 0 0 1 22 12a13 13 0 0 1-1.7 2.7"/><path d="M6.6 6.6A13.5 13.5 0 0 0 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.4-1.6"/><path d="m2 2 20 20"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/>`)
	icChevL     = icon(`<path d="m15 18-6-6 6-6"/>`)
	icChevR     = icon(`<path d="m9 18 6-6-6-6"/>`)
	icGrip      = icon(`<circle cx="9" cy="6" r="1" fill="currentColor"/><circle cx="15" cy="6" r="1" fill="currentColor"/><circle cx="9" cy="12" r="1" fill="currentColor"/><circle cx="15" cy="12" r="1" fill="currentColor"/><circle cx="9" cy="18" r="1" fill="currentColor"/><circle cx="15" cy="18" r="1" fill="currentColor"/>`)
	icContrast  = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor"/>`)
	icAlert     = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 8v4M12 16h.01"/>`)
	icLanguages = icon(`<path d="m5 8 6 6M4 14l6-6 2-3M2 5h12M7 2h1M22 22l-5-10-5 10M14 18h6"/>`)
	icSource    = icon(`<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/>`)
	icPalette   = icon(`<path d="M12 22a10 10 0 1 1 10-10c0 2.8-2.2 3-4 3h-1.5a1.5 1.5 0 0 0-1 2.6A1.5 1.5 0 0 1 14.4 20c-.7 1.2-1.4 2-2.4 2z"/><circle cx="7.5" cy="10.5" r="1" fill="currentColor"/><circle cx="12" cy="7.5" r="1" fill="currentColor"/><circle cx="16.5" cy="10.5" r="1" fill="currentColor"/>`)
	icWindow    = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/>`)
	icKeyboard  = icon(`<rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10"/>`)
)

// iconButton is a round, borderless toolbar button showing one icon;
// active tints it with the accent, as a toggled toolbar item.
func iconButton(c *ui.Context, ic *ui.SVG, label string, active bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(32, 32).Radius(16)
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
	b.Children(func() { ui.Icon(c, ic).FontSize(16).TextColor(fg) })
	return b
}

func paneGlass(t *ui.Theme, clarity float32) glass.Glass {
	return glass.Glass{Clarity: clarity, Tint: t.Background.Alpha(0.38)}
}
