# 云厂商配置与排错

CloudPix 通过 S3 协议上传，因此“换云”通常只是改几个配置字段。
本文给出缤纷云 S4 的配置步骤、常见 S3 兼容服务的参数，以及错误排查表。

## 1. 配置字段的含义

| 字段 | 含义 | 影响 |
| --- | --- | --- |
| 厂商类型 | `bitiful` / `s3` | 只决定预设默认值，实际仍走 S3 协议 |
| Endpoint | 服务地址，带 `https://` | 请求目标与签名 Host |
| Region | 参与 SigV4 签名的区域名 | 填错会得到 `SignatureDoesNotMatch` |
| Bucket | 存储桶名 | 决定请求路径 / Host |
| 公开访问域名 | 生成外链时使用的域名 | 留空则用 Endpoint 拼 |
| 对象前缀 | 对象键的第一段目录 | 例如 `images` |
| PathStyle | 地址风格 | `true` 为 `endpoint/bucket/key`；`false` 为 `bucket.endpoint/key` |
| AccessKey / SecretKey | 访问密钥 | 随配置保存在 `config.json`（0600） |

## 2. 缤纷云 S4

1. 在缤纷云控制台创建一个存储桶（S4 的桶是“目录桶”）。
2. 创建子账户并生成 AccessKey / SecretKey。建议单独建一个只有目标桶
   写入权限的子账户，不要使用主账户密钥。
3. 按需开启公开访问：如果希望复制出来的链接能直接在浏览器打开，
   需要把桶设为公开读，或在控制台绑定自定义域名 / CDN 域名，并填入
   “公开访问域名”。
4. 在 CloudPix 的「设置」里填写：

| 字段 | 值 |
| --- | --- |
| 厂商类型 | `bitiful` |
| Endpoint | `https://s3.bitiful.net` |
| Region | `cn-east-1` |
| Bucket | 你的桶名 |
| PathStyle | 关闭 |
| 公开访问域名 | 可留空，或填绑定的 CDN / 自定义域名 |

官方参考：[缤纷云文档 · 用 SDK 连接 S4](https://docs.bitiful.com/developer/sdk/python3-demo/start-to-use)、
[客户端安全上传文件](https://docs.bitiful.com/best-practice/web-uploads)。

上传成功后对象键形如：

```
images/2026/11/07/8f3c1a2b9d4e5f60.png
```

公开链接形如（未填公开域名时）：

```
https://<bucket>.s3.bitiful.net/images/2026/11/07/8f3c1a2b9d4e5f60.png
```

> 链接能否访问取决于桶的读权限。上传成功但打开 403，属于桶策略问题，不是签名问题。

## 3. 其他 S3 兼容服务

| 服务 | Endpoint 示例 | Region | PathStyle | 备注 |
| --- | --- | --- | --- | --- |
| MinIO | `http://127.0.0.1:9000` | 任意（如 `us-east-1`） | 打开 | 本地自建首选 |
| Cloudflare R2 | `https://<account_id>.r2.cloudflarestorage.com` | `auto` | 关闭 | 需在 R2 面板绑定公开域名 |
| Backblaze B2 | `https://s3.<region>.backblazeb2.com` | 如 `us-west-004` | 打开 | 桶名全局唯一 |
| AWS S3 | `https://s3.<region>.amazonaws.com` | 如 `us-east-1` | 关闭 | 需给密钥 PutObject 权限 |
| 七牛 / 阿里 / 腾讯 | 以控制台提供的 S3 端点为准 | 以官方为准 | 视端点而定 | 兼容程度不一，先做一次测试上传 |

判断规则：如果服务只提供 `endpoint/bucket/key` 形式的地址（自建 MinIO、部分 B2 配置），
打开 PathStyle；如果官方 SDK 示例里 URL 的主机名带桶名前缀，则关闭。

### 配置通用 S3 的步骤

1. 「设置」里厂商类型选 `s3`。
2. 填 Endpoint、Region、Bucket。
3. 先在浏览器/命令行确认该 Endpoint 的 S3 接口可用，再到 CloudPix 里上传。
4. 上传一张小图，复制链接验证公开访问。

## 4. 公开访问与 CDN

- **公开域名留空**：使用 `<bucket>.<endpoint host>/<key>`（PathStyle 时是
  `<endpoint>/<bucket>/<key>`）。
- **公开域名**：填写绑定到桶的 CDN / 自定义域名，CloudPix 会生成
  `<publicBase>/<key>`，对象键中的中文与空格会被 URL 转义。
- 如果配置了防盗链、Referer 白名单、签名 URL，直接把裸链接贴到浏览器可能 403；
  这是 CDN 策略，与应用无关。

## 5. 排错表

| 现象 | 可能原因 | 处理 |
| --- | --- | --- |
| `HTTP 403：SignatureDoesNotMatch` | Region 填错；AK/SK 不匹配；本机时间偏差超过 15 分钟；虚拟主机风格下 Bucket 名错误 | 核对 Region（缤纷云为 `cn-east-1`）；校准系统时间；核对桶名 |
| `HTTP 403：AccessDenied` | 密钥没有 `PutObject` 权限；桶只读；IP/Referer 限制 | 给子账户授予目标桶写入权限 |
| `HTTP 404：NoSuchBucket` | Bucket 拼错；Endpoint 与 PathStyle 组合错误 | 切换 PathStyle 再试；确认桶存在 |
| `HTTP 400：RequestTimeTooSkewed` | 系统时间不准 | 同步系统时间 |
| 上传中长时间无进度 | 网络/代理阻断；Endpoint 用了 http 但服务要求 https | 用 `curl` 验证 Endpoint 可达 |
| `x509: certificate ...` | 自签证书或代理中间人 | MinIO 用 http；或把 CA 加入系统信任 |
| 上传成功但链接 403 | 桶私有；未绑定 CDN；防盗链 | 开启公开读或使用自定义域名 |
| 链接打不开且含中文 | CDN 未正确解码 URL | 开启“保留原文件名”后仍应能访问；否则改用摘要命名 |
| 大文件失败 | 单次 PUT 限制；网关超时 | 改用 multipart 上传（需扩展实现） |

## 6. 安全建议

- 桌面应用把 SK 存在本机是合理的：随配置写入 `config.json`，权限为 `0600`。
- **不要**把这种“客户端持 SK 直传”的模式搬到 Web 前端；浏览器里应使用服务端签发的
  预签名 URL（限时、限对象键、限方法）。
- 使用子账户 + 最小权限（仅目标桶、仅 PutObject），并定期轮换密钥。
- 不要把含密钥的 `config.json` 提交到代码仓库；用户数据目录在系统路径下，不在仓库内。

## 7. 连通性自检

1. 设置页填好参数后保存，左侧状态卡应显示“已配置”。
2. 上传页选一张小于 1 MB 的 PNG。
3. 进度到 100% 且状态为“已完成”，说明签名与写入都正常。
4. 点“复制链接”在浏览器验证公开读；若 403，按第 4、5 节处理。

下一篇：[开发指南](developing.md)
