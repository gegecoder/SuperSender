// Package storage 负责本地 JSON 配置文件持久化。
// 存放路径：应用本地数据目录（Windows: %APPDATA%/SuperSender，Linux: ~/.SuperSender）。
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"SuperSender/backend/models"
)

// Storage 存储管理器
type Storage struct {
	baseDir string
	mu      sync.RWMutex
}

// New 创建存储实例，自动初始化数据目录
func New(appName string) (*Storage, error) {
	baseDir, err := appDataDir(appName)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &Storage{baseDir: baseDir}, nil
}

// BaseDir 返回基础目录路径
func (s *Storage) BaseDir() string {
	return s.baseDir
}

// ResetAll 清空全部持久化数据（会话/模板/设置），恢复出厂状态
func (s *Storage) ResetAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{"sessions.json", "templates.json", "settings.json"} {
		p := filepath.Join(s.baseDir, name)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 %s 失败: %w", name, err)
		}
	}
	return nil
}

// appDataDir 跨平台获取应用数据目录
func appDataDir(appName string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			base = os.Getenv("LOCALAPPDATA")
		}
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, appName), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "."+appName), nil
	}
}

// path 拼接文件路径
func (s *Storage) path(name string) string {
	return filepath.Join(s.baseDir, name)
}

// SaveJSON 保存任意可序列化对象
func (s *Storage) SaveJSON(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// 写入临时文件再原子替换，防止写入中断损坏配置
	tmp := s.path(name + ".tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

// LoadJSON 读取任意可序列化对象
func (s *Storage) LoadJSON(name string, v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Exists 判断文件是否存在
func (s *Storage) Exists(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(s.path(name))
	return err == nil
}

// === 业务专用方法 ===

// SaveSessions 保存全部会话配置
func (s *Storage) SaveSessions(sessions []models.SessionConfig) error {
	return s.SaveJSON("sessions.json", sessions)
}

// LoadSessions 加载全部会话配置
func (s *Storage) LoadSessions() ([]models.SessionConfig, error) {
	var sessions []models.SessionConfig
	if err := s.LoadJSON("sessions.json", &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

// SaveTemplates 保存模板列表
func (s *Storage) SaveTemplates(tpl []models.Template) error {
	return s.SaveJSON("templates.json", tpl)
}

// LoadTemplates 加载模板列表
func (s *Storage) LoadTemplates() ([]models.Template, error) {
	var tpl []models.Template
	if err := s.LoadJSON("templates.json", &tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

// SaveSettings 保存全局设置
func (s *Storage) SaveSettings(set models.Settings) error {
	return s.SaveJSON("settings.json", set)
}

// LoadSettings 加载全局设置
func (s *Storage) LoadSettings() (models.Settings, error) {
	var set models.Settings
	if err := s.LoadJSON("settings.json", &set); err != nil {
		return DefaultSettings(), err
	}
	return set, nil
}

// DefaultSettings 默认全局设置
func DefaultSettings() models.Settings {
	return models.Settings{
		Theme:    "light",
		Language: "zh", // 默认中文
		LogDir:   "",   // 空 = 使用 applog.DefaultDir()
	}
}

// LoadSettingsOrDefault 加载设置，失败时返回默认值（不返回错误）
func (s *Storage) LoadSettingsOrDefault() models.Settings {
	def := DefaultSettings()
	set, err := s.LoadSettings()
	if err != nil {
		set = def
	}
	if set.Theme == "" {
		set.Theme = "light"
	}
	if set.Language == "" {
		set.Language = "zh"
	}
	return set
}

// AppendReceiveToFile 实时落盘保存接收报文
func (s *Storage) AppendReceiveToFile(name string, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path(name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}
