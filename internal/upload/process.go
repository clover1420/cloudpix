package upload

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gen2brain/avif"
	webpenc "github.com/gen2brain/webp"
	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/webp"
)

// 处理参数的目标格式。
const (
	processKeep = "keep"
	processJPEG = "jpeg"
	processAVIF = "avif"
	processWebP = "webp"
)

// ProcessConfig 是上传前图片处理参数。
type ProcessConfig struct {
	// Format 是目标格式：keep / jpeg / webp / avif。空值等同 keep。
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
	if format != processJPEG && format != processAVIF && format != processWebP {
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
	switch format {
	case processAVIF:
		ext = ".avif"
	case processWebP:
		ext = ".webp"
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
//
// 配色改用八叉树颜色量化，移植自 xiong35/color-extraction 的 src/octree.ts：
// 每个像素按 R/G/B 的二进制位逐层插入八叉树，叶子数超过上限时合并最深层
// 节点，最后按叶子累计的像素数排序取主色。透明像素不参与统计；接近纯白、
// 纯黑的大块背景会先跳过，避免背景色压过画面主体，全图都是中性色时再退回
// 像素最多的颜色。
func ThemeColor(path string) string {
	img, err := decodeImage(path)
	if err != nil {
		return ""
	}
	return themeColor(img)
}

// themeColor 对已解码的图片取主题色。常见解码格式直接按字节遍历，
// 绕开 image.Image 的接口调用和 RGBA() 的 16 位预乘换算。
func themeColor(img image.Image) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}
	step := 1
	if m := max(w, h); m > 160 {
		step = (m + 159) / 160
	}

	t := &octree{root: &octreeNode{}, maxLeaf: 16}
	total := 0

	switch src := img.(type) {
	case *image.NRGBA:
		// NRGBA 的 Pix 已经是非预乘的 8 位真色，可直接读。
		rect, stride, pix := src.Rect, src.Stride, src.Pix
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			row := (y-rect.Min.Y)*stride - rect.Min.X*4
			for x := rect.Min.X; x < rect.Max.X; x += step {
				i := row + x*4
				if pix[i+3] < 0x80 {
					continue // 透明像素不参与配色
				}
				t.add(int(pix[i]), int(pix[i+1]), int(pix[i+2]))
				total++
			}
		}
	case *image.RGBA:
		// RGBA 的 Pix 是预乘值，半透明像素要先还原成真色。
		rect, stride, pix := src.Rect, src.Stride, src.Pix
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			row := (y-rect.Min.Y)*stride - rect.Min.X*4
			for x := rect.Min.X; x < rect.Max.X; x += step {
				i := row + x*4
				a := int(pix[i+3])
				if a < 0x80 {
					continue
				}
				r, g, bl := int(pix[i]), int(pix[i+1]), int(pix[i+2])
				if a != 0xff {
					r, g, bl = min(255, r*255/a), min(255, g*255/a), min(255, bl*255/a)
				}
				t.add(r, g, bl)
				total++
			}
		}
	case *image.Gray:
		rect, stride, pix := src.Rect, src.Stride, src.Pix
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			row := (y-rect.Min.Y)*stride - rect.Min.X
			for x := rect.Min.X; x < rect.Max.X; x += step {
				v := int(pix[row+x])
				t.add(v, v, v)
				total++
			}
		}
	case *image.YCbCr:
		// JPEG 多解码成 YCbCr，直接做一次颜色转换，省掉接口与预乘开销。
		rect := src.Rect
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			for x := rect.Min.X; x < rect.Max.X; x += step {
				ci := src.COffset(x, y)
				r, g, bl := color.YCbCrToRGB(src.Y[src.YOffset(x, y)], src.Cb[ci], src.Cr[ci])
				t.add(int(r), int(g), int(bl))
				total++
			}
		}
	default:
		// 其余格式（GIF 调色板、CMYK 等）走通用路径。
		for y := b.Min.Y; y < b.Max.Y; y += step {
			for x := b.Min.X; x < b.Max.X; x += step {
				r, g, bl, a := img.At(x, y).RGBA()
				if a < 0x8000 {
					continue
				}
				// RGBA() 返回预乘值，先还原成 8 位真色。
				cr := int(r * 0xffff / a >> 8)
				cg := int(g * 0xffff / a >> 8)
				cb := int(bl * 0xffff / a >> 8)
				t.add(cr, cg, cb)
				total++
			}
		}
	}
	if total == 0 {
		return ""
	}
	return t.theme()
}

