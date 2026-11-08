package upload

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVisionLabels(t *testing.T) {
	var model, auth, imageURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("请求体不是合法 JSON：%v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		model = payload.Model
		auth = r.Header.Get("Authorization")
		if len(payload.Messages) > 0 {
			for _, raw := range payload.Messages[0].Content {
				var part struct {
					Type     string `json:"type"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				}
				_ = json.Unmarshal(raw, &part)
				if part.Type == "image_url" {
					imageURL = part.ImageURL.URL
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"[\"人物\",\"二次元\",\"插画\"]"}}]}`))
	}))
	defer srv.Close()

	cfg := VisionConfig{Enabled: true, Endpoint: srv.URL, Token: "sk-test", Model: "gpt-4o-mini"}
	tags, err := cfg.Labels(context.Background(), "https://img.example.com/a.png")
	if err != nil {
		t.Fatalf("生成标签失败：%v", err)
	}
	if len(tags) != 3 || tags[0] != "人物" || tags[1] != "二次元" || tags[2] != "插画" {
		t.Fatalf("标签解析不正确：%v", tags)
	}
	if model != "gpt-4o-mini" || auth != "Bearer sk-test" {
		t.Fatalf("请求参数不正确：model=%q auth=%q", model, auth)
	}
	if imageURL != "https://img.example.com/a.png" {
		t.Fatalf("图片链接不正确：%q", imageURL)
	}
}

func TestVisionLabelsDisabled(t *testing.T) {
	tags, err := VisionConfig{}.Labels(context.Background(), "https://img.example.com/a.png")
	if err != nil || len(tags) != 0 {
		t.Fatalf("未启用时不应请求接口：tags=%v err=%v", tags, err)
	}
}

func TestParseTags(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"代码块包裹", "```json\n[\"风景\",\"摄影\"]\n```", []string{"风景", "摄影"}},
		{"带说明文字", "标签如下：[\"人物\",\"动物\"]", []string{"人物", "动物"}},
		{"逗号分隔", "人物, 美食、建筑", []string{"人物", "美食", "建筑"}},
		{"去重与去空", "[\"人物\",\" 人物 \",\"\",\"插画\"]", []string{"人物", "插画"}},
	}
	for _, tc := range cases {
		got := parseTags(tc.content)
		if len(got) != len(tc.want) {
			t.Fatalf("%s：期望 %v，得到 %v", tc.name, tc.want, got)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%s：期望 %v，得到 %v", tc.name, tc.want, got)
			}
		}
	}
}

func TestCleanTagsLimit(t *testing.T) {
	many := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		many = append(many, string(rune('a'+i)))
	}
	if got := cleanTags(many); len(got) != visionTagLimit {
		t.Fatalf("标签数量应限制为 %d，得到 %d", visionTagLimit, len(got))
	}
}

// 视觉模型不接受 AVIF 链接，标签改用本地缩图，这里验证缩图的格式与尺寸。
func TestThumbnailDataURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建测试图片失败：%v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1200))
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("写入测试图片失败：%v", err)
	}
	f.Close()

	dataURL, err := ThumbnailDataURL(path, 1024)
	if err != nil {
		t.Fatalf("生成缩略图失败：%v", err)
	}
	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("缩略图不是 JPEG data URI：%.40s", dataURL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("base64 解码失败：%v", err)
	}
	thumb, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("JPEG 解码失败：%v", err)
	}
	if b := thumb.Bounds(); b.Dx() != 1024 || b.Dy() != 614 {
		t.Fatalf("缩略图尺寸应为 1024x614，得到 %dx%d", b.Dx(), b.Dy())
	}
}
