package upload

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Category 是接口返回的受控分类。
type Category struct {
	CID  int    `json:"cid"`
	Name string `json:"name"`
}

// FetchCategories 从上报接口读取全部分类，顺序与服务端一致。
func FetchCategories(base, token string) ([]Category, error) {
	if baseURL(base) == "" {
		return nil, nil
	}
	req, err := http.NewRequest(http.MethodGet, apiEndpoint(base, "/api/v1/categories"), nil)
	if err != nil {
		return nil, fmt.Errorf("分类接口地址无效：%w", err)
	}
	if token != "" {
		req.Header.Set("X-Api-Token", token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取分类失败：%w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("读取分类失败（HTTP %d）", resp.StatusCode)
	}
	var body struct {
		Code int        `json:"code"`
		Msg  string     `json:"msg"`
		Data []Category `json:"data"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("分类返回格式不正确：%w", err)
	}
	if body.Code != http.StatusOK && body.Code != 0 {
		msg := body.Msg
		if msg == "" {
			msg = "未知错误"
		}
		return nil, fmt.Errorf("读取分类失败：%s", msg)
	}

	out := make([]Category, 0, len(body.Data))
	for _, c := range body.Data {
		if name := strings.TrimSpace(c.Name); name != "" {
			out = append(out, Category{CID: c.CID, Name: name})
		}
	}
	return out, nil
}
