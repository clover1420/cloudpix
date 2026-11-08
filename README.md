# CloudPix

用 Go + [MyGo](https://github.com/egoist/mygo)（`mygoui` 原生 UI）实现的桌面图片上传工具。
把本地图片批量上传到**缤纷云 S4**，或任何兼容 S3 协议的对象存储（MinIO、Cloudflare R2、
Backblaze B2、AWS S3、七牛/阿里/腾讯的 S3 兼容端点等）。

当前版本可以直接运行：选择图片 → 后台上传（并发 3、带进度）→ 自动复制外链 → 本地历史记录。

## 功能

- 多选图片，后台队列上传，不阻塞界面
- 上传进度、失败原因、单条重试
- 上传成功自动复制链接（可关闭）
- 本地历史记录（复制 / 打开 / 清空）
- 多套云存储配置切换；密钥随配置保存在 `config.json`
- 上传前可选转 JPEG / WebP / AVIF；上传后可调用视觉模型自动生成标签，
  随图片信息上报到自建接口（服务端需支持 `tags` 字段）
- 对象键按日期分层：`images/2026/11/07/<sha256 前 16 位>.jpg`
- 纯 Go 实现，无 cgo；S3 SigV4 签名自行实现，无云厂商 SDK 依赖
- 界面沿用 `mygo-qyin` 项目的设计：232 宽侧栏、深浅双色板、红色强调色

## 快速开始

```powershell
# 需要 Go 1.21+
go mod tidy
go run .

# 构建可执行文件
go build -o build/CloudPix.exe .
```

启动后打开「设置」：

1. 厂商类型选「缤纷云 S4」或「通用 S3」。
2. 填写 Bucket、AccessKey、SecretKey。
3. 公开访问域名可留空；留空时用 Endpoint 拼出链接。
4. 保存设置，回到「上传」页选择图片。

缤纷云默认参数：

| 字段 | 值 |
| --- | --- |
| Endpoint | `https://s3.bitiful.net` |
| Region | `cn-east-1` |
| PathStyle | 关闭（虚拟主机风格） |

完整的厂商配置与排错见 [docs/providers.md](docs/providers.md)。

## 目录结构

```
.
├── main.go                     # 入口，仅调用 app.Run()
├── mygo.json                   # MyGo 应用元信息（名称、标识、版本）
├── internal/
│   ├── app/                    # 界面层：只在主线程读写 UI 状态
│   │   ├── app.go              # 应用状态、窗口、线程切换、文件选择、设置保存
│   │   ├── theme.go            # 深浅调色板、状态色、字体
│   │   ├── icons.go            # 描边图标
│   │   ├── view.go             # 根视图、侧边栏、导航、toast
│   │   ├── page_upload.go      # 上传页
│   │   ├── page_history.go     # 历史页
│   │   ├── page_settings.go    # 设置页
│   │   └── ui_kit.go           # 字节格式化、状态颜色、标题与设置行等组件
│   ├── upload/                 # 上传域：与 UI 无关，可独立测试
│   │   ├── provider.go         # Provider 接口、对象键规则
│   │   ├── s3.go               # S3 兼容实现 + SigV4 签名 + 进度读取
│   │   ├── manager.go          # 任务队列、并发、状态机
│   │   └── s3_test.go          # 上传与对象键测试
│   └── store/                  # 持久化：配置（含密钥）、历史
│       ├── config.go
│       └── history.go
└── docs/
    ├── architecture.md         # 架构、线程模型、数据流
    ├── providers.md            # 缤纷云与其他 S3 服务配置、排错
    └── developing.md           # 开发约定、扩展厂商、打包
```

## 上传流程

```
用户选择文件
   │
   ▼
app.pickFiles ──► upload.Manager.Enqueue(后台协程, 并发 3)
                       │
                       ├─ 读取文件计算 SHA-256（S3 签名需要）
                       ├─ 生成对象键 images/yyyy/mm/dd/<sha>.ext
                       ├─ S3 兼容上传（HTTP PUT + SigV4）
                       │      └─ 读取过程中回报进度
                       └─ 成功后回调 app.done
                                  │
                                  ▼
                    Window.Update(主线程): 写历史、复制链接、刷新界面
```

## 文档

- [架构与设计](docs/architecture.md)
- [云厂商配置与排错](docs/providers.md)
- [开发指南：扩展厂商与页面](docs/developing.md)

## 测试

```powershell
go vet ./...
go test ./...
```

测试覆盖对象键规则、SigV4 请求结构、上传结果与队列状态机，不依赖真实云账号。
