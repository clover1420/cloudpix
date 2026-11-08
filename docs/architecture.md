# 架构与设计

本文说明 CloudPix 的分层、线程模型、上传数据流，以及后续扩展时应该把代码放在哪里。

## 1. 设计目标

- **薄界面、厚业务**：UI 只负责展示与编排，上传能力可脱离 UI 运行和测试。
- **零云厂商 SDK**：S3 协议用标准库实现，避免为每家云引入一套 SDK。
- **明确的线程边界**：所有 UI 状态只在主线程；后台协程只碰业务对象。
- **易扩展**：新增云厂商 = 实现一个 `Provider`；新增页面 = 加一个函数与导航项。

## 2. 分层与依赖方向

```
        ┌────────────────────────────────────────────┐
        │ internal/app    （MyGo UI / 主线程）          │
        │  状态、页面、回调、post、文件选择              │
        └───────────┬─────────────────────┬──────────┘
                    │                     │
          ┌─────────▼─────────┐  ┌────────▼──────────┐
          │ internal/upload   │  │ internal/store    │
          │ Provider / S3     │  │ 配置（含密钥）/ 历史│
          │ Manager / Job     │  │ (JSON 文件)        │
          └───────────────────┘  └───────────────────┘
```

依赖方向只有一条：`app → upload / store`。

- `upload` 不导入 `app`、不导入 `mygo/ui`，可以在 `go test` 里独立运行。
- `store` 导入 `mygo` 只为了一个系统能力：`mygo.App.Path(PathUserData)`；
  它同样不导入 UI。
- 跨层交互通过**接口与回调**：`upload.Manager` 不知道界面的存在，只调用
  `onChange` / `onDone`；由 `app` 决定把它们投递到主线程。

## 3. 界面层运行模型（MyGo 原生 UI）

MyGo 的 `ui` 包是**即时模式**：`ui.View(fn)` 注册的 `fn` 会在需要时被反复调用，
每一帧都从头构建元素树。由此带来两条硬性约定：

1. **状态放在 `app` 结构体里**，不要放在元素上。输入框通过指针直接绑定
   `&a.draft.Bucket` 这类字段，元素只是当帧的映射。
2. **元素没有跨帧身份**。需要保持滚动位置、输入焦点等状态时，用稳定的
   `c.Key("...")` 标记（例如 `ui.Scroll(c.Key("job-list"))`）。

事件回调（`.OnClick(fn)`）在主线程执行，但执行时机在构建帧之后。因此可以安全地
在回调里打开系统对话框（`mygo.Dialog.Open` / `Message`），不会把构建过程卡住。

线程边界：

| 线程 | 可以做什么 | 不可以做什么 |
| --- | --- | --- |
| 主线程 | 读写 `app` 字段、构建元素、调用 `mygo.Dialog` / `Clipboard` | 直接做慢 IO（会卡帧） |
| 后台协程 | 文件读写、sha256、HTTP 上传、调用 `Manager` 内部方法 | 访问任何 `ui.Element` / `ui.Context` |

后台把结果交回主线程的唯一入口：

```go
func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(fn) // 在下一个主线程 tick 执行 fn 并重绘
}
```

`upload.Manager` 的 `onChange`、`onDone` 都是在后台触发，`app` 里分别接到
`changed` 与 `done`，两者第一行都是 `a.post(...)`。详见
[internal/app/app.go](../internal/app/app.go)。

## 4. 上传数据流

1. **选择文件**：`app.pickFiles` 调 `mygo.Dialog.Open`，过滤图片扩展名，
   记住上次目录；选中后交给 `Manager.Enqueue`。
2. **入队**：`Manager` 为每个文件建一个 `Job`（状态 `queued`），启动一个协程。
3. **处理**：若设置里选了目标格式，`upload.ProcessFile` 先转码到临时文件
   （JPEG 会铺白底以处理透明，WebP / AVIF 保留透明通道，分别由
   gen2brain/webp 与 gen2brain/avif 的 WASM 编码器完成；非图片文件原样通过）。
   临时文件在任务结束后删除。转码与取主题色串行执行，避免多张同时吃满 CPU。
4. **摘要**：协程先占一个并发槽（`sem`，容量 3），状态改为 `hashing`，
   流式计算 SHA-256 与文件大小。S3 的 SigV4 需要内容摘要，所以这一步不能省。
5. **生成键**：`upload.BuildKey(prefix, path, sha, keepName, now)`，规则见第 6 节；
   路径用的是处理后文件的扩展名，文件名主干仍取原文件。
6. **上传**：`app.provider()` 用当前配置快照构造 `upload.S3`；状态改为
   `uploading`；`Object` 携带 key、大小、Content-Type、摘要与文件句柄。
7. **进度**：`progressReader` 在每次读取时回调；`Manager` 按 200ms 或
   100% 变化做节流，再触发 `onChange`。
