// 本文件复刻 mygo-qyin（lilt）的视觉体系：深浅两套调色板、网易云式红色强调色、
// 以及跨平台中文字体选择。所有页面都从这里取色，不再各自定义常量。
package app

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

// palette 是一套外观的全部颜色。
type palette struct {
	Dark     bool
	Bg       ui.Color
	Sidebar  ui.Color
	Text     ui.Color
	Muted    ui.Color
	Faint    ui.Color
	Hairline ui.Color
	Hover    ui.Color
	Press    ui.Color
	Accent   ui.Color
	OnAccent ui.Color
}

// paletteFor 依据主题深浅返回调色板，取值与 mygo-qyin 完全一致。
func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			Dark:     true,
			Bg:       ui.Hex("#141416"),
			Sidebar:  ui.Hex("#1c1c1f"),
			Text:     ui.Hex("#f5f5f7"),
			Muted:    ui.RGBA(235, 235, 245, 0.56),
			Faint:    ui.RGBA(235, 235, 245, 0.3),
			Hairline: ui.RGBA(255, 255, 255, 0.07),
			Hover:    ui.RGBA(255, 255, 255, 0.06),
			Press:    ui.RGBA(255, 255, 255, 0.1),
			Accent:   ui.Oklch(0.66, 0.2, 22),
			OnAccent: ui.Hex("#ffffff"),
		}
	}
	return palette{
		Bg:       ui.Hex("#ffffff"),
		Sidebar:  ui.Hex("#f4f4f6"),
		Text:     ui.Hex("#1d1d1f"),
		Muted:    ui.RGBA(60, 60, 67, 0.62),
		Faint:    ui.RGBA(60, 60, 67, 0.32),
		Hairline: ui.RGBA(0, 0, 0, 0.07),
		Hover:    ui.RGBA(0, 0, 0, 0.04),
		Press:    ui.RGBA(0, 0, 0, 0.08),
		Accent:   ui.Oklch(0.6, 0.21, 24),
		OnAccent: ui.Hex("#ffffff"),
	}
}

// reportLabel 返回上报状态的文案与颜色，供上传列表与历史页共用。
func reportLabel(state string, p palette) (string, ui.Color) {
	switch state {
	case "ok":
		return "已上报", okColor(p.Dark)
	case "fail":
		return "上报失败", badColor(p.Dark)
	default:
		return "未上报", p.Faint
	}
}

// 状态色沿用系统绿 / 红，在深浅外观下都清晰。
func okColor(dark bool) ui.Color {
	if dark {
		return ui.Hex("#30d158")
	}
	return ui.Hex("#248a3d")
}

func badColor(dark bool) ui.Color {
	if dark {
		return ui.Hex("#ff453a")
	}
	return ui.Hex("#d70015")
}

// fontFamily 与 mygo-qyin 一致：Windows 优先思源黑体，其余平台交给系统。
func fontFamily() string {
	if runtime.GOOS == "windows" {
		return "Noto Sans SC, Microsoft YaHei UI"
	}
	return ""
}

// theme 在系统主题基础上补齐应用配色与字体。
func (a *app) theme(c *ui.Context) *ui.Theme {
	t := *c.Theme()
	p := paletteFor(&t)
	t.Background = p.Bg
	t.Surface = p.Bg
	t.Text = p.Text
	t.TextMuted = p.Muted
	t.Border = p.Hairline
	t.Accent = p.Accent
	t.Font = fontFamily()
	return &t
}
