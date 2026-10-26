// Package template 报文模板管理：CRUD、内置预制样本（防火墙/交换机/Oracle/SNMP-Trap）、
// 参数化变量替换（${random_ip} ${random_time} ${random_int} ${seq}）。
package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"SuperSender/backend/models"
	"SuperSender/backend/storage"
)

// Manager 模板管理器
type Manager struct {
	store *storage.Storage
	mu    sync.RWMutex
	items map[string]*models.Template
}

// New 创建模板管理器并加载/初始化内置模板
func New(store *storage.Storage) *Manager {
	m := &Manager{
		store: store,
		items: make(map[string]*models.Template),
	}
	m.loadOrSeed()
	return m
}

// loadOrSeed 加载本地模板；不存在则种入内置模板。
// 已有数据时补种缺失的内置模板，保证升级后新增内置模板自动出现。
func (m *Manager) loadOrSeed() {
	list, err := m.store.LoadTemplates()
	if err != nil {
		m.seedBuiltin()
		return
	}
	m.mu.Lock()
	for i := range list {
		t := list[i]
		m.items[t.ID] = &t
	}
	added := false
	for _, b := range builtinTemplates() {
		if _, ok := m.items[b.ID]; !ok {
			t := b
			m.items[t.ID] = &t
			added = true
		}
	}
	m.mu.Unlock()
	if added {
		m.mu.Lock()
		defer m.mu.Unlock()
		_ = m.persistLocked()
	}
}

// builtinTemplates 返回全部内置模板定义
func builtinTemplates() []models.Template {
	now := time.Now()
	return []models.Template{
		{
			ID: "tpl-syslog-firewall", Name: "Syslog-防火墙拦截日志", Category: "syslog-firewall",
			Content: "<134>Oct 12 10:23:45 firewall1 %FIREWALL/2/SESSION: ${random_ip} 被拦截, 端口 ${random_int(1,65535)}, 协议 TCP",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-syslog-firewall-attack", Name: "Syslog-防火墙攻击告警", Category: "syslog-firewall",
			Content: "<132>${random_time} FW-MASTER %%01ATK/3/ATTACK(l)[${seq}]: 服务器 ${random_ip} 端口 ${random_port} 受到来自 ${random_ip} 的 SYN Flood 攻击, 速率 ${random_int(100,9999)}pps, 已自动封禁",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-syslog-switch", Name: "Syslog-交换机接口告警", Category: "syslog-switch",
			Content: "<189>Oct 12 14:01:22 core-sw01 %LINEPROTO/5/UPDOWN: Line protocol on Interface Gi1/0/${random_int(1,48)} changed state to DOWN",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-syslog-huawei", Name: "Syslog-华为设备接口状态", Category: "syslog-switch",
			Content: "<189>${random_time} HUAWEI-SW %%01IFNET/4/IF_STATE(l)[${seq}]:Interface GigabitEthernet0/0/${random_int(1,24)} has turned into DOWN state",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-syslog-oracle", Name: "Syslog-Oracle审计日志", Category: "syslog-oracle",
			Content: "<134>${random_time} dbserver Oracle[AUDIT]: user admin@${random_ip} executed SELECT on table T_USER_DATA, rows=${random_int(1,1000)}",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-web-nginx", Name: "Web-Nginx 访问日志", Category: "web",
			Content: "<190>${random_time} nginx access: ${random_ip} - - \"GET /api/v1/users?page=${random_int(1,20)} HTTP/1.1\" 200 ${random_int(100,9999)} \"-\" \"Mozilla/5.0\" rt=${random_int(10,500)}ms",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-windows-logon", Name: "Windows-登录审计", Category: "windows",
			Content: "<134>${datetime} WIN-GK ${random_str(8)} Security[4624]: 登录成功 用户名 administrator 源地址 ${random_ip} 源端口 ${random_port}",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-app-heartbeat", Name: "应用-业务心跳(JSON)", Category: "custom",
			Content: "{\"seq\":${seq},\"ts\":${timestamp_ms},\"level\":\"INFO\",\"node\":\"node-${random_int(1,8)}\",\"msg\":\"order ${random_int(100000,999999)} processed in ${random_int(5,300)}ms\"}",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-snmp-trap", Name: "SNMP-Trap 链路中断", Category: "snmp",
			Content: "0x021401060406080204726f75746572040106020a2b06010406080204726f75746572",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "tpl-udp-heartbeat", Name: "通用UDP心跳", Category: "custom",
			Content: "{\"type\":\"heartbeat\",\"ts\":${random_int(1000000,9999999)},\"src\":\"${random_ip}\"}",
			Builtin: true, CreatedAt: now, UpdatedAt: now,
		},
	}
}

