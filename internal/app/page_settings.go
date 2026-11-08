package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"cloudpix/internal/store"
)

// visionPreset 是「自动标签」里可一键填入的视觉接口预设。
type visionPreset struct {
	Name     string
	Endpoint string
	Model    string
}

// visionPresets 按国内网络的可达性挑选：百炼、智谱、硅基流动、Kimi
// 均可直连，OpenAI 需要代理，本地 Ollama 需要先启动服务。
var visionPresets = []visionPreset{
	{"阿里云百炼", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", "qwen-vl-max"},
	{"智谱 GLM", "https://open.bigmodel.cn/api/paas/v4/chat/completions", "glm-4v-flash"},
	{"硅基流动", "https://api.siliconflow.cn/v1/chat/completions", "Qwen/Qwen2.5-VL-7B-Instruct"},
	{"Kimi", "https://api.moonshot.cn/v1/chat/completions", "moonshot-v1-8k-vision-preview"},
	{"本地 Ollama", "http://127.0.0.1:11434/v1/chat/completions", "qwen2.5vl:7b"},
	{"OpenAI", "https://api.openai.com/v1/chat/completions", "gpt-4o-mini"},
}

// pageSettingsView 是“设置”页。布局沿用 mygo-qyin：页面标题 + 分组卡片，
// 每行左侧标签与说明、右侧控件。页内分成「上传接口」与「通用设置」两个
// 子页签，分别管理云存储连接和与厂商无关的上传行为。
func (a *app) pageSettingsView(c *ui.Context, p palette) {
	a.heading(c, p, "设置", "上传接口与通用上传设置分开管理", func() {
		if primaryButton(c, p, "save-settings", nil, "保存设置").Clicked() {
			a.saveSettings()
		}
	})

	ui.Row(c).Gap(8).Margin(0, 0, 8, 0).Children(func() {
		segmented(c, p, "settings-tab", &a.settingsTab, "上传接口", "通用设置")
	})

	ui.Scroll(c.Key("settings-scroll")).TrackScroll(&a.setScroll).Grow(1).Shrink(1).
		MinWidth(0).Gap(8).Children(func() {
		if a.settingsTab == 0 {
			a.storageProfileGroup(c, p)
			a.storageGroup(c, p)
			a.linkGroup(c, p)
			a.credentialGroup(c, p)
			ui.Text(c, "访问密钥随配置一起保存在用户数据目录的 config.json（文件权限 0600），换机时复制该文件即可带走配置；请不要把它分享给他人。链接处理参数按接口保存，切换配置后自动生效。保存后立即生效，正在上传的任务继续使用旧配置完成。").
				FontSize(11.5).TextColor(p.Faint).LineHeight(1.5).Margin(6, 4, 0, 0)
			return
		}

		settingsGroup(c, p, "图片处理", func() {
			settingsRow(c, p, "目标格式", "上传前统一转码；选择“保持原格式”则直接上传，不做压缩。", func() {
				idx := 0
				switch a.cfg.Process.Format {
				case store.FormatJPEG:
					idx = 1
				case store.FormatAVIF:
					idx = 2
				case store.FormatWebP:
					idx = 3
				}
				if segmented(c, p, "process-format", &idx, "保持原格式", "JPEG", "AVIF", "WebP") {
					switch idx {
					case 1:
						a.cfg.Process.Format = store.FormatJPEG
					case 2:
						a.cfg.Process.Format = store.FormatAVIF
					case 3:
						a.cfg.Process.Format = store.FormatWebP
					default:
						a.cfg.Process.Format = store.FormatKeep
					}
				}
			})
			settingsRow(c, p, "压缩质量", "数值越高越清晰、文件越大；仅对 JPEG、WebP 与 AVIF 生效。", func() {
				labels := []string{"90 高清", "80 标准", "70 较小", "60 最小"}
				values := []int{90, 80, 70, 60}
				chosen := labels[1]
				for i, v := range values {
					if v == a.cfg.Process.Quality {
						chosen = labels[i]
					}
				}
				if ui.Select(c.Key("process-quality"), &chosen, labels).Label("压缩质量").Width(150).Changed() {
					for i, v := range values {
						if labels[i] == chosen {
							a.cfg.Process.Quality = v
							break
						}
					}
				}
			})
		})

		settingsGroup(c, p, "上传行为", func() {
			settingsRow(c, p, "分批上传", "同时上传的张数；一张传完后自动接下一张，避免一次性发出全部请求。", func() {
				labels := []string{"1 张（逐张）", "2 张", "3 张", "5 张"}
				values := []int{1, 2, 3, 5}
				idx := 0
				for i, v := range values {
					if v == a.cfg.Batch {
						idx = i
					}
				}
				chosen := labels[idx]
				if ui.Select(c.Key("batch-size"), &chosen, labels).Label("分批上传").Width(150).Changed() {
					for i, label := range labels {
						if label == chosen {
							a.cfg.Batch = values[i]
						}
					}
				}
			})
			settingsRow(c, p, "上传后复制链接", "", func() {
				ui.Switch(c.Key("auto-copy"), &a.cfg.AutoCopy).Label("上传后复制链接")
			})
			settingsRow(c, p, "保留原文件名", "打开后对象键包含原文件名，更容易辨认。", func() {
				ui.Switch(c.Key("keep-name"), &a.cfg.KeepName).Label("保留原文件名")
			})
		})

		settingsGroup(c, p, "图片信息上报", func() {
			settingsRow(c, p, "启用上报", "上传成功后把链接、尺寸、大小、MD5 与时间 POST 到自建接口，失败只提示，不影响上传结果。", func() {
				ui.Switch(c.Key("report-enabled"), &a.cfg.Report.Enabled).Label("启用上报")
			})
			settingsRow(c, p, "接口域名", "如 https://api.example.com；上报路径 /api/v1/img/up 与分类路径 /api/v1/categories 由程序拼接。", func() {
				ui.TextInput(c, &a.cfg.Report.Endpoint).Width(280).Label("接口域名").
					Placeholder("https://api.example.com")
			})
			settingsRow(c, p, "访问令牌", "写入请求头 X-Api-Token，需与服务端 UPLOAD_TOKEN 一致。", func() {
				ui.TextInput(c, &a.cfg.Report.Token).Width(280).Label("访问令牌").Password()
			})
		})

		settingsGroup(c, p, "自动标签", func() {
			settingsRow(c, p, "启用自动标签", "上传成功后把公开链接发给视觉模型，生成中文标签并随上报写入服务端 tags 字段。", func() {
				ui.Switch(c.Key("vision-enabled"), &a.cfg.Vision.Enabled).Label("启用自动标签")
			})
			settingsRow(c, p, "接口预设", "选择预设会自动填入地址与模型；国内网络可直接使用百炼、智谱、硅基流动与 Kimi。", func() {
				names := make([]string, 0, len(visionPresets)+1)
				chosen := "自定义"
				for _, v := range visionPresets {
					names = append(names, v.Name)
					if v.Endpoint == strings.TrimSpace(a.cfg.Vision.Endpoint) {
						chosen = v.Name
					}
				}
				names = append(names, "自定义")
				if ui.Select(c.Key("vision-preset"), &chosen, names).Label("接口预设").Width(180).Changed() {
					for _, v := range visionPresets {
						if v.Name == chosen {
							a.cfg.Vision.Endpoint = v.Endpoint
							a.cfg.Vision.Model = v.Model
							break
						}
					}
				}
			})
			settingsRow(c, p, "模型接口", "兼容 OpenAI 的 chat/completions 接口。", func() {
				ui.TextInput(c, &a.cfg.Vision.Endpoint).Width(280).Label("模型接口").
					Placeholder("https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions")
			})
			settingsRow(c, p, "模型名称", "预设已填好常用模型，可按需修改。", func() {
				ui.TextInput(c, &a.cfg.Vision.Model).Width(200).Label("模型名称").
					Placeholder("qwen-vl-max")
			})
			settingsRow(c, p, "访问令牌", "以 Bearer 方式写入 Authorization 请求头；本地 Ollama 可留空。", func() {
				ui.TextInput(c, &a.cfg.Vision.Token).Width(280).Label("访问令牌").Password()
			})
		})

		ui.Text(c, "本地处理在上传前转码文件，不改动已保存的原图；保存后立即生效。").
			FontSize(11.5).TextColor(p.Faint).LineHeight(1.5).Margin(6, 4, 0, 0)
	})
}

// storageProfileGroup 绘制当前配置的选择行；切换配置会同时切换当前生效的凭据。
func (a *app) storageProfileGroup(c *ui.Context, p palette) {
	settingsGroup(c, p, "存储配置", func() {
		settingsRow(c, p, "配置名称", "", func() {
			ui.TextInput(c, &a.draft.Name).Width(200).Label("配置名称").
				Placeholder("缤纷云")
		})
		settingsRow(c, p, "当前配置", "", func() {
			names := make([]string, 0, len(a.cfg.Profiles))
			chosen := a.prof.Name
			for _, prof := range a.cfg.Profiles {
				names = append(names, prof.Name)
			}
			if len(names) == 0 {
				names = []string{chosen}
			}
			if ui.Select(c.Key("profile-select"), &chosen, names).Label("当前配置").Width(150).Changed() {
				for _, prof := range a.cfg.Profiles {
					if prof.Name == chosen {
						a.switchProfile(prof.ID)
						break
					}
				}
			}
		})
	})
}

// storageGroup 是具体云厂商的连接参数。
func (a *app) storageGroup(c *ui.Context, p palette) {
	settingsGroup(c, p, "云存储", func() {
		settingsRow(c, p, "厂商类型", "缤纷云 S4 已填好默认端点；通用 S3 适用于 MinIO、Cloudflare R2 等。", func() {
			kind := 0
			if a.draft.Kind == "s3" {
				kind = 1
			}
			if segmented(c, p, "kind-select", &kind, "缤纷云 S4", "通用 S3") {
				if kind == 0 {
					a.draft.Kind = "bitiful"
				} else {
					a.draft.Kind = "s3"
				}
				applyPreset(&a.draft)
				if a.draft.Kind == "s3" && strings.Contains(a.draft.Endpoint, "bitiful") {
					a.draft.Endpoint = ""
					a.draft.Region = "us-east-1"
				}
			}
		})
		settingsRow(c, p, "Endpoint", "", func() {
			ui.TextInput(c, &a.draft.Endpoint).Width(280).Label("Endpoint").
				Placeholder("https://s3.bitiful.net")
		})
		settingsRow(c, p, "Region", "", func() {
			ui.TextInput(c, &a.draft.Region).Width(280).Label("Region").
				Placeholder("cn-east-1")
		})
		settingsRow(c, p, "存储桶", "", func() {
			ui.TextInput(c, &a.draft.Bucket).Width(280).Label("存储桶").
				Placeholder("bucket 名称")
		})
		settingsRow(c, p, "公开访问域名", "留空时用 Endpoint 拼出的默认地址。", func() {
			ui.TextInput(c, &a.draft.PublicBase).Width(280).Label("公开访问域名").
				Placeholder("https://cdn.example.com")
		})
		settingsRow(c, p, "对象前缀", "对象键形如 images/2026/11/07/<摘要>.jpg。", func() {
			ui.TextInput(c, &a.draft.Prefix).Width(280).Label("对象前缀").
				Placeholder("images")
		})
		settingsRow(c, p, "路径风格", "使用 endpoint/bucket/key，而不是 bucket.endpoint/key。MinIO 通常需要打开。", func() {
			ui.Switch(c.Key("path-style"), &a.draft.PathStyle).Label("路径风格")
		})
	})
}

// linkGroup 是该接口的链接媒体处理参数；不同接口可以各自设置。
func (a *app) linkGroup(c *ui.Context, p palette) {
	settingsGroup(c, p, "链接媒体处理", func() {
		settingsRow(c, p, "处理样式", "公开链接末尾自动带上该接口的媒体处理参数，访问时实时缩放或转码，不改动原图。缤纷云 S4 支持 CoreIX 参数。", func() {
			labels := []string{"原图", "缩略图 800", "缩略图 1200", "WebP 压缩", "自定义"}
			values := []string{"", "w=800", "w=1200", "fmt=webp&q=80"}
			chosen := labels[len(labels)-1]
			for i, v := range values {
				if v == a.draft.LinkParams {
					chosen = labels[i]
				}
			}
			if ui.Select(c.Key("link-style"), &chosen, labels).Label("处理样式").Width(150).Changed() {
				for i, label := range labels {
					if label == chosen && i < len(values) {
						a.draft.LinkParams = values[i]
						break
					}
				}
			}
		})
		settingsRow(c, p, "自定义参数", "可手写多个参数，如 w=800&q=80&fmt=webp；留空则使用原图链接。", func() {
			ui.TextInput(c, &a.draft.LinkParams).Width(280).Label("自定义参数").
				Placeholder("w=800&q=80")
		})
	})
}

// credentialGroup 是云存储的访问密钥。
func (a *app) credentialGroup(c *ui.Context, p palette) {
	settingsGroup(c, p, "访问密钥", func() {
		settingsRow(c, p, "AccessKey", "", func() {
			ui.TextInput(c, &a.draft.AccessKey).Width(280).Label("AccessKey")
		})
		settingsRow(c, p, "SecretKey", "", func() {
			ui.TextInput(c, &a.draft.SecretKey).Width(280).Label("SecretKey").Password()
		})
	})
}
