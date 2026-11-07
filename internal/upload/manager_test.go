package upload

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeProvider 记录上传次数与并发峰值，便于验证分批上传。
type fakeProvider struct {
	mu      sync.Mutex
	running int
	peak    int
	calls   int
	delay   time.Duration
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) PublicURL(key string) string { return "https://example.com/" + key }

func (f *fakeProvider) Upload(_ context.Context, obj Object, progress func(sent, total int64)) (Result, error) {
	f.mu.Lock()
	f.calls++
	f.running++
	if f.running > f.peak {
		f.peak = f.running
	}
	f.mu.Unlock()

	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	f.mu.Lock()
	f.running--
	f.mu.Unlock()

	if _, err := io.Copy(io.Discard, obj.Body); err != nil {
		return Result{}, err
	}
	if progress != nil {
		progress(obj.Size, obj.Size)
	}
	return Result{Key: obj.Key, URL: f.PublicURL(obj.Key)}, nil
}

func (f *fakeProvider) stats() (calls, peak int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.peak
}

func newTestManager(p *fakeProvider) *Manager {
	return NewManager(func() (Provider, error) { return p, nil }, nil, nil)
}

func writeFiles(t *testing.T, n int) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, "img"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func waitDone(t *testing.T, m *Manager, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		jobs := m.Jobs()
		done := 0
		for _, j := range jobs {
			if j.Status == StatusDone || j.Status == StatusFailed {
				done++
			}
		}
		if done == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("任务未在预期时间内结束：%+v", m.Jobs())
}

// TestStartRequiredForUpload 验证入队后必须点“开始上传”才真正上传。
func TestStartRequiredForUpload(t *testing.T) {
	paths := writeFiles(t, 1)
	p := &fakeProvider{}
	m := newTestManager(p)

	if n := m.Enqueue(paths); n != 1 {
		t.Fatalf("应加入 1 个任务，得到 %d", n)
	}
	if st := m.Jobs()[0].Status; st != StatusQueued {
		t.Fatalf("入队后状态应为 queued，得到 %s", st)
	}
	time.Sleep(50 * time.Millisecond)
	if calls, _ := p.stats(); calls != 0 {
		t.Fatalf("未开始上传就调用了上传器 %d 次", calls)
	}

	if n := m.Start(); n != 1 {
		t.Fatalf("应开始 1 个任务，得到 %d", n)
	}
	waitDone(t, m, 1)
}

// TestBatchUploadSerially 验证默认逐张上传：同时只有一个请求。
func TestBatchUploadSerially(t *testing.T) {
	paths := writeFiles(t, 3)
	p := &fakeProvider{delay: 30 * time.Millisecond}
	m := newTestManager(p)
	m.SetBatch(1)

	if n := m.Enqueue(paths); n != 3 {
		t.Fatalf("应加入 3 个任务，得到 %d", n)
	}
	m.Start()
	waitDone(t, m, 3)

	calls, peak := p.stats()
	if calls != 3 {
		t.Fatalf("应上传 3 次，得到 %d", calls)
	}
	if peak != 1 {
		t.Fatalf("逐张上传的并发峰值应为 1，得到 %d", peak)
	}
}

// TestRemoveQueued 验证待上传的任务可以直接移除。
func TestRemoveQueued(t *testing.T) {
	paths := writeFiles(t, 1)
	p := &fakeProvider{}
	m := newTestManager(p)
	m.Enqueue(paths)
	id := m.Jobs()[0].ID
	m.Remove(id)
	if jobs := m.Jobs(); len(jobs) != 0 {
		t.Fatalf("待上传任务应可移除，仍剩 %d 个", len(jobs))
	}
}
