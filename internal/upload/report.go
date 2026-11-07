package upload

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// idAlphabet 是短 ID 的字符集：小写字母与数字。
const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// NewReportID 生成 6 位短 ID（如 e7py7k），作为上报接口的业务 ID。
func NewReportID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵源不可用时退回时间纳秒的低 6 位，保证长度与字符集合法。
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		sb.WriteByte(idAlphabet[int(c)%len(idAlphabet)])
	}
	return sb.String()
}

// ReportConfig 是图片信息上报接口的配置。
type ReportConfig struct {
	Enabled  bool
	Endpoint string
	Token    string
	Age      string
}

// ReportImage 是一条待上报的图片信息。
type ReportImage struct {
	ID     string
	Title  string
	URL    string
	W      int
	H      int
	Age    string
	SizeMB float64
	MD5    string
	Time   string
	// Theme 是图片主题色（#rrggbb），取不到时为空字符串。
	Theme string
}

// reportPayload 是接口要求的 JSON 结构。
type reportPayload struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	URL   string  `json:"url"`
	W     int     `json:"w"`
	H     int     `json:"h"`
	Age   string  `json:"age"`
	Size  float64 `json:"size"`
	MD5   string  `json:"md5"`
	Time  string  `json:"time"`
	Color string  `json:"theme_color"`
}

// SendReport 把图片信息 POST 到配置的接口。409（已存在）视为成功，
// 便于重复上传同一张图时保持幂等。
func SendReport(cfg ReportConfig, img ReportImage) error {
	url := strings.TrimSpace(cfg.Endpoint)
	if !cfg.Enabled || url == "" {
		return nil
	}

	body, err := json.Marshal(reportPayload{
		ID:    img.ID,
		Title: img.Title,
		URL:   img.URL,
		W:     img.W,
		H:     img.H,
		Age:   img.Age,
		Size:  img.SizeMB,
		MD5:   img.MD5,
		Time:  img.Time,
		Color: img.Theme,
	})
	if err != nil {
		return fmt.Errorf("序列化上报内容失败：%w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
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
