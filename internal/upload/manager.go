package upload

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// JobStatus 是任务的状态。
type JobStatus string

// 任务状态机：queued → waiting → hashing → uploading → done / failed。
// queued 是“已选中但还没点开始上传”；waiting 是“已开始，等上一批腾出
// 位置”，用于实现分批上传。
const (
	StatusQueued    JobStatus = "queued"
	StatusWaiting   JobStatus = "waiting"
	StatusHashing   JobStatus = "hashing"
	StatusUploading JobStatus = "uploading"
	StatusDone      JobStatus = "done"
	StatusFailed    JobStatus = "failed"
)

// Job 是一次上传任务。它是快照类型：Manager 持有指针，外部通过
// Jobs() 拿到副本，避免 UI 与后台协程共享可变数据。
type Job struct {
	ID   int64
	Path string
	Name string
	Size int64
	// Processed 是转码后的字节数；没有经过处理时为 0。
	Processed int64
	Key       string
	URL       string
	Profile   string
	Status    JobStatus
	Progress  float64
	Err       string
	Started   time.Time
	Finished  time.Time
}

// ImageMeta 是上报图片信息所需的完整数据。
type ImageMeta struct {
	Job    Job
	SHA256 string
	MD5    string
	Width  int
	Height int
	// Theme 是原图的主题色（#rrggbb），读不到时为空。
	Theme string
}

// Manager 管理上传任务：计算摘要、生成对象键、分批上传、回报进度。
// 同时上传的张数由 SetBatch 控制，默认 1 张（逐张上传）。
//
// 主线程只调用 Enqueue / Start / SetBatch / Retry / Remove / ClearFinished / Jobs；
// 其他工作都在后台协程。onChange 与 onDone 从后台协程触发，
// UI 层必须把它们 post 回主线程。
type Manager struct {
	mu       sync.Mutex
	jobs     []*Job
	provider func() (Provider, error)
	onChange func()
	onDone   func(Job)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	batch   int // 同时上传的张数
	running int // 正在运行的任务数

	nextID   int64
	prefix   string
	keepName bool
	process  ProcessConfig
	linkArgs string
	reporter func(ImageMeta)

	// heavy 串行化耗 CPU 的图片工作（转码、取主题色），避免多张同时
	// 解码编码把 CPU 打满，导致界面掉帧。
	heavy chan struct{}
}

// NewManager 创建队列。provider 每次上传前调用，返回当前配置对应的
// 上传器；这样切换云厂商不需要重建队列。
func NewManager(provider func() (Provider, error), onChange func(), onDone func(Job)) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		provider: provider,
		onChange: onChange,
		onDone:   onDone,
		ctx:      ctx,
		cancel:   cancel,
		batch:    1,
		heavy:    make(chan struct{}, 1),
	}
}

// SetBatch 设置同时上传的张数，默认 1（逐张上传）。仅在主线程调用。
func (m *Manager) SetBatch(n int) {
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	m.mu.Lock()
	m.batch = n
	ids := m.launchLocked()
	m.mu.Unlock()
	m.runAll(ids)
}

// Configure 设置对象键的前缀、命名方式、图片处理参数与链接处理参数。
// linkArgs 非空时追加到公开链接末尾（缤纷云 CoreIX 的实时处理参数）。
// 仅在主线程调用。
func (m *Manager) Configure(prefix string, keepName bool, process ProcessConfig, linkArgs string) {
	m.mu.Lock()
	m.prefix, m.keepName, m.process, m.linkArgs = prefix, keepName, process, linkArgs
	m.mu.Unlock()
}

// SetReporter 设置上传成功后的信息上报回调；回调在后台协程执行，
// 不能直接操作 UI。仅在主线程调用。
func (m *Manager) SetReporter(fn func(ImageMeta)) {
	m.mu.Lock()
	m.reporter = fn
	m.mu.Unlock()
}

// withLinkArgs 把处理参数拼到链接末尾；已有的查询串用 & 衔接。
func withLinkArgs(url, args string) string {
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "?")
	args = strings.TrimPrefix(args, "&")
	if args == "" || url == "" {
		return url
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return url + sep + args
}

// Enqueue 把本地文件加入队列，返回实际加入的数量（目录与不存在的
// 文件会被跳过）。任务停留在 queued 状态，等 Start 触发后才上传。
// 仅在主线程调用。
func (m *Manager) Enqueue(paths []string) int {
	added := 0
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		m.mu.Lock()
		m.nextID++
		m.jobs = append(m.jobs, &Job{
			ID:     m.nextID,
			Path:   p,
			Name:   filepath.Base(p),
			Size:   st.Size(),
			Status: StatusQueued,
		})
		m.mu.Unlock()
		added++
	}
	m.notify()
	return added
}

