package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- 显示模式

// DisplayMode 状态栏 / 悬浮窗的显示方式
type DisplayMode int

const (
	// DisplayValue 显示积分数值：⚡ 1494
	DisplayValue DisplayMode = iota
	// DisplayPercent 显示剩余百分比：⚡ 9%
	DisplayPercent
	// DisplayIconOnly 仅显示图标：⚡
	DisplayIconOnly
)

// Label 菜单中展示的名称
func (m DisplayMode) Label() string {
	switch m {
	case DisplayPercent:
		return "显示剩余百分比"
	case DisplayIconOnly:
		return "仅显示图标"
	default:
		return "显示积分数值"
	}
}

// Key 持久化用的字符串键
func (m DisplayMode) Key() string {
	switch m {
	case DisplayPercent:
		return "percent"
	case DisplayIconOnly:
		return "icon"
	default:
		return "value"
	}
}

// ParseDisplayMode 从字符串还原，无法识别时回落到数值模式
func ParseDisplayMode(s string) DisplayMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "percent":
		return DisplayPercent
	case "icon", "icononly", "icon_only":
		return DisplayIconOnly
	default:
		return DisplayValue
	}
}

// ---------------------------------------------------------------- 数值格式化

// Number 千分位格式化：1494 -> "1,494"，1371.48 -> "1,371.48"
func Number(v float64) string {
	decimals := 2
	if math.Abs(v) >= 100 {
		decimals = 0
	}
	return groupThousands(strconv.FormatFloat(v, 'f', decimals, 64))
}

// CompactNumber 紧凑格式，用于空间有限的托盘 / 悬浮窗
func CompactNumber(v float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e8:
		return fmt.Sprintf("%.1f亿", v/1e8)
	case a >= 1e4:
		return fmt.Sprintf("%.2f万", v/1e4)
	case a >= 100:
		return fmt.Sprintf("%.0f", v)
	case a >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

// groupThousands 给整数部分插入千分位分隔符
func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i:]
	}
	var b strings.Builder
	for i, ch := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := b.String() + fracPart
	if neg {
		return "-" + out
	}
	return out
}

// ---------------------------------------------------------------- 状态文本

// DefaultPrefix 托盘图标 / 悬浮窗的默认前缀（闪电符号）
const DefaultPrefix = "⚡ "

// StatusText 生成托盘提示与悬浮窗上的文本
//
// prefix 由配置提供，默认 DefaultPrefix；若系统字体缺少 ⚡ 字形，
// 用户可在 config.json 里把 badgePrefix 改成纯文本（如 "积分 "）。
func StatusText(mode DisplayMode, summary *CreditSummary, prefix string) string {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	if summary == nil {
		return strings.TrimRight(prefix, " ")
	}
	switch mode {
	case DisplayIconOnly:
		return strings.TrimRight(prefix, " ")
	case DisplayPercent:
		return prefix + fmt.Sprintf("%.0f%%", summary.UsageRatio()*100)
	default:
		return prefix + CompactNumber(summary.TotalRemain())
	}
}

// BalanceLine 菜单里那行大字号余额文案
func BalanceLine(summary *CreditSummary) string {
	if summary == nil {
		return "💰 积分获取中…"
	}
	return fmt.Sprintf("💰 剩余 %s / %s", Number(summary.TotalRemain()), Number(summary.TotalCapacity()))
}

// CheckinLine 签到状态文案
func CheckinLine(c *CheckinStatus) string {
	if c == nil || !c.Active {
		return ""
	}
	if c.TodayCheckedIn {
		return fmt.Sprintf("✅ 今日已签到 · 连续 %d 天 · 本周 %d 天", c.StreakDays, c.WeekCheckinDays)
	}
	return fmt.Sprintf("📝 今日未签到 · 签可得 %s 积分", Number(c.DailyCredit))
}

// SummaryText 复制到剪贴板的多行摘要
func SummaryText(accountName string, summary *CreditSummary, lastErr string) string {
	var lines []string
	if accountName != "" {
		lines = append(lines, "账户："+accountName)
	}
	if summary != nil {
		lines = append(lines,
			fmt.Sprintf("剩余积分：%s / %s", Number(summary.TotalRemain()), Number(summary.TotalCapacity())))
		for _, p := range summary.ActivePackages() {
			lines = append(lines, fmt.Sprintf("  - %s: %s / %s",
				PackageDisplayName(p.Code), Number(p.Remain), Number(p.Total)))
		}
	}
	if lastErr != "" {
		lines = append(lines, "错误："+lastErr)
	}
	return strings.Join(lines, "\r\n")
}
