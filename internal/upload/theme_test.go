package upload

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writePNG 把给定图片写成 PNG，返回路径。
func writePNG(t testing.TB, img image.Image, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
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

func TestThemeColorDominant(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	// 白底加一块明显的蓝色区域，主题色应识别为蓝色。
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := 10; y < 70; y++ {
		for x := 10; x < 70; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 90, B: 220, A: 255})
		}
	}
	got := ThemeColor(writePNG(t, img, "blue.png"))
	if got != "#285adc" {
		t.Fatalf("主题色应为 #285adc，得到 %q", got)
	}
}

func TestThemeColorAllWhite(t *testing.T) {
	// 纯白图没有可投票的颜色，应退回平均色而不是空串。
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	if got := ThemeColor(writePNG(t, img, "white.png")); got != "#ffffff" {
		t.Fatalf("纯白图应返回 #ffffff，得到 %q", got)
	}
}

func TestThemeColorTransparent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	// 透明像素不参与配色，只剩一小块绿色可识别。
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{})
		}
	}
	for y := 5; y < 20; y++ {
		for x := 5; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 160, B: 70, A: 255})
		}
	}
	if got := ThemeColor(writePNG(t, img, "green.png")); got != "#1ea046" {
		t.Fatalf("透明区域不应影响主题色，得到 %q", got)
	}
}

func TestThemeColorOctreeReduce(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	// 铺 40 种颜色，让叶子数量超过上限，触发八叉树的合并分支。
	for i := 0; i < 40; i++ {
		c := color.RGBA{R: uint8(i * 6), G: uint8(255 - i*6), B: uint8(i * 3), A: 255}
		for y := 0; y < 400; y++ {
			for x := i * 10; x < (i+1)*10; x++ {
				img.Set(x, y, c)
			}
		}
	}
	// 中间一大块橙色才是真正的主色。
	for y := 20; y < 380; y++ {
		for x := 20; x < 380; x++ {
			img.Set(x, y, color.RGBA{R: 240, G: 120, B: 30, A: 255})
		}
	}
	got := ThemeColor(writePNG(t, img, "octree.png"))
	if len(got) != 7 || got[0] != '#' {
		t.Fatalf("主题色格式不对：%q", got)
	}
	r, err1 := strconv.ParseUint(got[1:3], 16, 8)
	b, err3 := strconv.ParseUint(got[5:7], 16, 8)
	if err1 != nil || err3 != nil {
		t.Fatalf("解析主题色失败：%q", got)
	}
	if r <= b {
		t.Fatalf("主色应偏橙，得到 %q", got)
	}
}

func TestThemeColorNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ThemeColor(path); got != "" {
		t.Fatalf("非图片文件应返回空字符串，得到 %q", got)
	}
}

func BenchmarkThemeColor(b *testing.B) {
	// 用 1200×900 的 NRGBA 模拟常见截图：渐变背景上叠几块主体色。
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1200; x++ {
			i := y*img.Stride + x*4
			img.Pix[i] = uint8(x * 255 / 1200)
			img.Pix[i+1] = uint8(y * 255 / 900)
			img.Pix[i+2] = 200
			img.Pix[i+3] = 255
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := themeColor(img); got == "" {
			b.Fatal("主题色不应为空")
		}
	}
}
