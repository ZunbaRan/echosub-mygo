package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settingsForm holds the text fields of the settings sheet until Done saves
// them; switches, sliders and palettes apply as they change.
type settingsForm struct {
	tab         int
	baseURL     string
	model       string
	apiKey      string
	supadataKey string
	englishHex  string
	chineseHex  string
	showKey     bool
	ytdlp       string

	mu         sync.Mutex
	testing    bool
	testResult string
	testOK     bool
}

var settingsTabs = []struct {
	icon  *ui.SVG
	label string
}{
	{icLanguages, "翻译"},
	{icSource, "字幕来源"},
	{icPalette, "外观"},
	{icWindow, "悬浮窗"},
	{icKeyboard, "快捷键"},
}

var translationPresets = []struct{ name, baseURL, model string }{
	{"DeepSeek", "https://api.deepseek.com", "deepseek-chat"},
	{"通义千问", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3-max"},
	{"OpenAI", "https://api.openai.com/v1", "gpt-5-mini"},
}

func (mw *mainWindow) openSettings() {
	settings := mw.app.Settings()
	creds := mw.app.Credentials()
	f := &mw.set
	f.baseURL = settings.TranslationBaseURL
	f.model = settings.TranslationModel
	f.apiKey = creds.TranslationAPIKey
	f.supadataKey = creds.SupadataAPIKey
	f.englishHex = settings.EnglishColorHex
	f.chineseHex = settings.ChineseColorHex
	f.showKey = false
	f.ytdlp = ytdlpExecutable()
	f.mu.Lock()
	f.testResult = ""
	f.mu.Unlock()
	mw.showSettings = true
}

// saveSettings saves the text fields and starts translating the current
// video when a key was just given.
func (mw *mainWindow) saveSettings() {
	f := &mw.set
	hadKey := mw.app.Credentials().TranslationAPIKey != ""
	baseURL, model := strings.TrimSpace(f.baseURL), strings.TrimSpace(f.model)
	mw.app.UpdateSettings(func(s *Settings) {
		s.TranslationBaseURL = baseURL
		s.TranslationModel = model
	})
	key, supadata := strings.TrimSpace(f.apiKey), strings.TrimSpace(f.supadataKey)
	mw.app.UpdateCredentials(func(cr *Credentials) {
		cr.TranslationAPIKey = key
		cr.SupadataAPIKey = supadata
	})
	if !hadKey && key != "" {
		if id := mw.app.snapshot().currentID; id != "" {
			go mw.app.translateMissing(id)
		}
	}
}

// testConnection translates one line with the form's endpoint.
func (mw *mainWindow) testConnection() {
	f := &mw.set
	f.mu.Lock()
	if f.testing {
		f.mu.Unlock()
		return
	}
	f.testing, f.testResult = true, "正在连接…"
	f.mu.Unlock()
	cfg := TranslationConfiguration{BaseURL: strings.TrimSpace(f.baseURL), Model: strings.TrimSpace(f.model), APIKey: strings.TrimSpace(f.apiKey)}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		start := time.Now()
		_, err := translateSegments(ctx, []SubtitleSegment{{ID: "test", Original: "Hello, nice to meet you."}}, nil, "EchoSub", cfg)
		f.mu.Lock()
		f.testing, f.testOK = false, err == nil
		if err == nil {
			f.testResult = fmt.Sprintf("连接成功 · %s · %d ms", cfg.Model, time.Since(start).Milliseconds())
		} else {
			f.testResult = err.Error()
		}
		f.mu.Unlock()
		mygo.RunOnMain(mw.app.InvalidateMain)
	}()
}

func settingsSection(c *ui.Context, t *ui.Theme, title, caption string, body func()) {
	ui.Column(c).Gap(10).Children(func() {
		ui.Column(c).Gap(3).Children(func() {
			ui.Text(c, title).Bold().FontSize(14)
			if caption != "" {
				ui.Text(c, caption).FontSize(11.5).TextColor(t.TextMuted).LineHeight(1.45)
			}
		})
		ui.Column(c).Gap(10).Padding(12, 14).Radius(14).Background(t.Text.Alpha(0.04)).Children(body)
	})
}

func settingsField(c *ui.Context, t *ui.Theme, label string, body func()) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).MinHeight(30).Children(func() {
		ui.Text(c, label).FontSize(12.5).TextColor(t.TextMuted).Width(104).Shrink(0)
		ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(body)
	})
}

