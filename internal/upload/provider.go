// Package upload 提供对象存储上传的抽象：一个 Provider 接口，以及
// 缤纷云 S4、七牛、MinIO 等兼容 S3 协议的实现。
//
// 该包不依赖 UI，也不依赖 mygo；它是纯粹的“业务能力层”，
// 可以在命令行工具或测试里单独复用。
package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
)

// Object 是一次待上传的对象。
//
// Key 是对象在存储桶中的完整路径（例如 images/2026/11/07/ab12cd.jpg）。
// SHA256 是内容的十六进制摘要，由调用方预先算好；S3 的签名需要它。
type Object struct {
	Key         string
	Size        int64
	ContentType string
	SHA256      string
	Body        io.Reader
}

// Result 是一次上传的结果。
type Result struct {
	Key  string
	URL  string
	ETag string
}

// Provider 是一个云存储的上传器。新增厂商时实现这个接口即可，
// UI 与队列层不需要任何改动。
type Provider interface {
	// Name 返回展示用的名称，例如“缤纷云 S4 / my-bucket”。
	Name() string
	// Upload 上传对象。progress 可能为 nil；实现应当在读取 Body 的
	// 过程中报告已发送字节数（sent）与总字节数（total）。
	Upload(ctx context.Context, obj Object, progress func(sent, total int64)) (Result, error)
	// PublicURL 返回对象的公开访问地址。
	PublicURL(key string) string
}

// BuildKey 依据前缀、日期与内容摘要生成对象键。
//
//	prefix/2006/01/02/<sha 前 16 位>.jpg
//	prefix/2006/01/02/<sha 前 8 位>-原文件名.jpg   // keepName 为 true
func BuildKey(prefix, filePath, sha string, keepName bool, now time.Time) string {
	ext := strings.ToLower(path.Ext(filePath))
	base := sanitizeName(strings.TrimSuffix(path.Base(filePath), path.Ext(filePath)))

	name := sha[:min(16, len(sha))] + ext
	if keepName && base != "" {
		name = sha[:min(8, len(sha))] + "-" + base + ext
	}

	folder := now.Format("2006/01/02")
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix != "" {
		folder = prefix + "/" + folder
	}
	return folder + "/" + name
}

// sanitizeName 只保留可安全放入对象键的字符：字母、数字、汉字，
// 以及 - _ .；其余替换为 -，并折叠连续的 -。
func sanitizeName(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
			prevDash = false
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			prevDash = r == '-'
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-.")
}

// joinURL 把公开域名与对象键拼成 URL，保留键里的路径分隔符。
func joinURL(base, key string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	segments := strings.Split(strings.TrimLeft(key, "/"), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return base + "/" + strings.Join(segments, "/")
}

// ErrNotConfigured 表示当前配置还不足以发起上传。
var ErrNotConfigured = errors.New("云存储尚未配置完成")

// ConfigError 给缺失的字段一个统一的错误信息。
func ConfigError(field string) error {
	return fmt.Errorf("%w：缺少 %s", ErrNotConfigured, field)
}
