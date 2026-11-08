package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"cloudpix/internal/store"
)

// pageHistoryView 是“历史记录”页：最近上传成功的对象与链接。
func (a *app) pageHistoryView(c *ui.Context, p palette) {
	a.heading(c, p, "历史记录", fmt.Sprintf("共 %d 条，保存在本地", len(a.history)), func() {
		if len(a.history) > 0 {
			quietButton(c, p, "clear-history", "清空").OnClick(a.confirmClearHistory)
		}
	})

	if len(a.history) == 0 {
		ui.Box(c.Key("history-empty")).FillWidth().Grow(1).Center().Children(func() {
			ui.Column(c).Center().Gap(10).Children(func() {
				ui.Icon(c, iconHistory).Size(34, 34).TextColor(p.Faint)
				ui.Text(c, "还没有上传记录").FontSize(14).FontWeight(600).TextColor(p.Muted)
				ui.Text(c, "上传成功后，链接会出现在这里").FontSize(12).TextColor(p.Faint)
			})
		})
		return
	}

	ui.Scroll(c.Key("history-list")).TrackScroll(&a.histList).Grow(1).Shrink(1).
		Gap(8).Children(func() {
		for i, r := range a.history {
			a.historyRow(c, p, i, r)
		}
	})
}

func (a *app) historyRow(c *ui.Context, p palette, index int, r store.Record) {
	// 记录 ID 每次运行都从 1 开始，跨会话会重复；用下标组合保证 Key 唯一。
	ui.Box(c.Key(fmt.Sprintf("hist-%d-%d", index, r.ID))).FillWidth().Padding(12, 16).Radius(20).
		Background(p.Bg).Border(1, p.Hairline).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(46, 46).Radius(9).Background(p.Hover).Center().Children(func() {
				ui.Icon(c, iconImage).Size(20, 20).TextColor(p.Faint)
			})

			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, r.Name).FontSize(13.5).FontWeight(600).SingleLine().
						Shrink(1).MinWidth(0)
					ui.Text(c, shortTime(r.At)).FontSize(11).TextColor(p.Faint)
					ui.Text(c, formatBytes(r.Size)).FontSize(11).TextColor(p.Faint)
					label, color := reportLabel(r.Report, p)
					ui.Text(c, label).FontSize(11).TextColor(color)
				})
				ui.Text(c, r.URL).FontSize(11.5).TextColor(p.Muted).SingleLine().Tooltip(r.URL)
			})

			ui.Row(c).Gap(2).Shrink(0).AlignItems(ui.Center).Children(func() {
				if iconButton(c, p, fmt.Sprintf("hist-copy-%d-%d", index, r.ID), iconCopy, 16, "复制链接").Clicked() {
					a.copyText(r.URL, "链接已复制")
				}
				if iconButton(c, p, fmt.Sprintf("hist-open-%d-%d", index, r.ID), iconOpen, 16, "在浏览器打开").Clicked() {
					a.openURL(r.URL)
				}
			})
		})
	})
}

// confirmClearHistory 用系统原生对话框确认后清空历史。
func (a *app) confirmClearHistory() {
	if a.win == nil {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageQuestion,
		Title:              "清空历史",
		Message:            "确定清空全部上传历史吗？",
		Detail:             "只会删除本地记录，不会删除云端的文件。",
		Buttons:            []string{"清空", "取消"},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	if err := store.ClearHistory(); err != nil {
		a.flash("清空失败：" + err.Error())
		return
	}
	a.history = nil
	a.flash("历史已清空")
}
