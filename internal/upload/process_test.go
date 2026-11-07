package upload

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/avif"
)

// makePNG 生成一张带透明区域的测试图，用来验证转码与白底合成。
func makePNG(t *testing.T, dir string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 4), B: 200, A: 255})
		}
	}
	// 左上角留一块透明区域。
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{})
		}
	}
	path := filepath.Join(dir, "sample.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建测试图失败：%v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("编码测试图失败：%v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭测试图失败：%v", err)
	}
	return path
}

func TestProcessKeep(t *testing.T) {
	src := makePNG(t, t.TempDir())
	got, err := ProcessFile(src, ProcessConfig{Format: "keep", Quality: 80})
	if err != nil {
		t.Fatalf("保持原格式不应报错：%v", err)
	}
	if got.Path != src || got.Temporary || got.Ext != "" {
		t.Fatalf("保持原格式应直接返回源文件，得到 %+v", got)
	}
}

func TestProcessNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ProcessFile(path, ProcessConfig{Format: "jpeg", Quality: 80})
	if err != nil {
		t.Fatalf("非图片文件不应报错：%v", err)
	}
	if got.Path != path || got.Temporary {
		t.Fatalf("非图片文件应原样返回，得到 %+v", got)
	}
}

func TestProcessJPEG(t *testing.T) {
	src := makePNG(t, t.TempDir())
	got, err := ProcessFile(src, ProcessConfig{Format: "jpeg", Quality: 85})
	if err != nil {
		t.Fatalf("转 JPEG 失败：%v", err)
	}
	defer os.Remove(got.Path)
	if !got.Temporary || got.Ext != ".jpg" || got.Size <= 0 {
		t.Fatalf("转码结果不符合预期：%+v", got)
	}
	f, err := os.Open(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("读取 JPEG 失败：%v", err)
	}
	if cfg.Width != 80 || cfg.Height != 60 {
		t.Fatalf("尺寸应保持不变，得到 %dx%d", cfg.Width, cfg.Height)
	}
}

func TestProcessAVIF(t *testing.T) {
	src := makePNG(t, t.TempDir())
	got, err := ProcessFile(src, ProcessConfig{Format: "avif", Quality: 60})
	if err != nil {
		t.Fatalf("转 AVIF 失败：%v", err)
	}
	defer os.Remove(got.Path)
	if !got.Temporary || got.Ext != ".avif" || got.Size <= 0 {
		t.Fatalf("转码结果不符合预期：%+v", got)
	}
	f, err := os.Open(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := avif.Decode(f)
	if err != nil {
		t.Fatalf("解码 AVIF 失败：%v", err)
	}
	b := img.Bounds()
	if b.Dx() != 80 || b.Dy() != 60 {
		t.Fatalf("尺寸应保持不变，得到 %dx%d", b.Dx(), b.Dy())
	}
}
