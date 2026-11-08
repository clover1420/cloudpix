package upload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ReportConfig 是图片信息上报接口的配置。
type ReportConfig struct {
	Enabled    bool
	Endpoint   string
	Token      string
	CategoryID int
}

// ReportImage 是一条待上报的图片信息。
type ReportImage struct {
	Title  string
	URL    string
	W      int
	H      int
	SizeMB float64
	MD5    string
	// Theme 是图片主题色（#rrggbb），取不到时为空字符串。
	Theme string
	// Tags 是视觉模型生成的内容标签，未启用时为空。
	Tags []string
}

// reportPayload 是接口要求的 JSON 结构（图片新增模型）。
type reportPayload struct {
	MD5        string   `json:"md5"`
	URL        string   `json:"url"`
	Title      string   `json:"title,omitempty"`
	CategoryID *int     `json:"category_id,omitempty"`
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	ThemeColor string   `json:"theme_color,omitempty"`
	Size       float64  `json:"size,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

// baseURL 规范接口域名：去掉结尾斜杠，未写协议时补 https://。
func baseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return strings.TrimSuffix(s, "/")
}

// apiEndpoint 把接口域名与固定路径拼成完整地址。
func apiEndpoint(base, path string) string { return baseURL(base) + path }

// SendReport 把图片信息 POST 到配置的接口。409（已存在）视为成功，
// 便于重复上传同一张图时保持幂等。
func SendReport(cfg ReportConfig, img ReportImage) error {
	base := baseURL(cfg.Endpoint)
	if !cfg.Enabled || base == "" {
		return nil
	}

	payload := reportPayload{
		MD5:        img.MD5,
		URL:        img.URL,
		Title:      img.Title,
		Width:      img.W,
		Height:     img.H,
		ThemeColor: img.Theme,
		Size:       img.SizeMB,
		Tags:       img.Tags,
	}
	if cfg.CategoryID > 0 {
		id := cfg.CategoryID
		payload.CategoryID = &id
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化上报内容失败：%w", err)
	}

	req, err := http.NewRequest(http.MethodPost, apiEndpoint(base, "/api/v1/img/up"), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("上报地址无效：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Token", cfg.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("上报失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		return nil
	}

	var detail struct {
		Detail string `json:"detail"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	_ = json.Unmarshal(data, &detail)
	if detail.Detail != "" {
		return fmt.Errorf("上报被拒绝（%d）：%s", resp.StatusCode, detail.Detail)
	}
	return fmt.Errorf("上报被拒绝（HTTP %d）", resp.StatusCode)
}
