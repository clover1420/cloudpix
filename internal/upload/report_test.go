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
	var token, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
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
		ID:     "0123456789abcdef0123456789abcdef",
		Title:  "测试图",
		URL:    "https://img.example.com/a.png",
		W:      1920,
		H:      1080,
		Age:    "p站",
		SizeMB: 1.25,
		MD5:    "0123456789abcdef0123456789abcdef",
		Time:   "2026-10-25 12:00:00",
		Theme:  "#285adc",
	}
	cfg := ReportConfig{Enabled: true, Endpoint: srv.URL, Token: "secret"}
	if err := SendReport(cfg, img); err != nil {
		t.Fatalf("上报失败：%v", err)
	}
	if token != "secret" || contentType != "application/json" {
		t.Fatalf("请求头不正确：token=%q content-type=%q", token, contentType)
	}
	if got.ID != img.ID || got.URL != img.URL || got.W != 1920 || got.H != 1080 ||
		got.Age != "p站" || got.Size != 1.25 || got.MD5 != img.MD5 || got.Time != img.Time ||
		got.Color != img.Theme {
		t.Fatalf("请求体不正确：%+v", got)
	}
	// 字段名必须与接口约定一致：theme_color。
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("解析请求体失败：%v", err)
	}
	if fields["theme_color"] != "#285adc" {
		t.Fatalf("theme_color 字段缺失或取值不对：%v", fields)
	}
}

func TestSendReportConflictIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"detail":"该图片已存在"}`))
	}))
	defer srv.Close()

	cfg := ReportConfig{Enabled: true, Endpoint: srv.URL}
	if err := SendReport(cfg, ReportImage{ID: "x"}); err != nil {
		t.Fatalf("409 应视为成功：%v", err)
	}
}

func TestSendReportDisabled(t *testing.T) {
	if err := SendReport(ReportConfig{Endpoint: "http://127.0.0.1:1"}, ReportImage{}); err != nil {
		t.Fatalf("未启用时不应发请求：%v", err)
	}
}

func TestNewReportID(t *testing.T) {
	id := NewReportID()
	if len(id) != 6 {
		t.Fatalf("短 ID 长度应为 6，得到 %d：%s", len(id), id)
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			t.Fatalf("短 ID 只能包含小写字母与数字，得到 %s", id)
		}
	}
	if again := NewReportID(); again == id {
		t.Fatalf("两次生成的短 ID 不应相同：%s", id)
	}
}
