// Package appconfig 应用的统一信息配置（对应仓库根目录 app.config.json）。
// Go 端通过 //go:embed 编译期内嵌，前端通过 Vite 直接导入同一份 JSON，
// 发版时只需修改 app.config.json 一处。
package appconfig

import (
	"encoding/json"
	"errors"
	"strings"
)

// AuthorInfo 作者信息
type AuthorInfo struct {
	Name  string `json:"name"`  // 作者名称
	Email string `json:"email"` // 联系邮箱
	Blog  string `json:"blog"`  // 博客地址
}

// UpdateConfig 更新检测配置
type UpdateConfig struct {
	APIURL          string `json:"apiUrl"`          // GitCode 最新 release 接口
	ReleasePageBase string `json:"releasePageBase"` // 发布页面前缀，拼接 tag 使用
}

// BrandingConfig 界面品牌文案配置（顶栏副标题、欢迎页标题/副标题）
type BrandingConfig struct {
	BrandSub     string `json:"brandSub"`     // 顶栏品牌副标题
	WelcomeTitle string `json:"welcomeTitle"` // 欢迎页主标题
	WelcomeSub   string `json:"welcomeSub"`   // 欢迎页副标题
}

// AppConfig 是应用的统一信息配置
type AppConfig struct {
	Name        string         `json:"name"`        // 应用名称（窗口标题、托盘、关于页）
	Description string         `json:"description"` // 应用一句话描述
	Version     string         `json:"version"`     // 当前版本号
	Branding    BrandingConfig `json:"branding"`    // 界面品牌文案
	Author      AuthorInfo     `json:"author"`      // 作者信息
	Update      UpdateConfig   `json:"update"`      // 更新检测相关地址
}

// LoadAppConfig 解析并校验内嵌的 app.config.json 内容
func LoadAppConfig(raw []byte) (AppConfig, error) {
	var cfg AppConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, errors.New("解析 app.config.json 失败：" + err.Error())
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return cfg, errors.New("app.config.json 缺少 name")
	}
	if strings.TrimSpace(cfg.Version) == "" {
		return cfg, errors.New("app.config.json 缺少 version")
	}
	if strings.TrimSpace(cfg.Update.APIURL) == "" {
		return cfg, errors.New("app.config.json 缺少 update.apiUrl")
	}
	if strings.TrimSpace(cfg.Update.ReleasePageBase) == "" {
		return cfg, errors.New("app.config.json 缺少 update.releasePageBase")
	}
	return cfg, nil
}