8. **收尾**：成功写 `done` + URL；失败写 `failed` + 可读错误。成功时 `onDone`
   交给 `app.done`，由主线程写历史、复制链接并提示。若设置里填了链接处理参数，
   会拼到 URL 末尾（缤纷云 CoreIX 的 `w` / `q` / `fmt` 等），原图不受影响。

## 5. 任务状态机

```
queued ──► hashing ──► uploading ──► done
   │          │            │
   └──────────┴────────────┴────► failed ──(Retry)──► queued
```

- `Retry` 只能对 `done` / `failed` 使用；会重置进度、错误与 URL 后重新排队。
- `Remove` 只删除已结束的任务，避免把正在上传的任务从列表里抽走。
- `ClearFinished` 保留 `queued/hashing/uploading`，清掉其余。
- 所有状态修改都通过 `Manager.update`（持锁 → 改 → 解锁 → 通知）。

## 6. 对象键规则

```
images/2026/11/07/<sha256 前 16 位>.png              // 默认
images/2026/11/07/<sha256 前 8 位>-原文件名.png       // 开启“保留原文件名”
```

- 日期目录让桶内文件自然分层，便于 CDN 缓存与生命周期策略。
- 摘要前缀保证同名文件不会互相覆盖（内容相同则键相同，天然去重）。
- 开启图片处理后，扩展名换成本次输出的格式（`.jpg` / `.webp` / `.avif`），其余规则不变。
- 文件名做了净化：只保留字母、数字、汉字与 `-_.`，其余折叠为 `-`。
- 前缀可在设置页修改，留空则直接使用日期目录。

## 7. 配置与历史

| 数据 | 位置 | 说明 |
| --- | --- | --- |
| 配置 | `PathUserData/config.json` | JSON，0600；含各配置的 AccessKey / SecretKey；损坏时备份为 `.bak` 并回退默认 |
| 历史 | `PathUserData/history.json` | 最多 300 条，最新的在前；只记录上传成功的对象 |

`PathUserData` 由 MyGo 依据 `mygo.json` 的 `name` / `identifier` 决定，不同系统位置不同。

`app.state` 是一份 `atomic.Pointer[providerState]` 配置快照。主线程保存设置时整体替换，
后台上传协程只读快照，避免共享可变结构体产生数据竞争。

> Windows 的文件权限位不生效；`0600` 只在 macOS / Linux 上提供实际保护。
> 桌面应用在本机保存 SK 是可接受的，但**不要把这种模式搬到 Web 前端**，见
> [providers.md](providers.md#6-安全建议)。

## 8. S3 签名要点

`internal/upload/s3.go` 自行实现 AWS Signature Version 4：

- 只签 `host`、`x-amz-content-sha256`、`x-amz-date` 三个头，这是 S3 PUT 的最小集合。
- Canonical URI 用 `req.URL.EscapedPath()`；对象键只包含安全字符，避免二次编码差异。
- `Content-Type` 只作为请求头发送，不参与签名。
- 签名串：`AWS4-HMAC-SHA256 Credential=<AK>/<date>/<region>/s3/aws4_request,
  SignedHeaders=..., Signature=...`。
- `Region` 必须与服务的配置一致。缤纷云为 `cn-east-1`；多数 S3 兼容服务会忽略
  该值，但仍建议填写正确值。

上传使用一次 HTTP `PUT`，带 `Content-Length`，不依赖分片上传。超大文件（>5GB）
或需要断点续传时，应扩展为 multipart 上传（见 [developing.md](developing.md#7-可扩展方向)）。

## 9. 错误处理

- 配置缺失（Bucket / Endpoint / AK / SK）在 `NewS3` 阶段就返回
  `upload.ConfigError`，错误文案可以直接展示给用户。
- 非 2xx 响应包装成 `upload.HTTPError`，包含状态码与服务端返回的前 4KB 正文，
  便于定位 `SignatureDoesNotMatch` 等问题。
- 失败不自动重试（避免反复计费与覆盖）；由用户在界面上点“重试”。
- 后台协程不 panic；所有错误都写进 `Job.Err`，界面按失败状态展示。

## 10. 扩展点一览

| 想做的事 | 改哪里 |
| --- | --- |
| 新增 S3 兼容厂商 | 只改配置（`Kind = s3` + Endpoint/Region/PathStyle），无需代码 |
| 新增非 S3 厂商 | 在 `internal/upload` 实现 `Provider`，并在 `app.provider()` 分支构造 |
| 调整并发 | `internal/upload/manager.go` 的 `concurrency` 常量 |
| 修改对象键 | `upload.BuildKey` |
| 增加页面 | `internal/app` 新文件 + `view.go` 的导航与 `content` 分支 |
| 拖拽上传 | 在 `page_upload.go` 的拖放区接入 MyGo 的拖放能力，入队入口不变 |
| 上传前压缩 | 在 `Manager.run` 的摘要与上传之间插入压缩步骤（见开发指南） |
| 预签名直传 | 新增 `Provider.Presign`，桌面端一般不需要 |

下一篇：[云厂商配置与排错](providers.md) · [开发指南](developing.md)
