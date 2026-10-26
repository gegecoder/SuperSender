// Package update 版本更新检测：请求 GitCode 最新发布接口并与当前版本比对。
// 版本号与接口地址均来自 app.config.json。
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"SuperSender/backend/appconfig"
)

// checkUpdateTimeout 检测更新的网络超时时间
const checkUpdateTimeout = 10 * time.Second

// Info 版本更新检测结果，返回前端
type Info struct {
	CurrentVersion string `json:"currentVersion"` // 当前版本
	LatestVersion  string `json:"latestVersion"`  // 远端最新版本（tag_name，已去掉前导 v）
	HasUpdate      bool   `json:"hasUpdate"`      // 是否存在新版本
	Prerelease     bool   `json:"prerelease"`     // 是否为预发布版本
	ReleaseName    string `json:"releaseName"`    // 发布标题
	ReleaseNotes   string `json:"releaseNotes"`   // 更新说明（body 原文）
	PublishedAt    string `json:"publishedAt"`    // 发布时间（yyyy-MM-dd）
	ReleaseUrl     string `json:"releaseUrl"`     // 发布页面地址
	DownloadUrl    string `json:"downloadUrl"`    // 优先 .exe 附件的下载地址
	Msg            string `json:"msg"`            // ok 或错误提示
}

// gitcodeRelease GitCode releases 接口返回结构（仅保留需要的字段）
type gitcodeRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	Prerelease bool   `json:"prerelease"`
	CreatedAt  string `json:"created_at"`
	Assets     []struct {
		Name               string `json:"name"`
		Type               string `json:"type"` // source 源码包 / attach 附件
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckForUpdate 请求 GitCode 最新发布接口并与当前版本比对
func CheckForUpdate(cfg appconfig.AppConfig) Info {
	current := cfg.Version
	result := Info{CurrentVersion: current, Msg: "ok"}

	req, err := http.NewRequest(http.MethodGet, cfg.Update.APIURL, nil)
	if err != nil {
		result.Msg = "构造检测请求失败：" + err.Error()
		return result
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cfg.Name+"-UpdateChecker/"+current)

	client := &http.Client{Timeout: checkUpdateTimeout}
	resp, err := client.Do(req)
	if err != nil {
		result.Msg = "检测更新失败（网络异常）：" + err.Error()
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Msg = fmt.Sprintf("检测更新失败：服务端返回 %d", resp.StatusCode)
		return result
	}

	var rel gitcodeRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		result.Msg = "解析版本信息失败：" + err.Error()
		return result
	}
	if strings.TrimSpace(rel.TagName) == "" {
		result.Msg = "暂未发布任何版本"
		return result
	}

	latest := normalizeVersion(rel.TagName)
	result.LatestVersion = latest
	result.HasUpdate = compareVersion(latest, current) > 0
	result.Prerelease = rel.Prerelease
	result.ReleaseName = rel.Name
	result.ReleaseNotes = strings.TrimSpace(rel.Body)
	result.PublishedAt = formatReleaseDate(rel.CreatedAt)
	result.ReleaseUrl = cfg.Update.ReleasePageBase + url.PathEscape(rel.TagName)
	result.DownloadUrl = pickDownloadURL(rel, cfg.Update.ReleasePageBase)
	return result
}

// pickDownloadURL 优先选取 .exe 附件，其次任意附件，再次任意源码包；
// 没有可用附件时回退到发布页面
func pickDownloadURL(rel gitcodeRelease, releasePageBase string) string {
	fallbackAttach := ""
	for _, asset := range rel.Assets {
		if asset.BrowserDownloadURL == "" {
			continue
		}
		name := strings.ToLower(asset.Name)
		if asset.Type == "attach" {
			if strings.HasSuffix(name, ".exe") {
				return asset.BrowserDownloadURL
			}
			if fallbackAttach == "" {
				fallbackAttach = asset.BrowserDownloadURL
			}
		}
	}
	if fallbackAttach != "" {
		return fallbackAttach
	}
	for _, asset := range rel.Assets {
		if asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL
		}
	}
	return releasePageBase + url.PathEscape(rel.TagName)
}

// formatReleaseDate 将 RFC3339 时间格式化为 yyyy-MM-dd，解析失败时原样返回
func formatReleaseDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.Format("2006-01-02")
	}
	return raw
}

// normalizeVersion 去除版本号前后空白、前导 v/V 以及预发布后缀（-beta 等）
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	return strings.TrimSpace(v)
}

// compareVersion 按点分数字段比较版本号：a>b 返回 1，a<b 返回 -1，相等返回 0；
// 非数字片段按 0 处理，缺失段按 0 补齐
func compareVersion(a, b string) int {
	pa := strings.Split(normalizeVersion(a), ".")
	pb := strings.Split(normalizeVersion(b), ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = leadingInt(pa[i])
		}
		if i < len(pb) {
			y = leadingInt(pb[i])
		}
		if x > y {
			return 1
		}
		if x < y {
			return -1
		}
	}
	return 0
}

// leadingInt 提取字符串开头的连续数字，无数字时返回 0
func leadingInt(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return n
}
