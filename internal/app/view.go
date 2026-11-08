package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// titleBarHeight 是隐藏系统标题栏后，窗口按钮占据的顶部条高度。
const titleBarHeight = 46

// view 是窗口的根视图，每帧被 MyGo 调用一次。布局与 mygo-qyin 一致：
// 232 宽的侧边栏 + 40 内边距的内容区，靠色调而非分割线区分两栏。
// 窗口标题栏隐藏（TitleBarHidden），窗口按钮浮在页面右上角，页面从
// 窗口顶端开始绘制；侧栏空白处可拖动窗口。
func (a *app) view(c *ui.Context) {
	c.SetTheme(a.theme(c))
	p := paletteFor(c.Theme())
	a.ensureCategories()

	// Toast 每次消息只投递一次，避免逐帧重置它的停留时间。
	if a.toast != "" && a.toastSeq != a.toastedSeq {
		c.Toast(a.toast)
		a.toastedSeq = a.toastSeq
	}

	ui.Box(c.Key("window")).Fill().Background(p.Bg).Children(func() {
		ui.Row(c.Key("root")).Fill().AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c, p)
			a.content(c, p)
		})
	})
}

// sidebar 绘制品牌、导航与当前配置。
func (a *app) sidebar(c *ui.Context, p palette) {
	// macOS 的交通灯在左上角、压在侧栏上，需要让开；Windows 上窗口按钮
	// 在右上角，侧栏保持原来的上边距。
	top := float32(16)
	if c.TitleBar().Left > 0 {
		top = titleBarHeight + 4
	}
	ui.Column(c.Key("sidebar")).Width(232).Shrink(0).Padding(top, 12, 16, 8).
		Gap(2).Background(p.Sidebar).DragWindow().Children(func() {
		ui.Row(c).Gap(9).AlignItems(ui.Center).Padding(6, 8).Margin(0, 0, 0, 14).Children(func() {
			ui.Box(c).Size(30, 30).Radius(9).Background(p.Accent.Alpha(0.14)).Center().Children(func() {
				ui.Icon(c, iconCloud).Size(17, 17).TextColor(p.Accent)
			})
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, "CloudPix").FontSize(15).FontWeight(700)
				ui.Text(c, "图片上传").FontSize(11).TextColor(p.Faint)
			})
		})

		a.navItem(c, p, "nav-upload", pageUpload, "上传", iconUpload)
		a.navItem(c, p, "nav-history", pageHistory, "历史记录", iconHistory)
		a.navItem(c, p, "nav-settings", pageSettings, "设置", iconSettings)

		ui.Box(c).Grow(1)
		a.profileRow(c, p)
	})
}

// navItem 复刻 mygo-qyin 的导航项：38 高、10 内边距、选中用 p.Press。
func (a *app) navItem(c *ui.Context, p palette, key string, page int, label string, icon *ui.SVG) {
	selected := a.page == page
	b := ui.ButtonBase(c.Key(key)).Height(38).PaddingX(10).Gap(10).Radius(10).
		AlignItems(ui.Center).Justify(ui.Start).Label(label).Selected(selected)

	bg, color, weight := ui.Transparent, p.Muted, 500
	switch {
	case selected:
		bg, color, weight = p.Press, p.Text, 600
	case b.Hovered():
		bg, color = p.Hover, p.Text
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).
		TextColor(color)
	b.OnClick(func() {
		a.page = page
		if page == pageSettings {
			// 每次进入设置页都放弃未保存的编辑。
			a.draft = a.prof
		}
	})
	b.Children(func() {
		ui.Icon(c, icon).Size(17, 17)
		ui.Text(c, label).FontSize(13.5).FontWeight(weight).TextColor(color)
	})
}

// profileRow 是侧边栏底部的账号行，点击进入设置。
func (a *app) profileRow(c *ui.Context, p palette) {
	b := ui.ButtonBase(c.Key("profile-row")).Height(50).PaddingX(8).Radius(10).
		AlignItems(ui.Center).Justify(ui.Start).Label("当前云存储配置").OnClick(func() {
		a.page = pageSettings
		a.draft = a.prof
	})
	if b.Hovered() {
		b.Background(p.Hover)
	}
	b.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(30, 30).Radius(999).Background(p.Accent.Alpha(0.16)).Center().Children(func() {
				ui.Text(c, initial(a.prof.Name)).FontSize(13).FontWeight(700).TextColor(p.Accent)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, a.prof.Name).FontSize(12.5).FontWeight(600).SingleLine()
				bucket := a.prof.Bucket
				if bucket == "" {
					bucket = "尚未填写存储桶"
				}
				ui.Text(c, bucket).FontSize(11).TextColor(p.Faint).SingleLine().Tooltip(bucket)
			})
		})
	})
}

// content 绘制右侧内容区。
func (a *app) content(c *ui.Context, p palette) {
	ui.Column(c.Key("content")).Grow(1).MinWidth(0).
		Padding(40, 28, 40, 28).Gap(12).Children(func() {
		switch a.page {
		case pageHistory:
			a.pageHistoryView(c, p)
		case pageSettings:
			a.pageSettingsView(c, p)
		default:
			a.pageUploadView(c, p)
		}
	})
}

// initial 返回名称的首个字符，用于账号行头像。
func initial(name string) string {
	for _, r := range name {
		return string(r)
	}
	return "C"
}
