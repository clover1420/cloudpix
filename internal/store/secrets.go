package store

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
)

// Credentials 是对象存储的访问密钥。它只保存在系统钥匙串里，
// 或者（钥匙串不可用时）保存在 0600 权限的本地文件中。
type Credentials struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// Empty 表示还没有填写密钥。
func (c Credentials) Empty() bool { return c.AccessKey == "" || c.SecretKey == "" }

// 凭据文件只在系统钥匙串不可用时使用。
var credMu sync.Mutex

func credKey(profileID string) string { return "cred." + profileID }

// LoadCredentials 读取某个配置的密钥。优先系统钥匙串，
// 失败时回退到 credentials.json。
func LoadCredentials(profileID string) Credentials {
	if data, err := mygo.Secrets.Get(credKey(profileID)); err == nil {
		var c Credentials
		if json.Unmarshal(data, &c) == nil {
			return c
		}
	} else if !errors.Is(err, mygo.ErrSecretNotFound) {
		log.Printf("读取钥匙串失败，回退到本地凭据文件：%v", err)
	}
	return readCredFile(profileID)
}

// SaveCredentials 写入某个配置的密钥。钥匙串不可用时写本地文件。
func SaveCredentials(profileID string, c Credentials) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := mygo.Secrets.Set(credKey(profileID), data); err == nil {
		return nil
	} else {
		log.Printf("写入钥匙串失败，回退到本地凭据文件：%v", err)
	}
	return writeCredFile(profileID, c)
}

// DeleteCredentials 删除某个配置的密钥（两处都删）。
func DeleteCredentials(profileID string) {
	_ = mygo.Secrets.Delete(credKey(profileID))
	credMu.Lock()
	defer credMu.Unlock()
	all := readAllCredFiles()
	delete(all, profileID)
	if data, err := json.MarshalIndent(all, "", "  "); err == nil {
		_ = writeFile(credFilePath(), data)
	}
}

func credFilePath() string { return filepath.Join(dataDir(), "credentials.json") }

func readCredFile(profileID string) Credentials {
	credMu.Lock()
	defer credMu.Unlock()
	return readAllCredFiles()[profileID]
}

func readAllCredFiles() map[string]Credentials {
	out := map[string]Credentials{}
	data, err := os.ReadFile(credFilePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func writeCredFile(profileID string, c Credentials) error {
	credMu.Lock()
	defer credMu.Unlock()
	all := readAllCredFiles()
	all[profileID] = c
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(credFilePath(), data)
}
