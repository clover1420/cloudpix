package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record 是一条上传成功的历史记录。
type Record struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Key     string    `json:"key"`
	URL     string    `json:"url"`
	Profile string    `json:"profile"`
	At      time.Time `json:"at"`
	// Report 是图片信息上报的结果："ok" 成功、"fail" 失败，
	// 空值表示没有上报（未启用或任务被跳过）。
	Report string `json:"report,omitempty"`
}

// historyLimit 是历史文件保留的条数。
const historyLimit = 300

var historyMu sync.Mutex

func historyPath() string { return filepath.Join(dataDir(), "history.json") }

// LoadHistory 返回历史记录，最新的在前。
func LoadHistory() []Record {
	historyMu.Lock()
	defer historyMu.Unlock()
	return loadHistoryLocked()
}

func loadHistoryLocked() []Record {
	data, err := os.ReadFile(historyPath())
	if err != nil {
		return nil
	}
	var out []Record
	_ = json.Unmarshal(data, &out)
	if len(out) > historyLimit {
		out = out[:historyLimit]
	}
	return out
}

// AddHistory 在文件头部插入一条记录，并裁剪到上限。
func AddHistory(r Record) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	all := loadHistoryLocked()
	// 同一个对象重复上传时只保留最新一条。
	kept := all[:0]
	for _, old := range all {
		if old.Key == r.Key && old.URL == r.URL {
			continue
		}
		kept = append(kept, old)
	}
	all = append([]Record{r}, kept...)
	if len(all) > historyLimit {
		all = all[:historyLimit]
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(historyPath(), data)
}

// UpdateHistoryReport 把最近一条对应 ID 的记录标记上报结果并落盘。
func UpdateHistoryReport(id int64, state string) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	all := loadHistoryLocked()
	for i := range all {
		if all[i].ID == id {
			if all[i].Report == state {
				return nil
			}
			all[i].Report = state
			break
		}
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(historyPath(), data)
}

// ClearHistory 删除全部历史。
func ClearHistory() error {
	historyMu.Lock()
	defer historyMu.Unlock()
	return writeFile(historyPath(), []byte("[]"))
}
