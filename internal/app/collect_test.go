package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectImages(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{
		"a.png":                           true,
		"b.JPG":                           true,
		filepath.Join("nested", "c.webp"): true,
		"note.txt":                        false,
		"raw.cr2":                         false,
	}
	for name := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := collectImages(dir)
	if len(got) != 3 {
		t.Fatalf("应收集 3 张图片，得到 %d：%v", len(got), got)
	}
	for _, path := range got {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			t.Fatal(err)
		}
		if ok, exists := files[rel]; !exists || !ok {
			t.Fatalf("收集到了非图片文件：%s", rel)
		}
	}
}
