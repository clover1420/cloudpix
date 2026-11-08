package upload

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendReport(t *testing.T) {
	var got reportPayload
	var raw []byte
	var token, contentType, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path = r.URL.Path
		token = r.Header.Get("X-Api-Token")
		contentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		raw = body
		if err := json.Unmarshal(body, &got); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"msg":"ok"}`))
	}))
	defer srv.Close()

	img := ReportImage{
		Title:  "测试图",
		URL:    "https://img.example.com/a.png",
		W:      1920,
		H:      1080,
		SizeMB: 1.25,
		MD5:    "0123456789abcdef0123456789abcdef",
		Theme:  "#285adc",
		Tags:   []string{"人物", "风景"},
	}
	cfg := ReportConfig{Enabled: true, Endpoint: srv.URL, Token: "secret", CategoryID: 2}
	if err := SendReport(cfg, img); err != nil {
		t.Fatalf("上报失败：%v", err)
	}
	if token != "secret" || contentType != "application/json" || path != "/api/v1/img/up" {
		t.Fatalf("请求头或路径不正确：token=%q content-type=%q path=%q", token, contentType, path)
	}
	if got.URL != img.URL || got.Width != 1920 || got.Height != 1080 ||
		got.Size != 1.25 || got.MD5 != img.MD5 || got.ThemeColor != img.Theme {
		t.Fatalf("请求体不正确：%+v", got)
	}
	if got.CategoryID == nil || *got.CategoryID != 2 {
		t.Fatalf("category_id 不正确：%v", got.CategoryID)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "人物" || got.Tags[1] != "风景" {
		t.Fatalf("tags 字段不正确：%v", got.Tags)
	}

	// 字段名必须与接口约定一致，且不再发送旧版的 id / time / age / w / h。
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("解析请求体失败：%v", err)
	}
	if fields["theme_color"] != "#285adc" || fields["category_id"] != float64(2) {
		t.Fatalf("theme_color 或 category_id 字段不对：%v", fields)
	}
	for _, key := range []string{"id", "time", "age", "w", "h"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("请求体不应包含旧字段 %s：%s", key, raw)
		}
	}
}

func TestSendReportConflictIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"detail":"该图片已存在"}`))
	}))
	defer srv.Close()

	cfg := ReportConfig{Enabled: true, Endpoint: srv.URL}
	if err := SendReport(cfg, ReportImage{}); err != nil {
		t.Fatalf("409 应视为成功：%v", err)
	}
}

func TestSendReportDisabled(t *testing.T) {
	if err := SendReport(ReportConfig{Endpoint: "http://127.0.0.1:1"}, ReportImage{}); err != nil {
		t.Fatalf("未启用时不应发请求：%v", err)
	}
}
