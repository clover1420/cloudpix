package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"cloudpix/internal/upload"
)

// pageUploadView 是“上传”页：选择图片、查看队列进度、复制结果链接。
// 选中的文件先进入待上传队列，点“开始上传”后才真正上传。
func (a *app) pageUploadView(c *ui.Context, p palette) {
	jobs := a.mgr.Jobs()
	pending := 0
	for _, j := range jobs {
		if j.Status == upload.StatusQueued {
			pending++
		}
	}

	a.heading(c, p, "上传图片", a.targetLine(), func() {
		ui.Row(c).Gap(8).Children(func() {
			if quietButton(c, p, "clear-jobs", "清空已结束").Clicked() {
				a.mgr.ClearFinished()
			}
			if quietButton(c, p, "pick-folder", "选择文件夹").Clicked() {
				a.pickFolder()
			}
			if pending > 0 {
				if quietButton(c, p, "pick-files", "继续添加").Clicked() {
					a.pickFiles()
				}
				if primaryButton(c, p, "start-upload", iconUpload, fmt.Sprintf("开始上传 %d 张", pending)).Clicked() {
					a.mgr.Start()
				}
			} else if primaryButton(c, p, "pick-files", iconUpload, "选择图片").Clicked() {
				a.pickFiles()
			}
		})
	})

	a.dropZone(c, p)
	a.reportBar(c, p)

	if len(jobs) == 0 {
		ui.Box(c.Key("empty")).FillWidth().Grow(1).Center().Children(func() {
			ui.Column(c).Center().Gap(10).Children(func() {
				ui.Icon(c, iconImage).Size(34, 34).TextColor(p.Faint)
				ui.Text(c, "队列还是空的").FontSize(14).FontWeight(600).TextColor(p.Muted)
				ui.Text(c, "点击右上角“选择图片”或“选择文件夹”，加入队列后再点“开始上传”").FontSize(12).TextColor(p.Faint)
			})
		})
		return
	}

	ui.Scroll(c.Key("job-list")).TrackScroll(&a.uploadList).Grow(1).Shrink(1).
		Gap(8).Children(func() {
		for i := len(jobs) - 1; i >= 0; i-- { // 最新的在最上面
			a.jobRow(c, p, jobs[i])
		}
	})
}

// reportBar 是上传页的分类标记选择，分类会随图片信息一起上报。
// 未启用上报时不显示，避免占位。
func (a *app) reportBar(c *ui.Context, p palette) {
	if !a.cfg.Report.Enabled {
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "分类标记").FontSize(12).TextColor(p.Muted)
		opts := []string{"不设置"}
		chosen := "不设置"
		for _, cat := range a.categories {
			opts = append(opts, cat.Name)
			if cat.CID == a.cfg.Report.CategoryID {
				chosen = cat.Name
			}
		}
		if a.cfg.Report.CategoryID > 0 && chosen == "不设置" {
			// 分类已被服务端删除时保留原选择，避免静默丢失。
			chosen = "已失效的分类"
			opts = append(opts, chosen)
		}
		if ui.Select(c.Key("report-age"), &chosen, opts).Width(160).Changed() {
			a.cfg.Report.CategoryID = 0
			a.cfg.Report.Age = ""
			for _, cat := range a.categories {
				if cat.Name == chosen {
					a.cfg.Report.CategoryID = cat.CID
					break
				}
			}
			a.publishReport()
		}
		if quietButton(c, p, "reload-categories", "刷新").Clicked() {
			a.reloadCategories()
		}
		switch {
		case a.catsLoading:
			ui.Text(c, "加载中…").FontSize(11).TextColor(p.Faint)
		case a.catErr != "":
			ui.Text(c, "分类加载失败").FontSize(11).TextColor(badColor(p.Dark))
		case len(a.categories) == 0:
			ui.Text(c, "暂无分类").FontSize(11).TextColor(p.Faint)
		}
	})
}

// targetLine 说明当前会传到哪个桶。
func (a *app) targetLine() string {
	p := a.prof
	if p.Bucket == "" {
		return "尚未配置存储桶，请先到“设置”里填写"
	}
	return p.Name + " · " + p.Bucket
}