func (mw *mainWindow) settingsModal(c *ui.Context, t *ui.Theme) {
	f := &mw.set
	ui.Row(c).Width(760).Height(540).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(176).Shrink(0).Padding(6, 10, 6, 2).Gap(2).Children(func() {
			ui.Text(c, "设置").FontSize(18).Bold().Margin(0, 0, 12, 10)
			for i, tab := range settingsTabs {
				b := ui.ButtonBase(c).Label(tab.label).Height(32).Padding(0, 10).Gap(10).Radius(9).AlignItems(ui.Center)
				fg := t.TextMuted
				if f.tab == i {
					b.Background(t.Accent.Alpha(0.16))
					fg = t.Text
				} else if b.Hovered() {
					b.Background(t.Text.Alpha(0.05))
				}
				b.Children(func() {
					ic := ui.Icon(c, tab.icon).FontSize(15).TextColor(fg)
					if f.tab == i {
						ic.TextColor(t.Accent)
					}
					ui.Text(c, tab.label).FontSize(13).TextColor(fg)
				})
				if b.Clicked() {
					f.tab = i
				}
			}
		})
		ui.Box(c).Width(1).Background(t.Border.Alpha(0.6)).Margin(0, 14, 0, 0)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(18).Padding(4, 8, 12, 4).Children(func() {
					switch f.tab {
					case 0:
						mw.translationTab(c, t)
					case 1:
						mw.sourcesTab(c, t)
					case 2:
						mw.appearanceTab(c, t)
					case 3:
						mw.overlaysTab(c, t)
					default:
						shortcutsTab(c, t)
					}
				})
			})
			ui.Row(c).Padding(12, 4, 2, 4).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "开关和滑杆立即生效 · 数据在 ~/Library/Application Support/EchoSub/").FontSize(11).TextColor(t.TextMuted).Grow(1).SingleLine().Ellipsis("…")
				if ui.PrimaryButton(c, "完成").Clicked() {
					mw.saveSettings()
					mw.showSettings = false
				}
			})
		})
	})
}

func (mw *mainWindow) translationTab(c *ui.Context, t *ui.Theme) {
	f := &mw.set
	settingsSection(c, t, "翻译服务", "任意 OpenAI 兼容接口。字幕按批发送，每批带前后几句作为上下文。", func() {
		settingsField(c, t, "快速选择", func() {
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, p := range translationPresets {
					if pillButton(c, nil, p.name, p.name, f.baseURL == p.baseURL).Clicked() {
						f.baseURL, f.model = p.baseURL, p.model
					}
				}
			})
		})
		settingsField(c, t, "服务地址", func() {
			ui.TextInput(c, &f.baseURL).Placeholder("https://api.deepseek.com").Grow(1)
		})
		settingsField(c, t, "模型", func() {
			ui.TextInput(c, &f.model).Placeholder("deepseek-chat").Grow(1)
		})
		settingsField(c, t, "API Key", func() {
			key := ui.TextInput(c, &f.apiKey).Placeholder("sk-…").Grow(1)
			if !f.showKey {
				key.Password()
			}
			if iconButton(c, icEyeOff, "显示 / 隐藏", f.showKey).Clicked() {
				f.showKey = !f.showKey
			}
		})
	})
	f.mu.Lock()
	testing, result, ok := f.testing, f.testResult, f.testOK
	f.mu.Unlock()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "测试连接").Disabled(testing || strings.TrimSpace(f.apiKey) == "").Clicked() {
			mw.testConnection()
		}
		color := t.TextMuted
		if result != "" && !testing {
			color = t.Danger
			if ok {
				color = t.Success
			}
		}
		ui.Text(c, result).FontSize(12).TextColor(color).Grow(1).MaxLines(2).Ellipsis("…")
	})
	ui.Text(c, "密钥以明文保存在本机 credentials.json（文件权限 600），与 Swift 版共用。").FontSize(11.5).TextColor(t.TextMuted)
}

