package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildKey(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

	got := BuildKey("images", "/tmp/照片 (1).PNG", sha, false, at)
	want := "images/2026/11/07/" + sha[:16] + ".png"
	if got != want {
		t.Fatalf("BuildKey() = %q, want %q", got, want)
	}

	got = BuildKey("/images/", "/tmp/照片 (1).PNG", sha, true, at)
	want = "images/2026/11/07/" + sha[:8] + "-照片-1.png"
	if got != want {
		t.Fatalf("BuildKey(keepName) = %q, want %q", got, want)
	}
}

func TestS3Upload(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("ETag", `"etag-1"`)
	}))
	defer srv.Close()

	p, err := NewS3(S3Config{
		Endpoint:  srv.URL,
		Region:    "us-east-1",
		Bucket:    "demo",
		AccessKey: "AK",
		SecretKey: "SK",
		PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("hello cloudpix")
	sum := sha256.Sum256(data)
	res, err := p.Upload(context.Background(), Object{
		Key:         "images/2026/11/07/a.jpg",
		Size:        int64(len(data)),
		ContentType: "image/jpeg",
		SHA256:      hex.EncodeToString(sum[:]),
		Body:        bytes.NewReader(data),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %s", gotMethod)
	}
	if gotPath != "/demo/images/2026/11/07/a.jpg" {
		t.Errorf("path = %s", gotPath)
	}
	if gotType != "image/jpeg" {
		t.Errorf("content-type = %s", gotType)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AK/") {
		t.Errorf("authorization = %s", gotAuth)
	}
	if !bytes.Equal(gotBody, data) {
		t.Errorf("body = %q", gotBody)
	}
	if res.URL != srv.URL+"/demo/images/2026/11/07/a.jpg" {
		t.Errorf("url = %s", res.URL)
	}
	if res.ETag != "etag-1" {
		t.Errorf("etag = %s", res.ETag)
	}
}

func TestS3RejectsMissingConfig(t *testing.T) {
	if _, err := NewS3(S3Config{Bucket: "b"}); err == nil {
		t.Fatal("expected error for missing key")
	}
	if _, err := NewS3(S3Config{Bucket: "b", AccessKey: "a", SecretKey: "s"}); err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestManagerRunsJob(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cat.png")
	if err := os.WriteFile(file, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	var uploaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploaded = r.URL.Path
	}))
	defer srv.Close()

	m := NewManager(func() (Provider, error) {
		return NewS3(S3Config{
			Endpoint:  srv.URL,
			Region:    "us-east-1",
			Bucket:    "demo",
			AccessKey: "AK",
			SecretKey: "SK",
			PathStyle: true,
		})
	}, nil, nil)
	m.Configure("images", false, ProcessConfig{Format: "keep", Quality: 80}, "")
	m.Enqueue([]string{file})
	m.Start()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs := m.Jobs()
		if len(jobs) == 1 && (jobs[0].Status == StatusDone || jobs[0].Status == StatusFailed) {
			if jobs[0].Status == StatusFailed {
				t.Fatalf("job failed: %s", jobs[0].Err)
			}
			if !strings.HasPrefix(uploaded, "/demo/images/") {
				t.Fatalf("uploaded path = %q", uploaded)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestWithLinkArgs(t *testing.T) {
	cases := []struct {
		url, args, want string
	}{
		{"https://cdn.example.com/a.jpg", "", "https://cdn.example.com/a.jpg"},
		{"https://cdn.example.com/a.jpg", "w=800&q=80", "https://cdn.example.com/a.jpg?w=800&q=80"},
		{"https://cdn.example.com/a.jpg", "?w=800", "https://cdn.example.com/a.jpg?w=800"},
		{"https://cdn.example.com/a.jpg?x=1", "w=800", "https://cdn.example.com/a.jpg?x=1&w=800"},
	}
	for _, c := range cases {
		if got := withLinkArgs(c.url, c.args); got != c.want {
			t.Fatalf("withLinkArgs(%q, %q) = %q，期望 %q", c.url, c.args, got, c.want)
		}
	}
}
