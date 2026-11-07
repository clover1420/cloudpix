// Package store 负责持久化：云厂商配置（JSON 文件）、访问密钥
// （系统钥匙串，失败时回退到本地文件）、上传历史。
//
// 所有方法都可以在主线程调用；纯 IO 不做额外协程，文件都很小。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// Profile 是一套云存储配置。凭据不在其中，见 credentials.go。
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind 是预设类型：bitiful（缤纷云 S4）或 s3（通用 S3 兼容）。
	Kind       string `json:"kind"`
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Bucket     string `json:"bucket"`
	PublicBase string `json:"publicBase"`
	Prefix     string `json:"prefix"`
	PathStyle  bool   `json:"pathStyle"`
	// LinkParams 是该接口的链接媒体处理参数（如 w=800&q=80），
	// 留空表示直接用原图链接。不同接口可以各自设置。
	LinkParams string `json:"linkParams"`
}

// Process 是上传前的图片处理参数。
type Process struct {
	// Format 是目标格式：keep（保持原格式）、jpeg、avif。
	Format string `json:"format"`
	// Quality 是压缩质量，取值 1-100。
	Quality int `json:"quality"`
}

// 目标格式常量。
const (
	FormatKeep = "keep"
	FormatJPEG = "jpeg"
	FormatAVIF = "avif"
)

// Report 是上传成功后把图片信息登记到外部接口的配置。
type Report struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	// Age 是接口里的分类标记，可留空。
	Age string `json:"age"`
}

// Config 是应用配置。
type Config struct {
	Active   string    `json:"active"`
	Profiles []Profile `json:"profiles"`
	Process  Process   `json:"process"`
	// LinkParams 是旧版的全局链接参数，只用于读取旧配置并迁移到
	// Profile.LinkParams；新配置不再写入这个字段。
	LinkParams string `json:"linkParams,omitempty"`
	Report     Report `json:"report"`
	AutoCopy   bool   `json:"autoCopy"`
	KeepName   bool   `json:"keepName"`
	// Batch 是同时上传的张数（1-10），默认 1 表示逐张上传。
	Batch   int    `json:"batch"`
	LastDir string `json:"lastDir"`
}

// Default 返回首次启动时的配置：缤纷云为主，另附一个通用 S3 模板。
func Default() Config {
	return Config{
		Active: "bitiful",
		Profiles: []Profile{
			{
				ID:       "bitiful",
				Name:     "缤纷云 S4",
				Kind:     "bitiful",
				Endpoint: "https://s3.bitiful.net",
				Region:   "cn-east-1",
				Prefix:   "images",
			},
			{
				ID:       "custom",
				Name:     "通用 S3",
				Kind:     "s3",
				Endpoint: "",
				Region:   "us-east-1",
				Prefix:   "images",
			},
		},
		Process:  Process{Format: FormatKeep, Quality: 80},
		AutoCopy: true,
		Batch:    1,
	}
}

// LoadConfig 读取配置；文件不存在时返回 Default，并保留一个损坏
// 配置的备份，避免用户下次启动直接丢配置。
func LoadConfig() (Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			return cfg, SaveConfig(cfg)
		}
		return Default(), err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		_ = os.Rename(path, path+".bak")
		return Default(), fmt.Errorf("配置无法解析，已备份为 %s.bak：%w", path, err)
	}
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = Default().Profiles
	}
	if cfg.ActiveProfile().ID == "" {
		cfg.Active = cfg.Profiles[0].ID
	}
	cfg.Process = normalizeProcess(cfg.Process)
	cfg.Batch = normalizeBatch(cfg.Batch)
	// 旧版的全局链接参数迁移到当前配置上。
	if cfg.LinkParams != "" {
		for i := range cfg.Profiles {
			if cfg.Profiles[i].ID == cfg.Active && cfg.Profiles[i].LinkParams == "" {
				cfg.Profiles[i].LinkParams = cfg.LinkParams
			}
		}
		cfg.LinkParams = ""
	}
	return cfg, nil
}

// normalizeBatch 把同时上传的张数限制在设置页提供的档位：
// 1（逐张）、2、3、5。
func normalizeBatch(n int) int {
	switch {
	case n <= 1:
		return 1
	case n <= 2:
		return 2
	case n <= 3:
		return 3
	default:
		return 5
	}
}

// normalizeProcess 补齐处理参数的缺省值，兼容旧版配置文件。
func normalizeProcess(p Process) Process {
	switch p.Format {
	case FormatJPEG, FormatAVIF:
	default:
		p.Format = FormatKeep
	}
	if p.Quality <= 0 || p.Quality > 100 {
		p.Quality = 80
	}
	return p
}

// SaveConfig 以 0600 权限写入配置文件。
func SaveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(configPath(), data)
}

// ActiveProfile 返回当前使用的配置。
func (c Config) ActiveProfile() Profile {
	for _, p := range c.Profiles {
		if p.ID == c.Active {
			return p
		}
	}
	if len(c.Profiles) > 0 {
		return c.Profiles[0]
	}
	return Profile{}
}

// Upsert 按 ID 写入或替换一套配置。
func (c *Config) Upsert(p Profile) {
	for i := range c.Profiles {
		if c.Profiles[i].ID == p.ID {
			c.Profiles[i] = p
			return
		}
	}
	c.Profiles = append(c.Profiles, p)
}

func configPath() string { return filepath.Join(dataDir(), "config.json") }

// dataDir 返回用户数据目录；拿不到时退回当前目录，保证程序仍可运行。
func dataDir() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || dir == "" {
		return "."
	}
	return dir
}

// writeFile 写文件并限制权限；Windows 上权限位不生效，但在
// macOS / Linux 上能防止同机其他用户读取。
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