func (mw *mainWindow) sourcesTab(c *ui.Context, t *ui.Theme) {
	f := &mw.set
	source := func(n, title, detail string, status string, color ui.Color, extra func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Text(c, n).FontSize(11).Bold().Size(22, 22).Radius(11).TextAlign(ui.Center).Padding(4, 0).Background(t.Text.Alpha(0.1)).Shrink(0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(13)
				ui.Text(c, detail).FontSize(11.5).TextColor(t.TextMuted)
				if extra != nil {
					extra()
				}
			})
			ui.Text(c, status).FontSize(11).Bold().TextColor(color).Padding(2, 8).Radius(10).Background(color.Alpha(0.14)).Shrink(0)
		})
	}
	settingsSection(c, t, "字幕来源", "按顺序尝试，前一个失败才会用下一个。公开视频不需要登录 YouTube。", func() {
		source("1", "YouTube 直连", "内置，无需配置。先读网页里的字幕轨，失败再换 Android 客户端。", "可用", t.Success, nil)
		if f.ytdlp != "" {
			source("2", "本机 yt-dlp", f.ytdlp, "已检测到", t.Success, nil)
		} else {
			source("2", "本机 yt-dlp", "没有找到。安装：brew install yt-dlp", "未安装", t.Warning, nil)
		}
		status, color := "未配置", t.TextMuted
		if strings.TrimSpace(f.supadataKey) != "" {
			status, color = "已配置", t.Success
		}
		source("3", "Supadata", "只读取已有字幕，不会触发 AI 转写费用。", status, color, func() {
			ui.TextInput(c, &f.supadataKey).Placeholder("API Key（可选）").Password().Margin(6, 0, 0, 0)
		})
	})
}

func (mw *mainWindow) appearanceTab(c *ui.Context, t *ui.Theme) {
	f := &mw.set
	s := mw.app.Settings()
	settingsSection(c, t, "字幕配色", "用于悬浮字幕和桌面歌词。主窗口里的字幕跟随系统深浅色。", func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for _, p := range subtitlePalettes {
				pal := p
				chip := ui.ButtonBase(c).Label(pal.name).Padding(6, 10, 6, 6).Gap(8).Radius(10)
				if s.SubtitlePaletteID == pal.id {
					chip.Border(2, t.Accent).Background(t.Accent.Alpha(0.10))
				} else {
					chip.Border(1, t.Border)
				}
				chip.Children(func() {
					ui.Row(c).Padding(3, 7).Gap(4).Radius(6).Background(ui.Hex("#1C1C1E")).Children(func() {
						ui.Text(c, "Aa").FontSize(11).Bold().TextColor(ui.Hex(pal.english))
						ui.Text(c, "中").FontSize(11).Bold().TextColor(ui.Hex(pal.chinese))
					})
					ui.Text(c, pal.name).FontSize(11.5)
				})
				if chip.Clicked() {
					f.englishHex, f.chineseHex = pal.english, pal.chinese
					mw.app.UpdateSettings(func(s *Settings) {
						s.SubtitlePaletteID, s.EnglishColorHex, s.ChineseColorHex = pal.id, pal.english, pal.chinese
					})
				}
			}
		})
		settingsField(c, t, "自定义", func() {
			ui.Box(c).Size(18, 18).Radius(5).Border(1, t.Border).Background(hexColor(f.englishHex, ui.Hex("#FFFFFF")))
			if ui.TextInput(c, &f.englishHex).Placeholder("英文 #FFFFFF").Grow(1).Changed() && validHex(f.englishHex) {
				hex := f.englishHex
				mw.app.UpdateSettings(func(s *Settings) { s.EnglishColorHex, s.SubtitlePaletteID = hex, "custom" })
			}
			ui.Box(c).Size(18, 18).Radius(5).Border(1, t.Border).Background(hexColor(f.chineseHex, ui.Hex("#FFD54F")))
			if ui.TextInput(c, &f.chineseHex).Placeholder("中文 #FFD54F").Grow(1).Changed() && validHex(f.chineseHex) {
				hex := f.chineseHex
				mw.app.UpdateSettings(func(s *Settings) { s.ChineseColorHex, s.SubtitlePaletteID = hex, "custom" })
			}
		})
		en, zh := subtitleColors(s)
		ui.Column(c).Padding(16).Gap(4).Radius(12).AlignItems(ui.Center).
			Gradient(ui.Hex("#2B3A67"), ui.Hex("#A0616A"), 135).Children(func() {
			ui.Column(c).Padding(10, 18).Radius(12).Gap(4).AlignItems(ui.Center).
				Background(ui.RGBA(0, 0, 0, float32(s.FloatingBackgroundOpacity)*0.6)).Children(func() {
				ui.Text(c, "Retention is super important and it's not obvious.").FontSize(float32(s.OverlayFontSize)).FontWeight(600).TextColor(en)
				ui.Text(c, "留存非常重要，而且这一点并不显而易见。").FontSize(float32(s.OverlayFontSize) - 3).TextColor(zh)
			})
		})
	})
	slider := func(label string, value float64, lo, hi float64, percent bool, apply func(*Settings, float64)) {
		settingsField(c, t, label, func() {
			v := value
			if ui.Slider(c, &v, lo, hi).Grow(1).Label(label).Changed() {
				if !percent {
					v = float64(int(v + 0.5))
				}
				mw.app.UpdateSettings(func(s *Settings) { apply(s, v) })
			}
			text := fmt.Sprintf("%d pt", int(value+0.5))
			if percent {
				text = fmt.Sprintf("%d%%", int(value*100+0.5))
			}
			ui.Text(c, text).FontSize(11).TextColor(t.TextMuted).Width(40).TextAlign(ui.End)
		})
	}
	settingsSection(c, t, "字号与透明度", "", func() {
		slider("悬浮字幕字号", s.OverlayFontSize, 12, 36, false, func(s *Settings, v float64) { s.OverlayFontSize = v })
		slider("歌词英文字号", s.DesktopEnglishFontSize, 12, 48, false, func(s *Settings, v float64) { s.DesktopEnglishFontSize = v })
		slider("歌词中文字号", s.DesktopChineseFontSize, 12, 48, false, func(s *Settings, v float64) { s.DesktopChineseFontSize = v })
		slider("悬浮字幕背景", s.FloatingBackgroundOpacity, 0, 1, true, func(s *Settings, v float64) { s.FloatingBackgroundOpacity = v })
		slider("桌面歌词背景", s.DesktopLyricsBackgroundOpacity, 0, 1, true, func(s *Settings, v float64) { s.DesktopLyricsBackgroundOpacity = v })
		slider("主窗口背景", s.MainWindowBackgroundOpacity, 0, 1, true, func(s *Settings, v float64) { s.MainWindowBackgroundOpacity = v })
	})
}

