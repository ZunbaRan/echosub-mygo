package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// subtitlePalette is one preset of the subtitle colors, mirroring the
// Swift app's SubtitlePalette presets.
type subtitlePalette struct {
	id      string
	name    string
	english string
	chinese string
}

var subtitlePalettes = []subtitlePalette{
	{id: "classic", name: "经典白 / 黄", english: "#FFFFFF", chinese: "#FFD54F"},
	{id: "uniform-white", name: "统一高对比白", english: "#FFFFFF", chinese: "#FFFFFF"},
	{id: "soft-cyan", name: "柔和白 / 青", english: "#F7F7F7", chinese: "#7FDBFF"},
	{id: "warm", name: "暖白 / 橙", english: "#FFF8E7", chinese: "#FFB74D"},
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// validHex reports whether s is a color ui.Hex accepts.
func validHex(s string) bool {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
		return false
	}
	for i := 0; i < len(h); i++ {
		if !isHexDigit(h[i]) {
			return false
		}
	}
	return true
}

// hexColor parses s, falling back when it is empty or invalid (ui.Hex
// panics on bad input, so it is only called after validation).
func hexColor(s string, fallback ui.Color) ui.Color {
	if validHex(s) {
		return ui.Hex(s)
	}
	return fallback
}

// subtitleColors resolves the English and Chinese text colors from the
// settings, falling back to the classic palette.
func subtitleColors(s Settings) (english, chinese ui.Color) {
	return hexColor(s.EnglishColorHex, ui.Hex("#FFFFFF")),
		hexColor(s.ChineseColorHex, ui.Hex("#FFD54F"))
}

// paletteByID returns the preset with the given id, or nil.
func paletteByID(id string) *subtitlePalette {
	for i := range subtitlePalettes {
		if subtitlePalettes[i].id == id {
			return &subtitlePalettes[i]
		}
	}
	return nil
}
