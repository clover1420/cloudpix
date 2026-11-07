package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"cloudpix/internal/upload"
)

// formatBytes 把字节数格式化成便于阅读的大小。
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// statusLabel 是任务状态的中文名。
func statusLabel(s upload.JobStatus) string {
	switch s {
	case upload.StatusQueued:
		return "待上传"
	case upload.StatusWaiting:
		return "排队中"
	case upload.StatusHashing:
		return "计算校验值"
	case upload.StatusUploading:
		return "上传中"
	case upload.StatusDone:
		return "已完成"
	case upload.StatusFailed:
		return "失败"
	default:
		return string(s)
	}
}

// statusColor 是任务状态对应的颜色。
func statusColor(p palette, s upload.JobStatus) ui.Color {
	switch s {
	case upload.StatusDone:
		return okColor(p.Dark)
	case upload.StatusFailed:
		return badColor(p.Dark)
	default:
		return p.Muted
	}
}

// shortTime 把时间格式化成本地短时间。
func shortTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("01-02 15:04")
}

// heading 复刻 mygo-qyin 的页面标题：小号副标题在上，30 号粗标题在下。
func (a *app) heading(c *ui.Context, p palette, title, sub string, right func()) {
	ui.Row(c).AlignItems(ui.Center).Margin(0, 0, 18, 0).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			if sub != "" {
				ui.Text(c, sub).FontSize(12).FontWeight(600).TextColor(p.Muted).LetterSpacing(0.4)
			}
			ui.Text(c, title).FontSize(30).FontWeight(700).LetterSpacing(-0.6)
		})
		if right != nil {
			right()
		}
	})
}

// primaryButton 是强调色实心按钮。
func primaryButton(c *ui.Context, p palette, key string, icon *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(34).PaddingX(14).Gap(8).Radius(9).
		AlignItems(ui.Center).Justify(ui.Start).Label(label).Background(p.Accent).TextColor(p.OnAccent)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(16, 16)
		}
		ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(p.OnAccent)
	})
	return b
}

// iconButton 复刻 mygo-qyin 的无边框图标按钮，指针悬停时浮出一层浅底。
func iconButton(c *ui.Context, p palette, key string, icon *ui.SVG, size float32, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(size+16, size+16).Center().Radius(999).
		Label(label).Tooltip(label).TextColor(p.Muted)
	if b.Hovered() {
		b.Background(p.Hover).TextColor(p.Text)
	}
	b.Children(func() { ui.Icon(c, icon).Size(size, size) })
	return b
}

// quietButton 是描边弱按钮，用于清空、次要操作。
func quietButton(c *ui.Context, p palette, key string, label string) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(34).PaddingX(14).Radius(9).
		AlignItems(ui.Center).Justify(ui.Start).Label(label).Border(1, p.Hairline)
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.Muted)
	})
	return b
}

// segmented 是外观与 mygo-qyin 一致的分段控件，返回是否改变了选项。
func segmented(c *ui.Context, p palette, key string, selected *int, labels ...string) bool {
	before := *selected
	ui.Row(c.Key(key)).Height(30).Padding(2).Radius(15).Gap(2).
		Background(p.Hover).Children(func() {
		for i, label := range labels {
			i := i
			seg := ui.ButtonBase(c.Key(label)).Height(26).PaddingX(14).Radius(13).
				AlignItems(ui.Center).Justify(ui.Center).Label(label)
			chosen := i == *selected
			if chosen {
				seg.Background(p.Press)
			} else if seg.Hovered() {
				seg.Background(p.Hover)
			}
			if seg.Clicked() {
				*selected = i
			}
			seg.Children(func() {
				color := p.Muted
				if chosen {
					color = p.Text
				}
				ui.Text(c, label).FontSize(13).TextColor(color).SingleLine()
			})
		}
	})
	return *selected != before
}

// settingsGroup 是设置页的分组卡片：20 圆角、发丝描边、行间发丝分隔。
func settingsGroup(c *ui.Context, p palette, title string, rows func()) {
	if title != "" {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.Muted).Margin(14, 4, 4, 4)
	}
	ui.Column(c).Radius(20).Background(p.Bg).Border(1, p.Hairline).
		Dividers(1, p.Hairline).Children(rows)
}

// settingsRow 是设置页的一行：左侧标签与说明，右侧控件。
func settingsRow(c *ui.Context, p palette, label, desc string, control func()) {
	ui.Row(c).Padding(12, 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, label).FontSize(13.5).FontWeight(500)
			if desc != "" {
				ui.Text(c, desc).FontSize(12).TextColor(p.Muted).LineHeight(1.4)
			}
		})
		control()
	})
}
