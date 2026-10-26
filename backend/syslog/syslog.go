// Package syslog 实现 RFC3164 协议封装：根据 Facility/Severity 自动拼接报文头，
// 用户只需填写日志正文，避免手工拼接错误。
package syslog

import (
	"fmt"
	"strings"
	"time"

	"SuperSender/backend/models"
)

// Facility 名称到数值映射（RFC 5424 常用部分）
var facilityMap = map[string]int{
	"kern": 0, "user": 1, "mail": 2, "daemon": 3, "auth": 4, "syslog": 5,
	"lpr": 6, "news": 7, "uucp": 8, "cron": 9, "authpriv": 10, "ftp": 11,
	"local0": 16, "local1": 17, "local2": 18, "local3": 19,
	"local4": 20, "local5": 21, "local6": 22, "local7": 23,
}

// Severity 名称到数值映射
var severityMap = map[string]int{
	"emerg": 0, "alert": 1, "crit": 2, "err": 3, "warning": 4,
	"notice": 5, "info": 6, "debug": 7,
}

// FacilityOptions 供下拉选择
func FacilityOptions() []map[string]any {
	out := make([]map[string]any, 0, len(facilityMap))
	for name, val := range facilityMap {
		out = append(out, map[string]any{"label": fmt.Sprintf("%d - %s", val, name), "value": val})
	}
	return out
}

// SeverityOptions 供下拉选择
func SeverityOptions() []map[string]any {
	out := make([]map[string]any, 0, len(severityMap))
	for name, val := range severityMap {
		out = append(out, map[string]any{"label": fmt.Sprintf("%d - %s", val, name), "value": val})
	}
	return out
}

// Build 根据配置生成完整 Syslog 报文字节
// 报文格式：<PRI>TIMESTAMP HOSTNAME TAG: MESSAGE
// 高级配置未启用（Advanced=false）时，不拼接 Syslog 头，直接发送正文原文；
// 启用后按 Facility/Severity/Hostname/Tag 拼接 RFC3164 头（缺省值兜底）
func Build(cfg *models.SyslogConfig, message string) []byte {
	if cfg == nil {
		return []byte(message)
	}
	// 高级配置未启用：以输入框回显内容为发送内容
	if !cfg.Advanced {
		return []byte(strings.TrimRight(message, "\r\n"))
	}
	// 自定义模式：用户自定义前缀直接拼接正文，不组装 RFC3164 头
	if cfg.Mode == "custom" {
		return []byte(cfg.CustomPrefix + strings.TrimRight(message, "\r\n"))
	}
	facility := 16 // 默认 local0
	severity := 6  // 默认 info
	hostname := "supersender"
	tag := "app"
	if cfg.Facility >= 0 && cfg.Facility <= 23 {
		facility = cfg.Facility
	}
	if cfg.Severity >= 0 && cfg.Severity <= 7 {
		severity = cfg.Severity
	}
	if cfg.Hostname != "" {
		hostname = cfg.Hostname
	}
	if cfg.Tag != "" {
		tag = cfg.Tag
	}
	pri := facility*8 + severity
	ts := time.Now().Format("Jan 2 15:04:05")
	// RFC3164: <PRI>TIMESTAMP HOSTNAME TAG: CONTENT
	header := fmt.Sprintf("<%d>%s %s %s: ", pri, ts, hostname, tag)
	return []byte(header + strings.TrimRight(message, "\r\n"))
}

// WrapIfNeeded 根据会话配置决定是否封装 syslog
func WrapIfNeeded(cfg *models.SessionConfig, payload string) []byte {
	if cfg.Syslog != nil && cfg.Syslog.Enabled {
		return Build(cfg.Syslog, payload)
	}
	return []byte(payload)
}
