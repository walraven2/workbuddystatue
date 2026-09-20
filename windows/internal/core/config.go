package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultEndpoint 接口前缀，与 WorkBuddy 桌面端 product.json 保持一致
const DefaultEndpoint = "https://copilot.tencent.com"

// ---------------------------------------------------------------- 路径

// HomeDir 用户主目录（Windows 下即 %USERPROFILE%）
func HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("USERPROFILE")
}

// ConfigDir 本工具的配置目录
func ConfigDir() string { return filepath.Join(HomeDir(), ".workbuddy-status") }

// ConfigPath 配置文件路径
func ConfigPath() string { return filepath.Join(ConfigDir(), "config.json") }

// LogPath 运行日志路径
func LogPath() string { return filepath.Join(ConfigDir(), "last-error.log") }

// CheckReportPath 诊断报告输出路径
func CheckReportPath() string { return filepath.Join(ConfigDir(), "last-check.txt") }

// ---------------------------------------------------------------- 配置

// Config 运行配置。字段与 macOS 版共用同一份 config.json，
// 在此基础上增加了 Windows 专属项。
type Config struct {
	Endpoint        string
	AccessToken     string
	UserID          string
	RefreshInterval int // 秒；0 表示仅手动刷新
	DisplayMode     DisplayMode
	BadgePrefix     string // 悬浮窗前缀，默认 "⚡ "
	ShowBadge       bool   // 是否显示桌面悬浮窗
	BadgeX          int
	BadgeY          int
	HasBadgePos     bool // 是否已有保存过的悬浮窗坐标
}

// DefaultConfig 返回带默认值的配置
func DefaultConfig() Config {
	return Config{
		Endpoint:        DefaultEndpoint,
		RefreshInterval: 300,
		DisplayMode:     DisplayValue,
		BadgePrefix:     DefaultPrefix,
		ShowBadge:       true,
	}
}

// LoadConfig 读取配置文件并叠加环境变量覆盖
func LoadConfig() Config {
	cfg := DefaultConfig()

	if raw := readRawConfig(); raw != nil {
		if v := rawString(raw, "endpoint"); v != "" {
			cfg.Endpoint = v
		}
		cfg.AccessToken = rawString(raw, "accessToken")
		cfg.UserID = rawString(raw, "userId")
		if v, ok := rawNumber(raw, "refreshInterval"); ok {
			cfg.RefreshInterval = int(v)
		}
		if v := rawString(raw, "displayMode"); v != "" {
			cfg.DisplayMode = ParseDisplayMode(v)
		}
		if v := rawString(raw, "badgePrefix"); v != "" {
			cfg.BadgePrefix = v
		}
		if v, ok := rawBool(raw, "showBadge"); ok {
			cfg.ShowBadge = v
		}
		if x, okx := rawNumber(raw, "badgeX"); okx {
			if y, oky := rawNumber(raw, "badgeY"); oky {
				cfg.BadgeX, cfg.BadgeY, cfg.HasBadgePos = int(x), int(y), true
			}
		}
	}

	if v := os.Getenv("WORKBUDDY_ENDPOINT"); v != "" {
		cfg.Endpoint = v
	}
	if v := os.Getenv("WORKBUDDY_ACCESS_TOKEN"); v != "" {
		cfg.AccessToken = v
	}
	if v := os.Getenv("WORKBUDDY_USER_ID"); v != "" {
		cfg.UserID = v
	}

	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.RefreshInterval < 0 {
		cfg.RefreshInterval = 0
	}
	return cfg
}

// UpdateConfig 以「读-改-写」方式更新配置，保留文件中的其它键
// （包括 macOS 版写入的 "//" 注释键）
func UpdateConfig(patch map[string]any) error {
	configMu.Lock()
	defer configMu.Unlock()

	raw := readRawConfig()
	if raw == nil {
		raw = map[string]any{}
	}
	for k, v := range patch {
		raw[k] = v
	}
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), buf, 0o644)
}

// SaveBadgePosition 记录悬浮窗坐标
func SaveBadgePosition(x, y int) error {
	return UpdateConfig(map[string]any{"badgeX": x, "badgeY": y})
}

// WriteTemplateIfNeeded 首次运行时生成一份带说明的示例配置
func WriteTemplateIfNeeded() {
	if _, err := os.Stat(ConfigPath()); err == nil {
		return
	}
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return
	}
	template := `{
  "//": "WorkBuddy 积分托盘小工具配置。accessToken / userId 留空时会自动从 WorkBuddy 桌面端读取，通常无需填写。",
  "endpoint": "` + DefaultEndpoint + `",
  "accessToken": "",
  "userId": "",
  "//refreshInterval": "自动刷新间隔（秒），0 表示仅手动刷新；也可在托盘菜单里改",
  "refreshInterval": 300,
  "//displayMode": "value=显示数值 / percent=显示百分比 / icon=仅显示图标",
  "displayMode": "value",
  "//badgePrefix": "桌面悬浮窗文字前缀。若闪电符号显示成方块，改成 \"积分 \" 即可",
  "badgePrefix": "` + DefaultPrefix + `",
  "//showBadge": "是否启用桌面悬浮窗（在托盘图标旁常驻显示积分）",
  "showBadge": true
}
`
	_ = os.WriteFile(ConfigPath(), []byte(template), 0o644)
}

// ---------------------------------------------------------------- 内部辅助

var configMu sync.Mutex

func readRawConfig() map[string]any {
	buf, err := os.ReadFile(ConfigPath())
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil
	}
	return out
}

func rawString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func rawNumber(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	if v, ok := m[key].(float64); ok {
		return v, true
	}
	return 0, false
}

func rawBool(m map[string]any, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	if v, ok := m[key].(bool); ok {
		return v, true
	}
	return false, false
}

// ---------------------------------------------------------------- 日志

var logMu sync.Mutex

// Log 追加一行运行日志（同时输出到 stdout，便于调试）
func Log(format string, args ...any) {
	line := fmt.Sprintf("[%s] %s\r\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))

	logMu.Lock()
	defer logMu.Unlock()

	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// TrimLogIfNeeded 日志超过 512KB 时只保留最后 300 行，避免无限增长
func TrimLogIfNeeded() {
	info, err := os.Stat(LogPath())
	if err != nil || info.Size() < 512*1024 {
		return
	}
	buf, err := os.ReadFile(LogPath())
	if err != nil {
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n")
	if len(lines) > 300 {
		lines = lines[len(lines)-300:]
	}
	_ = os.WriteFile(LogPath(), []byte(strings.Join(lines, "\r\n")), 0o644)
}
