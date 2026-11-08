package upload

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"
)

// VisionConfig 是调用视觉模型生成标签的配置，接口需兼容 OpenAI 的
// chat/completions；图片以公开链接或 base64 data URI 传入。
type VisionConfig struct {
	Enabled  bool
	Endpoint string
	Token    string
	Model    string
}

// visionPrompt 要求模型只输出 JSON 数组，便于解析。
const visionPrompt = "请根据图片内容，用简体中文给出 3 到 5 个简短标签（例如：人物、风景、二次元、动物、美食、建筑、插画、摄影）。只输出 JSON 字符串数组，不要输出其他文字。"

// 与服务端 tags 约束保持一致：最多 20 个，单个不超过 32 字符。
const (
	visionTagLimit     = 20
	visionTagMaxLength = 32
)

// Labels 让视觉模型根据公开图片链接生成内容标签。
// 未启用或未配置接口时返回空，不视为错误。
func (c VisionConfig) Labels(ctx context.Context, imageURL string) ([]string, error) {
	url := strings.TrimSpace(c.Endpoint)
	model := strings.TrimSpace(c.Model)
	if !c.Enabled || url == "" || model == "" || imageURL == "" {
		return nil, nil
	}

	payload := map[string]any{
		"model":       model,
		"temperature": 0,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": visionPrompt},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化标签请求失败：%w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("标签接口地址无效：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("标签请求失败：%w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取标签响应失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("标签接口返回 %d：%s", resp.StatusCode, truncateText(string(data), 200))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析标签响应失败：%w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("标签响应为空")
	}
	return parseTags(out.Choices[0].Message.Content), nil
}

// parseTags 从模型输出里取出标签：优先按 JSON 数组解析，失败时按
// 逗号、顿号、分号或换行拆分，兼容模型不守格式的情况。
func parseTags(content string) []string {
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if start := strings.Index(s, "["); start >= 0 {
		if end := strings.LastIndex(s, "]"); end > start {
			var tags []string
			if json.Unmarshal([]byte(s[start:end+1]), &tags) == nil {
				return cleanTags(tags)
			}
		}
	}
	return cleanTags(strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', '\n', '\r':
			return true
		}
		return false
	}))
}

// cleanTags 去掉空值、重复与超长标签，并限制数量。
func cleanTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.Trim(strings.TrimSpace(raw), `"'“”`+"`")
		if tag == "" || len([]rune(tag)) > visionTagMaxLength {
			continue
		}
		dup := false
		for _, t := range out {
			if t == tag {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, tag)
		if len(out) >= visionTagLimit {
			break
		}
	}
	return out
}

// ThumbnailDataURL 把本地图片缩成最长边 maxSide 的 JPEG，返回 base64
// data URI。视觉模型不一定能解析 AVIF 等链接，本地缩略图更可靠。
func ThumbnailDataURL(path string, maxSide int) (string, error) {
	if maxSide <= 0 {
		maxSide = 1024
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开图片失败：%w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("解码图片失败：%w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("图片尺寸无效")
	}
	dw, dh := w, h
	if w > maxSide || h > maxSide {
		if w >= h {
			dw = maxSide
			dh = int(float64(h) * float64(maxSide) / float64(w))
		} else {
			dh = maxSide
			dw = int(float64(w) * float64(maxSide) / float64(h))
		}
	}
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return "", fmt.Errorf("编码缩略图失败：%w", err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// truncateText 按字符截断，避免把整段响应塞进错误提示。
func truncateText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
