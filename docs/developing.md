# 开发指南

## 1. 环境与命令

- Go 1.21+（本项目使用标准库的 `min` 内建函数与 `atomic.Pointer`）
- MyGo `v0.7.4`，随 `go.mod` 自动下载
- 无 cgo、无 Node.js、无 WebView 依赖

```powershell
go mod tidy
go run .                 # 开发运行
go vet ./...             # 静态检查
go test ./...            # 单元测试
go build -o build/CloudPix.exe .
```

首次运行会在用户数据目录生成 `config.json`，访问密钥随配置一起保存在该文件
（0600）。想重置配置，删除对应的 `config.json` 即可。

## 2. 代码约定

1. **依赖方向**：`app → upload / store`。新增代码不要让 `upload`、`store` 反向导入
   `app`，也不要让业务包导入 `mygo/ui`。
2. **线程**：后台协程只处理业务对象；任何 UI 变更都通过
   `app.post` / `Window.Update` 回到主线程。参考
   [architecture.md](architecture.md#3-界面层运行模型mygo-原生-ui)。
3. **配置快照**：后台需要的配置放在 `app.state`（`atomic.Pointer`），主线程整体替换，
   不要直接读 `app.cfg`。
4. **错误**：业务层返回带上下文的错误（`upload.ConfigError`、`upload.HTTPError`），
   由 UI 决定如何展示；不要在业务包里弹对话框。
5. **中文注释**：本项目注释与文案统一使用中文。

## 3. 新增一个非 S3 云厂商

如果厂商兼容 S3，先尝试只改配置（见 [providers.md](providers.md)）。不兼容时，
实现一个新的 `Provider`。

### 3.1 实现接口

```go
// internal/upload/acme.go
package upload

import (
	"context"
	"fmt"
	"net/http"
)

type AcmeConfig struct {
	Name      string
	APIBase   string
	Token     string
	PublicBase string
}

type Acme struct {
	cfg    AcmeConfig
	client *http.Client
}

func NewAcme(cfg AcmeConfig) (*Acme, error) {
	if cfg.APIBase == "" || cfg.Token == "" {
		return nil, ConfigError("APIBase / Token")
	}
	return &Acme{cfg: cfg, client: &http.Client{}}, nil
}

func (a *Acme) Name() string { return a.cfg.Name }

func (a *Acme) PublicURL(key string) string {
	return joinURL(a.cfg.PublicBase, key)
}

func (a *Acme) Upload(ctx context.Context, obj Object, progress func(sent, total int64)) (Result, error) {
	// 1. 用厂商 API 上传 obj.Body；需要进度时用 progressReader 包一层：
	//    body := &progressReader{r: obj.Body, total: obj.Size, fn: progress}
	// 2. 返回对象键与公开地址；服务端改了键时以服务端返回为准
	_ = progress
	return Result{}, fmt.Errorf("尚未实现：%s", a.cfg.Name)
}
```

要点：

- `Upload` 必须尊重 `ctx`（取消上传时能及时返回）。
- 有进度需求就复用 `progressReader`（`s3.go` 内已实现）。
- `PublicURL` 只做字符串拼接，不能返回空；UI 会直接展示它。

### 3.2 接入应用

1. `store.Profile` 的 `Kind` 增加新值（例如 `acme`），并在 `page_settings.go` 的
   厂商类型分段控件（`segmented(...)`）里增加该选项。
2. 在 `app.provider()` 里分支构造：

```go
func (a *app) provider() (upload.Provider, error) {
	st := a.state.Load()
	p := st.prof
	switch p.Kind {
	case "acme":
		return upload.NewAcme(upload.AcmeConfig{
			Name:       p.Name,
			APIBase:    p.Endpoint,
			Token:      p.SecretKey,
			PublicBase: p.PublicBase,
		})
	default:
		return upload.NewS3(upload.S3Config{ /* 现有代码 */ })
	}
}
```

3. 如果新厂商不需要 Region / PathStyle，可在 `applyPreset` 里隐藏或补默认值；
   设置页是分组行式布局（`settingsGroup` / `settingsRow`），新增字段时把新的
   `Profile` 字段绑定到 `settingsRow` 右侧的 `ui.TextInput` / `ui.Switch` 即可，
   配置会自动写入 `config.json`。

## 4. 新增一个页面

以“关于”页为例：

1. 在 `internal/app` 新建 `page_about.go`，实现
   `func (a *app) pageAboutView(c *ui.Context, p palette)`，内部用 `ui.*` 与
   `a.heading` / `settingsGroup` 等组件组装界面。
2. 在 `app.go` 的 page 常量里加 `pageAbout = 3`。
3. 在 `view.go` 的 `sidebar` 里加：
   `a.navItem(c, p, "nav-about", pageAbout, "关于", iconCheck)`。
4. 在 `view.go` 的 `content` 分支里加 `case pageAbout: a.pageAboutView(c, p)`。

页面函数每帧都会被调用，不要在函数里做网络或文件 IO；需要数据时提前放进 `app`
字段（主线程）或从 `Manager` 的快照读取。

## 5. 增加“上传前压缩”

推荐把图片处理放在 `upload` 层，保持 UI 无感：

1. 在 `Manager.run` 里，摘要计算之后、打开文件之前，判断扩展名与大小。
2. 用标准库 `image/jpeg` 解码并按长边缩放（`image/draw` 或邻域采样），
   输出到临时文件或 `bytes.Buffer`。
3. 用处理后的字节重新算 SHA-256，`Object.Body` 指向新数据，
   `ContentType` 改为 `image/jpeg`，对象键扩展名同步改为 `.jpg`。
4. 临时文件用 `defer os.Remove` 清理。

注意：压缩会改变内容摘要，因此**必须**在压缩之后再计算 SHA-256，否则签名失败。

## 6. 增加“拖拽上传”

上传入口已经收敛为 `Manager.Enqueue([]string)`，接入拖放只需要：

1. 在 `page_upload.go` 的 `dropZone` 上挂接 MyGo 的拖放事件（查阅所使用的 MyGo
   版本 `ui` 包的 drag & drop API）。
2. 在事件回调里过滤图片扩展名，然后调用 `a.mgr.Enqueue(paths)`。
3. 不要自己在回调里读文件或上传，队列已经做了这些事。

## 7. 可扩展方向

| 功能 | 建议实现位置 | 说明 |
| --- | --- | --- |
| 并发数可配置 | `Manager` + 设置页 | 把 `concurrency` 换成可原子的信号量容量 |
| 自动重试 | `Manager.run` | 失败后用 `time.After` 重试 N 次，注意区分 4xx（不重试）与网络错误 |
| multipart 上传 | `upload.S3` | 大文件分片、断点续传、并发分片 |
| 预签名直传 | `upload.S3` | 生成 presigned PUT URL，适合 Web 端 |
| 粘贴图片 | `app` | 读取剪贴板位图并写入临时文件，再入队 |
| 系统通知 | `app.done` | MyGo `Notify`；注意控制频率 |
| 上传统计 | `store` | 按日期累计数量与字节数 |

## 8. 测试

- `internal/upload/s3_test.go` 展示了如何用 `httptest` 模拟 S3 服务，
  验证方法、路径、签名头、请求体与返回值。
- 新增 `Provider` 时至少覆盖：配置校验、成功路径、HTTP 错误路径。
- 队列测试用轮询 `Manager.Jobs()` 等待终态，避免 `time.Sleep` 固定长等待。
- UI 层不做自动测试，改动后运行 `go run .` 手工走一遍：
  选图 → 上传 → 复制 → 历史 → 清空。

## 9. 打包发布

简单构建：

```powershell
go build -ldflags="-s -w" -o build/CloudPix.exe .
```

使用 MyGo 工具链（会生成带应用元信息的三平台产物）：

```powershell
go install github.com/egoist/mygo/cmd/mygo@latest
mygo build
```

`mygo.json` 控制名称、标识与版本：

```json
{
  "name": "CloudPix",
  "identifier": "dev.clover.cloudpix",
  "slug": "cloudpix",
  "version": "0.1.0",
  "out": "build"
}
```

修改 `name` / `identifier` 会改变 `PathUserData` 的位置，升级前注意迁移配置与密钥。

## 10. 常见问题

| 问题 | 原因 | 解决 |
| --- | --- | --- |
| 界面点按钮没反应，日志无输出 | 后台协程直接改了 `app` 字段或元素 | 所有回主线程的更新走 `a.post` |
| 输入框每帧丢焦点 | 元素没有稳定 `c.Key`，或字段被每帧重置 | 用 `c.Key("...")`；绑定字段而不是临时变量 |
| 中文显示为方框 | 系统没有主题指定字体 | `app.fontFamily()` 已按平台选择；Linux 需安装 Noto Sans CJK |
| 配置写在奇怪的位置 | `mygo.json` 的 `name`/`identifier` 变了 | 按新路径迁移 `config.json` 与 `history.json` |
| `go test` 通过但真机上传 403 | 本机时间偏差或 Region 错 | 见 [providers.md](providers.md#5-排错表) |
