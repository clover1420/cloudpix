package upload

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchCategories(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"msg":"ok","data":[
			{"cid":3,"name":"风景"},
			{"cid":1,"name":" 人物 "},
			{"cid":2,"name":""}
		]}`))
	}))
	defer srv.Close()

	cats, err := FetchCategories(srv.URL, "secret")
	if err != nil {
		t.Fatalf("读取分类失败：%v", err)
	}
	if path != "/api/v1/categories" {
		t.Fatalf("请求路径不对：%s", path)
	}
	if len(cats) != 2 || cats[0].CID != 3 || cats[0].Name != "风景" || cats[1].CID != 1 || cats[1].Name != "人物" {
		t.Fatalf("分类解析不对：%v", cats)
	}
}

func TestFetchCategoriesEmptyBase(t *testing.T) {
	if cats, err := FetchCategories("", ""); err != nil || cats != nil {
		t.Fatalf("未配置域名时不应发请求：%v %v", cats, err)
	}
}
