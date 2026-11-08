// Package app 是 CloudPix 的界面层：窗口、导航、上传页、历史页与设置页。
//
// 线程模型（MyGo 原生 UI 的约定）：
//   - 所有 UI 状态只在主线程读写；
//   - 后台协程通过 app.post（内部为 Window.Update）把结果交回主线程；
//   - 上传队列、文件 IO、HTTP 请求全部在 upload 包的后台协程里完成。
package app

import (
	"context"
	"fmt"
	"image"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/gen2brain/avif"
	xdraw "golang.org/x/image/draw"

	"cloudpix/internal/store"
	"cloudpix/internal/upload"
)

// providerState 是后台协程可见的配置快照。主线程更新它时用原子写，
// 避免上传协程与设置页共享可变结构体。
type providerState struct {
	prof store.Profile
}

type app struct {
	win *mygo.Window

	cfg   store.Config
	prof  store.Profile
	draft store.Profile // 设置页正在编辑的副本

	state     atomic.Pointer[providerState]
	reportCfg atomic.Pointer[store.Report]
	visionCfg atomic.Pointer[store.Vision]
	mgr       *upload.Manager

	// visionSem 让标签请求串行执行，避免一次上传多张时把视觉接口打满。
	visionSem chan struct{}

	history []store.Record

	page        int
	settingsTab int // 设置页的子页签：0 上传接口，1 通用设置
	toast       string
	toastSeq    int // 每条提示的序号
	toastedSeq  int // 已经交给 c.Toast 的序号，避免逐帧重复投递

	previews     map[string]*ui.Bitmap
	previewTried map[string]bool

	// 上传进度回调很频繁；这里把它们合并成最多每 100 毫秒重绘一帧，
	// 避免多次 RequestFrame 造成整帧反复重建。
	paintMu     sync.Mutex
	lastPaint   time.Time
	paintQueued bool

	uploadList ui.ScrollState
	histList   ui.ScrollState
	setScroll  ui.ScrollState
}

const (
	pageUpload   = 0
	pageHistory  = 1
	pageSettings = 2
)

// Run 启动应用，直到用户退出。
func Run() error {
	var a *app
	mygo.App.SetLanguages("zh-Hans")
	mygo.App.WhenReady(func() {
		a = newApp()
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           mygo.App.Name(),
			Width:           1080,
			Height:          720,
			MinWidth:        900,
			MinHeight:       600,
			BackgroundColor: "light-dark(#ffffff, #141416)",
			// 与 mygo-qyin 一致的沉浸式标题栏：隐藏系统标题栏，窗口按钮
			// 浮在页面右上角，页面顶部留出可拖动的条。
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       titleBarHeight,
			TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
			Content:              ui.View(a.view),
		})
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		if a == nil {
			return
		}
		a.mgr.Close()
		if err := store.SaveConfig(a.cfg); err != nil {
			log.Printf("保存配置失败：%v", err)
		}
	})
	return mygo.App.Run()
}

func newApp() *app {
	a := &app{
		previews:     map[string]*ui.Bitmap{},
		previewTried: map[string]bool{},
		visionSem:    make(chan struct{}, 1),
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		log.Printf("读取配置失败：%v", err)
		cfg = store.Default()
	}
	a.cfg = cfg
	a.prof = cfg.ActiveProfile()
	a.draft = a.prof
	a.history = store.LoadHistory()

	a.mgr = upload.NewManager(a.provider, a.changed, a.done)
	a.mgr.Configure(a.prof.Prefix, a.cfg.KeepName, a.processConfig(), a.prof.LinkParams)
	a.mgr.SetBatch(a.cfg.Batch)
	a.mgr.SetReporter(a.reportImage)
	rep := a.cfg.Report
	a.reportCfg.Store(&rep)
	vis := a.cfg.Vision
	a.visionCfg.Store(&vis)
	a.state.Store(&providerState{prof: a.prof})
	return a
}

// post 把 fn 交回主线程执行并重绘。
func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(fn)
}

// changed 由上传队列的后台协程调用；任务列表每帧从 Manager 拉取，
// 因此这里只需要触发一帧重绘。多个任务同时上传时回调会很密集，
// 合并为最多每 100 毫秒一帧，保证最后一次状态变化仍会刷新。
func (a *app) changed() {
	a.paintMu.Lock()
	if a.paintQueued {
		a.paintMu.Unlock()
		return
	}
	wait := 100*time.Millisecond - time.Since(a.lastPaint)
	if wait > 0 {
		a.paintQueued = true
		a.paintMu.Unlock()
		time.AfterFunc(wait, func() {
			a.paintMu.Lock()
			a.paintQueued = false
			a.lastPaint = time.Now()
			a.paintMu.Unlock()
			a.post(func() {})
		})
		return
	}
	a.lastPaint = time.Now()
	a.paintMu.Unlock()
	a.post(func() {})
}