func (mw *mainWindow) overlaysTab(c *ui.Context, t *ui.Theme) {
	s := mw.app.Settings()
	toggle := func(label string, on bool, apply func(*Settings, bool)) {
		v := on
		if ui.Toggle(c, &v, label).Changed() {
			mw.app.UpdateSettings(func(s *Settings) { apply(s, v) })
			mw.applyOverlaySettings()
		}
	}
	settingsSection(c, t, "悬浮字幕", "窗口高度决定显示多少：矮于 160 只显示当前句，160–300 显示前后三句，再高显示滚动列表。", func() {
		toggle("显示底板（关闭后背景完全透明）", s.OverlayPlateEnabled, func(s *Settings, v bool) { s.OverlayPlateEnabled = v })
		toggle("置顶", s.FloatPinned, func(s *Settings, v bool) { s.FloatPinned = v })
		toggle("在所有桌面空间显示", s.ShowsOnAllSpaces, func(s *Settings, v bool) { s.ShowsOnAllSpaces = v })
	})
	settingsSection(c, t, "桌面歌词", "一次只显示当前句，宽度可拖，高度随换行变化。锁定后鼠标穿透，从菜单栏图标或 ⇧⌘L 解锁。", func() {
		settingsField(c, t, "对齐方式", func() {
			align := 0
			if s.LyricsAlignLeft {
				align = 1
			}
			if ui.Segmented(c, &align, "居中", "左对齐").Label("对齐方式").Changed() {
				left := align == 1
				mw.app.UpdateSettings(func(s *Settings) { s.LyricsAlignLeft = left })
			}
		})
		toggle("显示句内进度条", s.LyricsProgress, func(s *Settings, v bool) { s.LyricsProgress = v })
	})
}

func shortcutsTab(c *ui.Context, t *ui.Theme) {
	keys := [][2]string{
		{"播放 / 暂停", "空格"}, {"后退 / 前进 5 秒", "← →"}, {"上一句 / 下一句", "⌥← ⌥→"}, {"重听本句", "R"},
		{"单句循环", "L"}, {"窗口内全屏", "F"}, {"搜索字幕", "⌘F"}, {"播放列表", "⌃⌘S"},
		{"显示 / 隐藏侧栏", "⌃⌘T"}, {"悬浮字幕", "⇧⌘F"}, {"桌面歌词", "⇧⌘D"}, {"锁定桌面歌词", "⇧⌘L"}, {"设置", "⌘,"},
	}
	settingsSection(c, t, "快捷键", "单键快捷键在主窗口内、输入框没有焦点时生效；⌘ 组合键在 EchoSub 的任何窗口里都生效。", func() {
		for _, k := range keys {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, k[0]).FontSize(12.5).Grow(1)
				ui.Text(c, k[1]).FontSize(11.5).TextColor(t.TextMuted).Padding(1, 6).Radius(5).Border(1, t.Border)
			})
		}
	})
}
