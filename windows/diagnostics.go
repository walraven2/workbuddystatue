//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"workbuddystatus/internal/core"
)

// runDiagnostics 生成一份纯文本诊断报告，供 --check / --diagnose 使用。
//
// 这个模式刻意不碰任何窗口：只需要在目标机器上跑一次 exe，
// 就能确认凭据路径、登录态与接口连通性到底卡在哪一步。
func runDiagnostics() string {
	var b strings.Builder
	line := func(format string, args ...any) {
		b.WriteString(fmt.Sprintf(format, args...))
		b.WriteString("\r\n")
	}

	cfg := core.LoadConfig()

	line("WorkBuddy 积分托盘小工具 · 诊断报告")
	line("========================================")
	line("生成时间：%s（北京时间）", time.Now().Format("2006-01-02 15:04:05"))
	line("程序版本：%s", appVersion)
	line("运行环境：%s/%s · Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	line("可执行文件：%s", exePath())
	line("")

	// ── 配置
	line("【配置】")
	line("  配置文件：%s", core.ConfigPath())
	if _, err := os.Stat(core.ConfigPath()); err == nil {
		line("  状态：已存在")
	} else {
		line("  状态：不存在（使用默认值）")
	}
	line("  接口地址：%s", cfg.Endpoint)
	line("  刷新间隔：%s", intervalLabel(cfg.RefreshInterval))
	line("  显示方式：%s", cfg.DisplayMode.Label())
	line("  悬浮窗：%s", onOff(cfg.ShowBadge))
	if cfg.AccessToken != "" {
		line("  令牌来源：配置文件 / 环境变量（%s）", maskToken(cfg.AccessToken))
	}
	line("")

	// ── 凭据目录
	line("【凭据目录】")
	for _, dir := range core.CandidateAuthDirs() {
		state := "不存在"
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				if !e.IsDir() {
					names = append(names, e.Name())
				}
			}
			if len(names) == 0 {
				state = "目录存在但没有文件"
			} else {
				state = "发现 " + strings.Join(names, ", ")
			}
		}
		line("  %s — %s", dir, state)
	}
	line("")

	// ── 登录状态
	line("【登录状态】")
	cred, err := core.LoadCredential(cfg)
	if err != nil {
		e := core.AsError(err)
		line("  ✗ %s", e.UserMessage())
		line("    错误类型：%s", e.Kind)
		line("")
		line("【结论】")
		line("  未能取得凭据，后面的接口测试已跳过。")
		line("  请先打开并登录 WorkBuddy 桌面端，然后重新运行本诊断。")
		return b.String()
	}

	line("  ✓ 已取得访问令牌")
	line("    来源：%s", cred.Source)
	line("    账户：%s", cred.DisplayName())
	if cred.UserID != "" {
		line("    用户 ID：%s", cred.UserID)
	}
	line("    令牌：%s", maskToken(cred.AccessToken))
	if cred.HasExpiry {
		left := time.Until(cred.ExpiresAt)
		line("    有效期至：%s（剩余 %s）",
			cred.ExpiresAt.Local().Format("2006-01-02 15:04:05"), humanDuration(left))
	} else {
		line("    有效期：未提供")
	}
	line("")

	client := core.NewClient(20 * time.Second)
	client.Endpoint = cfg.Endpoint

	// ── 连通性
	line("【接口连通性】")
	status, elapsed, perr := client.ProbeEndpoint(cred)
	if perr != nil {
		line("  ✗ 请求失败：%s", core.AsError(perr).UserMessage())
	} else {
		line("  ✓ HTTP %d · 耗时 %d ms", status, elapsed.Milliseconds())
		if status == 401 || status == 403 {
			line("    → 令牌已被服务端拒绝，请在 WorkBuddy 中重新登录")
		}
	}
	line("")

	// ── 数据
	line("【积分数据】")
	summary, serr := client.FetchSummary(cred)
	if serr != nil {
		line("  ✗ %s", core.AsError(serr).UserMessage())
	} else {
		line("  ✓ 剩余 %s / %s（已用 %s，使用率 %.1f%%）",
			core.Number(summary.TotalRemain()), core.Number(summary.TotalCapacity()),
			core.Number(summary.TotalUsed()), summary.UsageRatio()*100)
		line("    付费用户：%s", onOff(summary.IsPaidUser))
		if summary.SubscriptionCode != "" {
			line("    订阅类型：%s", core.PackageDisplayName(summary.SubscriptionCode))
		}
		line("    明细：")
		for _, p := range summary.ActivePackages() {
			line("      - %s：%s / %s %s",
				core.PackageDisplayName(p.Code), core.Number(p.Remain), core.Number(p.Total), p.Unit)
		}
		for _, p := range summary.ExhaustedPackages() {
			line("      - %s：已用尽（%s）", core.PackageDisplayName(p.Code), core.Number(p.Total))
		}
	}
	line("")

	line("【签到状态】")
	checkin, cerr := client.FetchCheckin(cred)
	switch {
	case cerr != nil:
		line("  ✗ %s", core.AsError(cerr).UserMessage())
	case checkin == nil || !checkin.Active:
		line("  本次活动未开放")
	default:
		line("  今日已签到：%s", onOff(checkin.TodayCheckedIn))
		line("  连续签到：%d 天 · 本周 %d 天", checkin.StreakDays, checkin.WeekCheckinDays)
		line("  每日可得：%s · 今日已得 %s",
			core.Number(checkin.DailyCredit), core.Number(checkin.TodayCredit))
	}
	line("")

	line("【结论】")
	if serr == nil {
		line("  一切正常。直接双击 exe 即可在托盘看到积分。")
	} else {
		line("  凭据已就绪，但拉取积分失败，请对照上面的错误信息处理。")
	}
	line("")
	line("运行日志：%s", core.LogPath())
	return b.String()
}

// ---------------------------------------------------------------- 小工具

// maskToken 只保留头尾各若干字符，方便比对又不至于把令牌整条写进报告
func maskToken(tok string) string {
	n := len(tok)
	if n <= 12 {
		return strings.Repeat("*", n)
	}
	return fmt.Sprintf("%s…%s（共 %d 字符）", tok[:6], tok[n-4:], n)
}

func onOff(v bool) string {
	if v {
		return "已开启"
	}
	return "已关闭"
}

// intervalLabel 把秒数翻译成中文；0 表示仅手动刷新
func intervalLabel(seconds int) string {
	if seconds <= 0 {
		return "仅手动刷新"
	}
	if seconds%3600 == 0 {
		return fmt.Sprintf("每 %d 小时", seconds/3600)
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("每 %d 分钟", seconds/60)
	}
	return fmt.Sprintf("每 %d 秒", seconds)
}

// humanDuration 把剩余时长的精度收敛到「天/小时/分钟」
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "已过期"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d 天", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d 小时", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
}
