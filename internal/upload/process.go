package upload

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"

	"github.com/gen2brain/avif"
	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/webp"
)

// 处理参数的目标格式。
const (
	processKeep = "keep"
	processJPEG = "jpeg"
	processAVIF = "avif"
)

// ProcessConfig 是上传前图片处理参数。
type ProcessConfig struct {
	// Format 是目标格式：keep / jpeg / avif。空值等同 keep。
	Format string
	// Quality 是压缩质量，1-100；超出范围时按 80 处理。
	Quality int
}

// Processed 是处理结果。Temporary 为 true 时调用方需要删除 Path；
// Ext 是输出格式的扩展名（含点），未处理时为空。
type Processed struct {
	Path      string
	Size      int64
	Ext       string
	Temporary bool
}

// ProcessFile 按参数把 src 转码为临时文件。保持原格式、或源文件无法
// 解码（不是图片）时原样返回 src，不阻塞上传。
func ProcessFile(src string, pc ProcessConfig) (Processed, error) {
	format := strings.ToLower(strings.TrimSpace(pc.Format))
	if format == "" {
		format = processKeep
	}
	if format != processJPEG && format != processAVIF {
		return Processed{Path: src}, nil
	}

	img, err := decodeImage(src)
	if err != nil {
		return Processed{Path: src}, nil
	}

	quality := pc.Quality
	if quality <= 0 || quality > 100 {
		quality = 80
	}

	ext := ".jpg"
	if format == processAVIF {
		ext = ".avif"
	}
	out, err := os.CreateTemp("", "cloudpix-*"+ext)
	if err != nil {
		return Processed{}, fmt.Errorf("创建临时文件失败：%w", err)
	}
	name := out.Name()

	if err := encodeImage(out, img, format, quality); err != nil {
		out.Close()
		_ = os.Remove(name)
		return Processed{}, fmt.Errorf("转码失败：%w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(name)
		return Processed{}, fmt.Errorf("写入临时文件失败：%w", err)
	}
	st, err := os.Stat(name)
	if err != nil {
		_ = os.Remove(name)
		return Processed{}, fmt.Errorf("读取临时文件失败：%w", err)
	}
	return Processed{Path: name, Size: st.Size(), Ext: ext, Temporary: true}, nil
}

// ImageSize 只读文件头取图片宽高，用于上报图片信息。
// 非图片文件返回 ok=false。
func ImageSize(path string) (w, h int, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	var cfg image.Config
	switch strings.ToLower(filepath.Ext(path)) {
	case ".avif":
		cfg, err = avif.DecodeConfig(f)
	case ".webp":
		cfg, err = webp.DecodeConfig(f)
	default:
		cfg, _, err = image.DecodeConfig(f)
	}
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// ThemeColor 取图片的主题色，返回 #rrggbb；无法解码时返回空字符串。
// 做法是抽样做颜色分桶投票：透明像素、接近纯白与纯黑的像素不参与投票，
// 避免大块背景把主题色带偏；全图都是背景色时退回整体平均色。
func ThemeColor(path string) string {
	img, err := decodeImage(path)
	if err != nil {
		return ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}
	step := 1
	if m := max(w, h); m > 160 {
		step = (m + 159) / 160
	}

	type bucket struct{ r, g, b, n int }
	buckets := make(map[int]*bucket, 512)
	var sumR, sumG, sumB, sumN int

	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue // 透明像素不参与配色
			}
			// RGBA() 返回预乘值，先还原成 8 位真色。
			cr := int(r * 0xffff / a >> 8)
			cg := int(g * 0xffff / a >> 8)
			cb := int(bl * 0xffff / a >> 8)
			sumR, sumG, sumB, sumN = sumR+cr, sumG+cg, sumB+cb, sumN+1

			if (cr >= 244 && cg >= 244 && cb >= 244) || (cr <= 12 && cg <= 12 && cb <= 12) {
				continue
			}
			k := (cr>>4)<<8 | (cg>>4)<<4 | cb>>4
			bk := buckets[k]
			if bk == nil {
				bk = &bucket{}
				buckets[k] = bk
			}
			bk.r, bk.g, bk.b, bk.n = bk.r+cr, bk.g+cg, bk.b+cb, bk.n+1
		}
	}

	var top *bucket
	for _, bk := range buckets {
		if top == nil || bk.n > top.n {
			top = bk
		}
	}
	if top != nil {
		return fmt.Sprintf("#%02x%02x%02x", top.r/top.n, top.g/top.n, top.b/top.n)
	}
	if sumN == 0 {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", sumR/sumN, sumG/sumN, sumB/sumN)
}

// decodeImage 按扩展名解码；非图片文件返回错误。
func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch strings.ToLower(filepath.Ext(path)) {
	case ".avif":
		return avif.Decode(f)
	case ".webp":
		return webp.Decode(f)
	}
	img, _, err := image.Decode(f)
	return img, err
}

// encodeImage 按目标格式写出。JPEG 不支持透明，先铺白底。
func encodeImage(out *os.File, img image.Image, format string, quality int) error {
	if format == processAVIF {
		return avif.Encode(out, img, avif.Options{
			Quality: quality,
			Speed:   8,
		})
	}
	return jpeg.Encode(out, flatten(img), &jpeg.Options{Quality: quality})
}

// flatten 把带透明通道的图片合成到白色背景上，避免 JPEG 透出黑色。
func flatten(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}
