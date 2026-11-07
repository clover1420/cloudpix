package upload

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// S3Config 配置一个兼容 S3 协议的对象存储。
//
// 缤纷云（S4）的默认值：Endpoint 为 https://s3.bitiful.net，
// Region 为 cn-east-1，虚拟主机风格（PathStyle=false）。
// MinIO 通常需要 PathStyle=true 并填写 http://127.0.0.1:9000。
type S3Config struct {
	Name string
	// Endpoint 服务地址，带或不带 scheme 均可。
	Endpoint string
	// Region 参与 SigV4 签名；S3 兼容服务通常忽略它，但必须与配置一致。
	Region string
	Bucket string
	// AccessKey / SecretKey 从系统钥匙串读取，不写入配置文件。
	AccessKey string
	SecretKey string
	// PublicBase 是 CDN 或公开访问域名，例如 https://cdn.example.com。
	// 留空时用 Endpoint 拼出默认地址。
	PublicBase string
	// PathStyle 为 true 时 URL 形如 endpoint/bucket/key，
	// 为 false 时形如 bucket.endpoint/key。
	PathStyle bool
}

// S3 是兼容 S3 协议（SigV4 签名）的 Provider 实现。
type S3 struct {
	cfg      S3Config
	endpoint *url.URL
	client   *http.Client
}

// NewS3 校验配置并创建上传器。
func NewS3(cfg S3Config) (*S3, error) {
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	if cfg.Bucket == "" {
		return nil, ConfigError("Bucket")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, ConfigError("AccessKey / SecretKey")
	}
	ep := strings.TrimSpace(cfg.Endpoint)
	if ep == "" {
		return nil, ConfigError("Endpoint")
	}
	if !strings.Contains(ep, "://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w：Endpoint 无法解析（%s）", ErrNotConfigured, cfg.Endpoint)
	}
	if cfg.Region = strings.TrimSpace(cfg.Region); cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Name = strings.TrimSpace(cfg.Name); cfg.Name == "" {
		cfg.Name = cfg.Bucket
	}
	return &S3{cfg: cfg, endpoint: u, client: &http.Client{}}, nil
}

// Name 返回展示用名称。
func (s *S3) Name() string { return s.cfg.Name }

// PublicURL 返回对象的公开访问地址。
func (s *S3) PublicURL(key string) string {
	if s.cfg.PublicBase != "" {
		return joinURL(s.cfg.PublicBase, key)
	}
	return s.objectURL(key).String()
}

// Upload 用一次 HTTP PUT 上传对象。S3 的签名要求提前知道内容摘要，
// 因此 Object.SHA256 必须由调用方提供；没有时这里会读完 Body 兜底计算。
func (s *S3) Upload(ctx context.Context, obj Object, progress func(sent, total int64)) (Result, error) {
	if obj.Key == "" {
		return Result{}, ConfigError("对象键")
	}
	sum := obj.SHA256
	if sum == "" {
		data, err := io.ReadAll(obj.Body)
		if err != nil {
			return Result{}, err
		}
		h := sha256.Sum256(data)
		sum = hex.EncodeToString(h[:])
		obj.Size = int64(len(data))
		obj.Body = bytes.NewReader(data)
	}

	target := s.objectURL(obj.Key)
	var body io.Reader = obj.Body
	if progress != nil {
		body = &progressReader{r: obj.Body, total: obj.Size, fn: progress}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target.String(), body)
	if err != nil {
		return Result{}, err
	}
	req.ContentLength = obj.Size
	if obj.ContentType != "" {
		req.Header.Set("Content-Type", obj.ContentType)
	}
	req.Header.Set("x-amz-content-sha256", sum)
	now := time.Now().UTC()
	req.Header.Set("x-amz-date", now.Format("20060102T150405Z"))
	signV4(req, sum, s.cfg.Region, s.cfg.AccessKey, s.cfg.SecretKey, now)

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Result{}, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(detail))}
	}
	return Result{
		Key:  obj.Key,
		URL:  s.PublicURL(obj.Key),
		ETag: strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

// objectURL 依据 PathStyle 生成对象地址。
func (s *S3) objectURL(key string) *url.URL {
	u := *s.endpoint
	u.RawQuery = ""
	u.Fragment = ""
	key = strings.TrimLeft(key, "/")
	if s.cfg.PathStyle {
		u.Path = "/" + s.cfg.Bucket + "/" + key
	} else {
		u.Host = s.cfg.Bucket + "." + u.Host
		u.Path = "/" + key
	}
	return &u
}

// progressReader 在读取过程中回报进度；HTTP 客户端按块读取，足够顺滑。
type progressReader struct {
	r     io.Reader
	sent  int64
	total int64
	fn    func(sent, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	p.fn(p.sent, p.total)
	return n, err
}

// signV4 计算 AWS Signature Version 4 并写入 Authorization 头。
// 只签 host、x-amz-content-sha256、x-amz-date 三个头，这正是 S3 PUT
// 要求的最小集合。
func signV4(req *http.Request, payloadHash, region, accessKey, secretKey string, t time.Time) {
	const algorithm = "AWS4-HMAC-SHA256"
	date := t.Format("20060102")
	scope := date + "/" + region + "/s3/aws4_request"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + req.Header.Get("x-amz-date") + "\n"
	canonicalRequest := req.Method + "\n" +
		req.URL.EscapedPath() + "\n" +
		"" + "\n" + // 查询串为空
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash

	hashed := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := algorithm + "\n" + req.Header.Get("x-amz-date") + "\n" + scope + "\n" +
		hex.EncodeToString(hashed[:])

	kDate := hmacSHA256([]byte("AWS4"+secretKey), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	req.Header.Set("Authorization", algorithm+
		" Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+
		", Signature="+signature)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// HTTPError 是服务端返回的非 2xx 响应。
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d：%s", e.StatusCode, e.Body)
}