// done 在上传成功后由后台协程调用：写历史、复制链接、提示。
func (a *app) done(j upload.Job) {
	a.post(func() {
		rec := store.Record{
			ID:      j.ID,
			Name:    j.Name,
			Size:    j.Size,
			Key:     j.Key,
			URL:     j.URL,
			Profile: j.Profile,
			At:      j.Finished,
		}
		if err := store.AddHistory(rec); err != nil {
			log.Printf("写入历史失败：%v", err)
		}
		a.history = append([]store.Record{rec}, a.history...)
		if len(a.history) > 300 {
			a.history = a.history[:300]
		}
		if a.cfg.AutoCopy {
			a.copyText(j.URL, "上传成功，链接已复制")
		} else {
			a.flash("上传成功：" + j.Name)
		}
	})
}

// provider 构造当前配置对应的上传器；每次上传前调用，切换配置后立即生效。
func (a *app) provider() (upload.Provider, error) {
	st := a.state.Load()
	if st == nil {
		return nil, upload.ConfigError("配置")
	}
	p := st.prof
	return upload.NewS3(upload.S3Config{
		Name:       p.Name + " / " + p.Bucket,
		Endpoint:   p.Endpoint,
		Region:     p.Region,
		Bucket:     p.Bucket,
		AccessKey:  p.AccessKey,
		SecretKey:  p.SecretKey,
		PublicBase: p.PublicBase,
		PathStyle:  p.PathStyle,
	})
}

// flash 显示一条 3 秒后自动消失的提示。
func (a *app) flash(msg string) {
	a.toast = msg
	a.toastSeq++
	seq := a.toastSeq
	time.AfterFunc(3*time.Second, func() {
		a.post(func() {
			if a.toastSeq == seq {
				a.toast = ""
			}
		})
	})
}

// copyText 复制文本到剪贴板，并给出提示。
func (a *app) copyText(text, okMsg string) {
	if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
		a.flash("复制失败：" + err.Error())
		return
	}
	a.flash(okMsg)
}

// openURL 用系统默认浏览器打开链接。
func (a *app) openURL(url string) {
	if url == "" {
		return
	}
	if err := mygo.Shell.OpenExternal(url); err != nil {
		a.flash("无法打开链接：" + err.Error())
	}
}

// pickFiles 打开系统文件选择器，把选中的图片加入上传队列。
// 在按钮的 OnClick 回调里调用，此时不在构建帧内，对话框可安全阻塞。
func (a *app) pickFiles() {
	if a.win == nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       "选择图片",
		ButtonLabel: "上传",
		DefaultPath: a.cfg.LastDir,
		Multiple:    true,
		Filters: []mygo.FileFilter{
			{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "svg"}},
			{Name: "所有文件", Extensions: []string{"*"}},
		},
	})
	if err != nil {
		a.flash("无法打开文件选择器")
		return
	}
	if len(paths) == 0 {
		return
	}
	a.cfg.LastDir = filepath.Dir(paths[0])
	if err := store.SaveConfig(a.cfg); err != nil {
		log.Printf("保存配置失败：%v", err)
	}
	if n := a.mgr.Enqueue(paths); n == 0 {
		a.flash("没有可上传的文件")
	} else {
		a.flash(fmt.Sprintf("已加入队列：%d 个文件，点“开始上传”", n))
	}
}

// pickFolder 选择文件夹，把其中所有图片（含子目录）加入上传队列。
func (a *app) pickFolder() {
	if a.win == nil {
		return
	}
	dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       "选择图片文件夹",
		ButtonLabel: "选择",
		DefaultPath: a.cfg.LastDir,
		Directory:   true,
	})
	if err != nil {
		a.flash("无法打开文件夹选择器")
		return
	}
	if len(dirs) == 0 {
		return
	}
	dir := dirs[0]
	a.cfg.LastDir = dir
	if err := store.SaveConfig(a.cfg); err != nil {
		log.Printf("保存配置失败：%v", err)
	}
	files := collectImages(dir)
	if len(files) == 0 {
		a.flash("这个文件夹里没有图片")
		return
	}
	if n := a.mgr.Enqueue(files); n == 0 {
		a.flash("没有可上传的文件")
	} else {
		a.flash(fmt.Sprintf("已加入队列：%d 张图片，点“开始上传”", n))
	}
}

// collectImages 递归收集文件夹里的图片文件；WalkDir 按路径字典序遍历，
// 队列顺序因此保持稳定。
func collectImages(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".bmp", ".svg":
			out = append(out, path)
		}
		return nil
	})
	return out
}

// saveSettings 把设置页的草稿写盘，并切换到新配置。
func (a *app) saveSettings() {
	p := a.draft
	applyPreset(&p)
	if p.ID == "" {
		p.ID = "custom"
	}
	a.cfg.Upsert(p)
	a.cfg.Active = p.ID
	if err := store.SaveConfig(a.cfg); err != nil {
		a.flash("配置保存失败：" + err.Error())
		return
	}
	a.prof = p
	a.state.Store(&providerState{prof: p})
	a.mgr.Configure(p.Prefix, a.cfg.KeepName, a.processConfig(), p.LinkParams)
	a.mgr.SetBatch(a.cfg.Batch)
	rep := a.cfg.Report
	a.reportCfg.Store(&rep)
	vis := a.cfg.Vision
	a.visionCfg.Store(&vis)
	a.flash("设置已保存")
}