// seedBuiltin 写入内置模板
func (m *Manager) seedBuiltin() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range builtinTemplates() {
		t := b
		m.items[t.ID] = &t
	}
	_ = m.persistLocked()
}

// List 返回全部模板列表
func (m *Manager) List() []models.Template {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.Template, 0, len(m.items))
	for _, t := range m.items {
		out = append(out, *t)
	}
	return out
}

// Get 按 ID 获取模板
func (m *Manager) Get(id string) *models.Template {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.items[id]; ok {
		cp := *t
		return &cp
	}
	return nil
}

// Save 保存或更新模板（builtin 不允许覆盖内容但允许改名）
func (m *Manager) Save(t models.Template) (models.Template, error) {
	if t.ID == "" {
		t.ID = "tpl-" + randID(8)
	}
	now := time.Now()
	t.UpdatedAt = now
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[t.ID] = &t
	if err := m.persistLocked(); err != nil {
		return t, err
	}
	return t, nil
}

// Delete 删除模板，内置模板不可删除
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.items[id]
	if !ok {
		return fmt.Errorf("模板不存在: %s", id)
	}
	if t.Builtin {
		return fmt.Errorf("内置模板不可删除")
	}
	delete(m.items, id)
	return m.persistLocked()
}

// Export 将全部模板序列化为 JSON 数组写入指定文件（UTF-8）
func (m *Manager) Export(path string) error {
	m.mu.RLock()
	list := make([]models.Template, 0, len(m.items))
	for _, t := range m.items {
		list = append(list, *t)
	}
	m.mu.RUnlock()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Import 从 JSON 文件读取模板数组并合并：按 ID 去重，与现有模板 ID 冲突时
// 重新生成新 ID（不覆盖既有模板）；导入条目一律视为非内置。返回成功导入条数。
func (m *Manager) Import(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("读取文件失败: %w", err)
	}
	// 兼容 Windows 记事本等工具写入的 UTF-8 BOM
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var list []models.Template
	if err := json.Unmarshal(data, &list); err != nil {
		return 0, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if len(list) == 0 {
		return 0, fmt.Errorf("文件中没有可导入的模板")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	seen := make(map[string]bool, len(list))
	imported := 0
	for _, t := range list {
		// 文件内重复 ID 去重
		if t.ID != "" {
			if seen[t.ID] {
				continue
			}
			seen[t.ID] = true
		}
		// 空 ID 或与现有模板冲突 → 重新生成新 ID（追加而非覆盖）
		if _, ok := m.items[t.ID]; ok || t.ID == "" {
			t.ID = "tpl-" + randID(8)
		}
		t.Builtin = false
		if t.Category == "" {
			t.Category = "custom"
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = now
		}
		t.UpdatedAt = now
		m.items[t.ID] = &t
		imported++
	}
	if imported > 0 {
		if err := m.persistLocked(); err != nil {
			return 0, err
		}
	}
	return imported, nil
}

// persistLocked 持久化（调用方持锁）
func (m *Manager) persistLocked() error {
	list := make([]models.Template, 0, len(m.items))
	for _, t := range m.items {
		list = append(list, *t)
	}
	return m.store.SaveTemplates(list)
}

// Render 参数化渲染：将模板内容中的变量替换为随机/递增值
// 支持：${seq} ${random_ip} ${random_port} ${random_int(min,max)} ${random_hex(len)}
//
//	${random_str(len)} ${random_time} ${date} ${datetime} ${timestamp} ${timestamp_ms} ${uuid}
func Render(content string, seq int64, r *rand.Rand) string {
	if !strings.Contains(content, "${") {
		return content
	}
	var b strings.Builder
	b.Grow(len(content) + 64)
	i := 0
	for {
		j := strings.Index(content[i:], "${")
		if j < 0 {
			b.WriteString(content[i:])
			break
		}
		b.WriteString(content[i : i+j])
		end := strings.Index(content[i+j:], "}")
		if end < 0 {
			b.WriteString(content[i+j:])
			break
		}
		expr := content[i+j+2 : i+j+end]
		b.WriteString(resolveVar(expr, seq, r))
		i = i + j + end + 1
	}
	return b.String()
}

// resolveVar 解析单个变量表达式
func resolveVar(expr string, seq int64, r *rand.Rand) string {
	expr = strings.TrimSpace(expr)
	switch {
	case expr == "random_ip":
		return randomIPv4(r)
	case expr == "random_time":
		return randomTime(r)
	case expr == "random_port":
		return strconv.Itoa(r.Intn(65535-1024) + 1024)
	case expr == "seq":
		return strconv.FormatInt(seq, 10)
	case expr == "date":
		return time.Now().Format("2006-01-02")
	case expr == "datetime":
		return time.Now().Format("2006-01-02 15:04:05")
	case expr == "timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10)
	case expr == "timestamp_ms":
		return strconv.FormatInt(time.Now().UnixMilli(), 10)
	case expr == "uuid":
		return randomUUID(r)
	case strings.HasPrefix(expr, "random_hex"):
		n := parseLen(expr, 8)
		return randomHex(n, r)
	case strings.HasPrefix(expr, "random_str"):
		n := parseLen(expr, 6)
		return randomStr(n, r)
	case strings.HasPrefix(expr, "random_int"):
		lo, hi := int64(1), int64(1000)
		if parsed, ok := parseRange(expr); ok {
			lo, hi = parsed[0], parsed[1]
		}
		if hi <= lo {
			hi = lo + 1
		}
		return strconv.FormatInt(r.Int63n(hi-lo)+lo, 10)
	default:
		// 未知变量保留原样
		return "${" + expr + "}"
	}
}

// parseLen 解析 fn(len) 形式的单参数长度，缺省回退 def
func parseLen(expr string, def int) int {
	idx := strings.Index(expr, "(")
	if idx < 0 {
		return def
	}
	inner := strings.TrimSuffix(expr[idx+1:], ")")
	inner = strings.TrimSpace(strings.SplitN(inner, ",", 2)[0])
	n, err := strconv.Atoi(inner)
	if err != nil || n <= 0 {
		return def
	}
	if n > 1024 {
		n = 1024
	}
	return n
}

const hexChars = "0123456789abcdef"

func randomHex(n int, r *rand.Rand) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[r.Intn(len(hexChars))]
	}
	return string(b)
}

const letterChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randomStr(n int, r *rand.Rand) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterChars[r.Intn(len(letterChars))]
	}
	return string(b)
}

func randomUUID(r *rand.Rand) string {
	var u [16]byte
	for i := range u {
		u[i] = byte(r.Intn(256))
	}
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// parseRange 解析 random_int(min,max)
func parseRange(expr string) ([2]int64, bool) {
	idx := strings.Index(expr, "(")
	if idx < 0 {
		return [2]int64{}, false
	}
	inner := strings.TrimSuffix(expr[idx+1:], ")")
	parts := strings.Split(inner, ",")
	if len(parts) != 2 {
		return [2]int64{}, false
	}
	lo, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	hi, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err1 != nil || err2 != nil {
		return [2]int64{}, false
	}
	return [2]int64{lo, hi}, true
}

func randomIPv4(r *rand.Rand) string {
	return fmt.Sprintf("%d.%d.%d.%d", r.Intn(254)+1, r.Intn(256), r.Intn(256), r.Intn(254)+1)
}

func randomTime(r *rand.Rand) string {
	// 随机生成近7天的时间戳
	now := time.Now()
	offset := time.Duration(r.Intn(7*24)) * time.Hour
	t := now.Add(-offset)
	return t.Format("Jan 2 15:04:05")
}

func randID(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// SeqCounter 序列号计数器（单会话内递增）
type SeqCounter struct {
	val int64
}

// Next 返回下一序列号
func (s *SeqCounter) Next() int64 {
	return atomic.AddInt64(&s.val, 1)
}

// Reset 重置序列号
func (s *SeqCounter) Reset() {
	atomic.StoreInt64(&s.val, 0)
}