// Start 把待上传的任务标记为等待，并按批次启动，返回本次开始的数量。
// 仅在主线程调用。
func (m *Manager) Start() int {
	select {
	case <-m.ctx.Done():
		return 0
	default:
	}

	m.mu.Lock()
	armed := 0
	for _, j := range m.jobs {
		if j.Status == StatusQueued {
			j.Status = StatusWaiting
			armed++
		}
	}
	ids := m.launchLocked()
	m.mu.Unlock()

	m.runAll(ids)
	m.notify()
	return armed
}

// launchLocked 在锁内把等待中的任务转成运行状态，直到达到批次上限。
// 调用方必须持有 m.mu。
func (m *Manager) launchLocked() []int64 {
	if m.batch < 1 {
		m.batch = 1
	}
	var ids []int64
	for m.running < m.batch {
		next := int64(0)
		for _, j := range m.jobs {
			if j.Status == StatusWaiting {
				j.Status = StatusHashing
				m.running++
				next = j.ID
				break
			}
		}
		if next == 0 {
			break
		}
		ids = append(ids, next)
	}
	return ids
}

// runAll 为一批任务启动后台协程。
func (m *Manager) runAll(ids []int64) {
	for _, id := range ids {
		m.wg.Add(1)
		go m.run(id)
	}
}

// finishOne 在一条任务结束时调用：释放位置并补位启动下一张，
// 从而实现分批上传。
func (m *Manager) finishOne() {
	m.mu.Lock()
	if m.running > 0 {
		m.running--
	}
	var ids []int64
	select {
	case <-m.ctx.Done():
	default:
		ids = m.launchLocked()
	}
	m.mu.Unlock()

	m.runAll(ids)
	m.notify()
}

// run 执行一条任务：摘要 → 生成键 → 上传 → 回写结果。
// 结束时补位启动下一张，形成分批上传。
func (m *Manager) run(id int64) {
	defer func() {
		m.wg.Done()
		m.finishOne()
	}()
	select {
	case <-m.ctx.Done():
		return
	default:
	}

	var job Job
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID == id {
			job = *j
			break
		}
	}
	m.mu.Unlock()
	if job.Path == "" {
		return
	}

	m.update(id, func(j *Job) {
		j.Status = StatusHashing
		j.Started = time.Now()
	})

	m.mu.Lock()
	prefix, keepName, process, linkArgs, reporter := m.prefix, m.keepName, m.process, m.linkArgs, m.reporter
	m.mu.Unlock()

	// 按设置转码，临时文件用完即删；保持原格式时直接用源文件。
	uploadPath := job.Path
	// 转码（尤其 AVIF 走 WASM）很吃 CPU；串行执行，避免同时几张把
	// CPU 打满导致界面掉帧。
	m.heavy <- struct{}{}
	proc, err := ProcessFile(job.Path, process)
	<-m.heavy
	if err != nil {
		m.fail(id, err)
		return
	}
	if proc.Temporary {
		uploadPath = proc.Path
		defer os.Remove(proc.Path)
	}

	sha, md5sum, size, err := hashFile(uploadPath)
	if err != nil {
		m.fail(id, fmt.Errorf("读取文件失败：%w", err))
		return
	}

	prov, err := m.provider()
	if err != nil {
		m.fail(id, err)
		return
	}

	// 对象键沿用原文件名，只把扩展名换成本次输出的格式。
	keyPath := job.Path
	if proc.Ext != "" {
		keyPath = strings.TrimSuffix(filepath.Base(job.Path), filepath.Ext(job.Path)) + proc.Ext
	}
	key := BuildKey(prefix, keyPath, sha, keepName, time.Now())

	m.update(id, func(j *Job) {
		j.Profile = prov.Name()
		j.Key = key
		j.Status = StatusUploading
		if proc.Temporary {
			j.Processed = size
		}
	})

	f, err := os.Open(uploadPath)
	if err != nil {
		m.fail(id, fmt.Errorf("读取文件失败：%w", err))
		return
	}
	defer f.Close()

	var lastNotify time.Time
	progress := func(sent, total int64) {
		if total <= 0 {
			return
		}
		p := float64(sent) / float64(total)
		if p > 1 {
			p = 1
		}
		now := time.Now()
		if sent < total && now.Sub(lastNotify) < 200*time.Millisecond {
			return
		}
		lastNotify = now
		m.update(id, func(j *Job) {
			if j.Status == StatusUploading {
				j.Progress = p
			}
		})
	}

	res, err := prov.Upload(m.ctx, Object{
		Key:         key,
		Size:        size,
		ContentType: contentType(keyPath),
		SHA256:      sha,
		Body:        f,
	}, progress)
	if err != nil {
		m.fail(id, err)
		return
	}

	finished := time.Now()
	finalURL := withLinkArgs(res.URL, linkArgs)
	done := Job{
		ID:       job.ID,
		Path:     job.Path,
		Name:     job.Name,
		Size:     job.Size,
		Key:      res.Key,
		URL:      finalURL,
		Profile:  prov.Name(),
		Status:   StatusDone,
		Progress: 1,
		Started:  job.Started,
		Finished: finished,
	}
	if proc.Temporary {
		done.Processed = size
	}
	m.update(id, func(j *Job) {
		j.Status = StatusDone
		j.Progress = 1
		j.URL = finalURL
		j.Finished = finished
		if proc.Temporary {
			j.Processed = size
		}
	})
	if m.onDone != nil {
		m.onDone(done)
	}
	if reporter != nil {
		// 尺寸、主题色、MD5 都取图片处理之前的原文件；URL 仍指向上传后的对象。
		// 解整图取主题色较慢，放到独立协程，避免占用上传批次的位置。
		go func() {
			// 解码整图取主题色同样吃 CPU，与转码串行；上报与标签请求是
			// 网络 IO，放在锁外，避免拖住下一张的转码。
			m.heavy <- struct{}{}
			w, h, ok := ImageSize(job.Path)
			md5sum := md5sum
			if proc.Temporary {
				if _, sum, _, err := hashFile(job.Path); err == nil {
					md5sum = sum
				}
			}
			theme := ""
			if ok {
				theme = ThemeColor(job.Path)
			}
			<-m.heavy
			if !ok {
				return
			}
			reporter(ImageMeta{
				Job:    done,
				SHA256: sha,
				MD5:    md5sum,
				Width:  w,
				Height: h,
				Theme:  theme,
			})
		}()
	}
}