// reportImage 在上传成功后把图片信息 POST 到用户配置的接口。
// 由上传队列的后台协程调用，不能直接操作 UI。
func (a *app) reportImage(m upload.ImageMeta) {
	cfg := a.reportCfg.Load()
	if cfg == nil || !cfg.Enabled || strings.TrimSpace(cfg.Endpoint) == "" {
		return
	}
	title := strings.TrimSuffix(m.Job.Name, filepath.Ext(m.Job.Name))
	if r := []rune(title); len(r) > 200 {
		title = string(r[:200])
	}
	size := m.Job.Processed
	if size <= 0 {
		size = m.Job.Size
	}
	img := upload.ReportImage{
		ID:     upload.NewReportID(),
		Title:  title,
		URL:    m.Job.URL,
		W:      m.Width,
		H:      m.Height,
		Age:    cfg.Age,
		SizeMB: float64(size) / 1e6,
		MD5:    m.MD5,
		Time:   m.Job.Finished.Format("2006-01-02 15:04:05"),
		Theme:  m.Theme,
	}
	// 标签由视觉模型生成；串行请求，避免一次传多张时打满接口。
	// 智谱等接口无法解析 AVIF 链接，优先传本地缩小的 JPEG base64。
	if v := a.visionCfg.Load(); v != nil && v.Enabled && strings.TrimSpace(v.Endpoint) != "" {
		a.visionSem <- struct{}{}
		imagePayload := m.Job.URL
		if thumb, err := upload.ThumbnailDataURL(m.Job.Path, 1024); err == nil {
			imagePayload = thumb
		}
		tags, err := upload.VisionConfig{
			Enabled:  v.Enabled,
			Endpoint: v.Endpoint,
			Token:    v.Token,
			Model:    v.Model,
		}.Labels(context.Background(), imagePayload)
		<-a.visionSem
		if err != nil {
			log.Printf("生成标签失败：%v", err)
		} else {
			img.Tags = tags
		}
	}
	rc := upload.ReportConfig{
		Enabled:  cfg.Enabled,
		Endpoint: cfg.Endpoint,
		Token:    cfg.Token,
		Age:      cfg.Age,
	}
	if err := upload.SendReport(rc, img); err != nil {
		a.post(func() { a.flash(err.Error()) })
	}
}

// processConfig 把 store 的处理参数转换成 upload 包的类型。
func (a *app) processConfig() upload.ProcessConfig {
	return upload.ProcessConfig{
		Format:  a.cfg.Process.Format,
		Quality: a.cfg.Process.Quality,
	}
}

// switchProfile 切换当前使用的配置。
func (a *app) switchProfile(id string) {
	for _, p := range a.cfg.Profiles {
		if p.ID == id {
			a.cfg.Active = id
			a.prof = p
			a.draft = p
			a.state.Store(&providerState{prof: p})
			a.mgr.Configure(p.Prefix, a.cfg.KeepName, a.processConfig(), p.LinkParams)
			_ = store.SaveConfig(a.cfg)
			return
		}
	}
}

// applyPreset 切换厂商类型时补齐默认值与端点。
func applyPreset(p *store.Profile) {
	switch p.Kind {
	case "bitiful":
		if p.Endpoint == "" || p.Endpoint == "https://s3.bitiful.net" {
			p.Endpoint = "https://s3.bitiful.net"
		}
		if p.Region == "" {
			p.Region = "cn-east-1"
		}
		p.PathStyle = false
	case "s3":
		if p.Region == "" {
			p.Region = "us-east-1"
		}
	}
}

// preview 返回列表里的缩略图。首次出现时在后台解码并缩放，解码完成后
// 再回到主线程放入缓存，避免在主线程解大图造成卡顿。
func (a *app) preview(path string) *ui.Bitmap {
	if b, ok := a.previews[path]; ok {
		return b
	}
	if a.previewTried[path] {
		return nil
	}
	a.previewTried[path] = true
	go func() {
		img, err := decodeThumb(path, 128)
		if err != nil {
			return
		}
		a.post(func() {
			a.previews[path] = ui.NewBitmap(img)
		})
	}()
	return nil
}

// decodeThumb 解码图片并把最长边缩到 maxSize 像素以内，只用于列表缩略图。
func decodeThumb(path string, maxSize int) (image.Image, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > 16<<20 {
		return nil, fmt.Errorf("图片超过 16 MB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var img image.Image
	switch strings.ToLower(filepath.Ext(path)) {
	case ".avif":
		img, err = avif.Decode(f)
	default:
		img, _, err = image.Decode(f)
	}
	if err != nil {
		return nil, err
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSize && h <= maxSize {
		return img, nil
	}
	scale := float64(maxSize) / float64(max(w, h))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst, nil
}