// dropZone 是上传页的引导卡片，视觉与 mygo-qyin 的卡片一致。
func (a *app) dropZone(c *ui.Context, p palette) {
	ui.Box(c.Key("drop-zone")).FillWidth().Height(132).Radius(20).
		Background(p.Bg).Border(1, p.Hairline).Center().Children(func() {
		ui.Column(c).Center().Gap(8).Children(func() {
			ui.Icon(c, iconImage).Size(30, 30).TextColor(p.Faint)
			ui.Text(c, "选择图片，一次可以选多张").FontSize(14).FontWeight(500).TextColor(p.Muted)
			ui.Text(c, "PNG / JPG / WEBP / GIF / AVIF / SVG / BMP").
				FontSize(11.5).TextColor(p.Faint).LetterSpacing(0.3)
		})
	})
}

// jobRow 是一行上传任务。
func (a *app) jobRow(c *ui.Context, p palette, j upload.Job) {
	ui.Box(c.Key(fmt.Sprintf("job-%d", j.ID))).FillWidth().Padding(12, 16).Radius(20).
		Background(p.Bg).Border(1, p.Hairline).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			if b := a.preview(j.Path); b != nil {
				ui.Image(c.Key(fmt.Sprintf("thumb-%d", j.ID)), b).Size(46, 46).Radius(9)
			} else {
				ui.Box(c).Size(46, 46).Radius(9).Background(p.Hover).Center().Children(func() {
					ui.Text(c, "IMG").FontSize(9).FontWeight(600).TextColor(p.Faint)
				})
			}

			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, j.Name).FontSize(13.5).FontWeight(600).SingleLine().
						Shrink(1).MinWidth(0).Tooltip(j.Path)
					ui.Text(c, statusLabel(j.Status)).FontSize(11).FontWeight(500).
						TextColor(statusColor(p, j.Status))
				})
				switch j.Status {
				case upload.StatusQueued, upload.StatusWaiting:
					ui.Progress(c, 0).Height(3)
				case upload.StatusHashing:
					ui.Progress(c, -1).Height(3)
				case upload.StatusUploading:
					ui.Progress(c, j.Progress).Height(3)
				case upload.StatusDone:
					ui.Text(c, j.URL).FontSize(11.5).TextColor(p.Muted).SingleLine().Tooltip(j.URL)
				case upload.StatusFailed:
					ui.Text(c, j.Err).FontSize(11.5).TextColor(badColor(p.Dark)).LineHeight(1.4)
				}
				sizeLine := formatBytes(j.Size)
				if j.Processed > 0 && j.Processed != j.Size {
					sizeLine = formatBytes(j.Size) + " → " + formatBytes(j.Processed)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, sizeLine).FontSize(10.5).TextColor(p.Faint)
					label, color := reportLabel(j.Report, p)
					ui.Text(c, label).FontSize(10.5).TextColor(color)
				})
			})

			ui.Row(c).Gap(2).Shrink(0).AlignItems(ui.Center).Children(func() {
				switch j.Status {
				case upload.StatusDone:
					if iconButton(c, p, fmt.Sprintf("copy-%d", j.ID), iconCopy, 16, "复制链接").Clicked() {
						a.copyText(j.URL, "链接已复制")
					}
					if iconButton(c, p, fmt.Sprintf("open-%d", j.ID), iconOpen, 16, "在浏览器打开").Clicked() {
						a.openURL(j.URL)
					}
				case upload.StatusFailed:
					if iconButton(c, p, fmt.Sprintf("retry-%d", j.ID), iconRetry, 16, "重试").Clicked() {
						a.mgr.Retry(j.ID)
					}
				}
				if j.Status == upload.StatusQueued || j.Status == upload.StatusWaiting ||
					j.Status == upload.StatusDone || j.Status == upload.StatusFailed {
					if iconButton(c, p, fmt.Sprintf("remove-%d", j.ID), iconX, 16, "移除").Clicked() {
						a.mgr.Remove(j.ID)
					}
				}
			})
		})
	})
}