// fail 把任务置为失败。
func (m *Manager) fail(id int64, err error) {
	msg := err.Error()
	if he, ok := err.(*HTTPError); ok {
		msg = he.Error()
	}
	m.update(id, func(j *Job) {
		j.Status = StatusFailed
		j.Err = msg
		j.Finished = time.Now()
	})
}

// update 在锁内修改一条任务，并触发一次变更通知。
func (m *Manager) update(id int64, fn func(*Job)) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID == id {
			fn(j)
			break
		}
	}
	m.mu.Unlock()
	m.notify()
}

// notify 通知 UI 层刷新；由 UI 负责切回主线程。
func (m *Manager) notify() {
	if m.onChange != nil {
		m.onChange()
	}
}

// Jobs 返回所有任务的快照，按加入顺序排列（最早的在前）。
func (m *Manager) Jobs() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, len(m.jobs))
	for i, j := range m.jobs {
		out[i] = *j
	}
	return out
}

// Retry 重跑一条已结束的任务，按批次排到队尾。仅在主线程调用。
func (m *Manager) Retry(id int64) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		if j.Status != StatusFailed && j.Status != StatusDone {
			break
		}
		j.Status = StatusWaiting
		j.Progress = 0
		j.Err = ""
		j.URL = ""
		j.Key = ""
		j.Processed = 0
		j.Started, j.Finished = time.Time{}, time.Time{}
		break
	}
	ids := m.launchLocked()
	m.mu.Unlock()

	m.runAll(ids)
	m.notify()
}

// Remove 删除一条任务；等待开始的与已结束的可以删除，进行中的保留。
func (m *Manager) Remove(id int64) {
	m.mu.Lock()
	kept := m.jobs[:0]
	for _, j := range m.jobs {
		if j.ID == id && (j.Status == StatusHashing || j.Status == StatusUploading) {
			kept = append(kept, j)
			continue
		}
		if j.ID != id {
			kept = append(kept, j)
		}
	}
	m.jobs = kept
	m.mu.Unlock()
	m.notify()
}

// ClearFinished 清空所有已结束的任务。
func (m *Manager) ClearFinished() {
	m.mu.Lock()
	kept := m.jobs[:0]
	for _, j := range m.jobs {
		if j.Status != StatusDone && j.Status != StatusFailed {
			kept = append(kept, j)
		}
	}
	m.jobs = kept
	m.mu.Unlock()
	m.notify()
}

// Close 取消进行中的上传。退出应用前调用。
func (m *Manager) Close() { m.cancel() }

// hashFile 计算 SHA-256、MD5 与大小；S3 签名需要摘要，上报接口需要 MD5，
// 因此一趟读取同时算出两者。
func hashFile(p string) (string, string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	m := md5.New()
	n, err := io.Copy(io.MultiWriter(h, m), f)
	if err != nil {
		return "", "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), hex.EncodeToString(m.Sum(nil)), n, nil
}

// md5File 单独计算文件的 MD5；上报需要处理前原图的摘要。
func md5File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// contentType 按扩展名给出对象的 Content-Type。
func contentType(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