// octreeNode 是八叉树量化节点；叶子累计颜色之和与像素数。
type octreeNode struct {
	children [8]*octreeNode
	leaf     bool
	r, g, b  int
	count    int
}

// octree 保存根节点、叶子数量以及第 0-6 层可合并的非叶节点。
type octree struct {
	root     *octreeNode
	leafNum  int
	maxLeaf  int
	toReduce [7][]*octreeNode
}

// add 把一个 8 位真色插入八叉树；插入后叶子超限就合并最深层节点。
// 循环展开代替递归，省掉每像素 8 层函数调用的开销。
func (t *octree) add(r, g, b int) {
	n := t.root
	for level := 0; ; level++ {
		if n.leaf {
			n.count++
			n.r += r
			n.g += g
			n.b += b
			break
		}
		shift := uint(7 - level)
		idx := (r>>shift)&1<<2 | (g>>shift)&1<<1 | (b>>shift)&1
		child := n.children[idx]
		if child == nil {
			child = &octreeNode{}
			n.children[idx] = child
			if level == 6 {
				child.leaf = true
				t.leafNum++
			} else {
				t.toReduce[level+1] = append(t.toReduce[level+1], child)
			}
		}
		n = child
	}
	// 叶子超过上限时合并最深层节点，与参考实现一致。
	for t.leafNum > t.maxLeaf {
		t.reduce()
	}
}

// reduce 从最深的非空层取出一个节点合并成叶子；它的子节点此时都已带统计值。
func (t *octree) reduce() {
	level := 6
	for level >= 0 && len(t.toReduce[level]) == 0 {
		level--
	}
	if level < 0 {
		return
	}
	list := t.toReduce[level]
	n := list[len(list)-1]
	t.toReduce[level] = list[:len(list)-1]

	n.leaf = true
	n.r, n.g, n.b, n.count = 0, 0, 0, 0
	for _, c := range n.children {
		if c == nil {
			continue
		}
		n.r += c.r
		n.g += c.g
		n.b += c.b
		n.count += c.count
		t.leafNum--
	}
	t.leafNum++
}

// theme 返回像素数最多的非中性色；没有非中性色时返回最多的颜色。
func (t *octree) theme() string {
	type entry struct{ r, g, b, n int }
	var palette []entry
	var walk func(n *octreeNode)
	walk = func(n *octreeNode) {
		if n.leaf {
			if n.count > 0 {
				palette = append(palette, entry{n.r / n.count, n.g / n.count, n.b / n.count, n.count})
			}
			return
		}
		for _, c := range n.children {
			if c != nil {
				walk(c)
			}
		}
	}
	walk(t.root)
	if len(palette) == 0 {
		return ""
	}
	sort.SliceStable(palette, func(i, j int) bool { return palette[i].n > palette[j].n })
	for _, e := range palette {
		// 近白、近黑通常是背景，跳过。
		if (e.r >= 244 && e.g >= 244 && e.b >= 244) || (e.r <= 12 && e.g <= 12 && e.b <= 12) {
			continue
		}
		return fmt.Sprintf("#%02x%02x%02x", e.r, e.g, e.b)
	}
	e := palette[0]
	return fmt.Sprintf("#%02x%02x%02x", e.r, e.g, e.b)
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
	switch format {
	case processAVIF:
		return avif.Encode(out, img, avif.Options{
			Quality: quality,
			Speed:   8,
		})
	case processWebP:
		// WebP 支持透明通道，直接保留原图。Method 0 是最快档，
		// 只影响压缩耗时与压缩率，不改变视觉质量。
		return webpenc.Encode(out, img, webpenc.Options{Quality: quality, Method: 0})
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
