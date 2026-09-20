//go:build windows

// WorkBuddyStatus —— 在 Windows 托盘与桌面悬浮窗显示 WorkBuddy 积分余额。
//
// 纯 Go 标准库实现，不依赖任何第三方模块，也不需要 cgo：
// 所有 Win32 调用都通过 syscall.NewLazyDLL 直接完成，因此可以在
// macOS / Linux 上交叉编译出 exe。
//
// 与 macOS 版共享同一套接口与凭据来源，仅 UI 层不同。
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"workbuddystatus/internal/core"
)

const (
	appTitle = "WorkBuddy 积分"

	// appVersion 与 macOS 版 release 保持一致
	appVersion = "1.2.0"

	// 窗口消息（WM_APP 段，用于托盘回调与刷新完成通知）
	msgTrayCallback = core.WMApp + 1
	msgRefreshDone  = core.WMApp + 2

	hiddenClassName = "WorkBuddyStatusHiddenWnd"
	badgeClassName  = "WorkBuddyStatusBadgeWnd"

	timerRefresh = 1

	// 单实例互斥体。刻意不用 WorkBuddy 自己的 win32MutexName（"workbuddy"）
	// 以免与桌面端互相干扰。
	selfMutexName = `Local\WorkBuddyStatusTray_SingleInstance`

	// AttachConsole(ATTACH_PARENT_PROCESS)
	attachParentProcess = ^uintptr(0)
)

// app 全局单例，供窗口过程访问
var app *App

// mutexHandle 持有单实例互斥体，保证进程存活期间不被关闭
var mutexHandle uintptr

func main() {
	// Win32 消息循环要求固定在同一个 OS 线程上
	runtime.LockOSThread()

	defer func() {
		if r := recover(); r != nil {
			core.Log("严重错误: %v", r)
			messageBox(appTitle, fmt.Sprintf("程序遇到错误：\n%v\n\n详情见日志：\n%s", r, core.LogPath()), mbOK|mbIconError)
		}
	}()

	core.WriteTemplateIfNeeded()
	core.TrimLogIfNeeded()

	if handleCLI() {
		return
	}

	if err := ensureSingleInstance(); err != nil {
		messageBox(appTitle, "WorkBuddyStatus 已经在运行了。\n\n请在任务栏右下角（可能收在折叠区）查看托盘图标。",
			mbOK|mbIconInformation)
		return
	}

	core.Log("启动 WorkBuddyStatus")
	core.Log("凭据目录候选: %s", strings.Join(core.CandidateAuthDirs(), " | "))

	app = newApp()
	if err := app.init(); err != nil {
		core.Log("初始化失败: %v", err)
		messageBox(appTitle, "初始化失败：\n"+err.Error(), mbOK|mbIconError)
		return
	}

	app.run()
}

// ensureSingleInstance 创建命名互斥体；已存在则返回错误
func ensureSingleInstance() error {
	p, err := syscall.UTF16PtrFromString(selfMutexName)
	if err != nil {
		return nil // 名字异常时不阻断启动
	}
	runtime.KeepAlive(p)

	h, _, callErr := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(p)))
	mutexHandle = h

	const errorAlreadyExists = 183
	if callErr != nil {
		if errno, ok := callErr.(syscall.Errno); ok && uint32(errno) == errorAlreadyExists {
			return fmt.Errorf("单实例互斥体已存在")
		}
	}
	return nil
}

// ---------------------------------------------------------------- 命令行

// handleCLI 处理不需要界面的命令行模式，返回 true 表示已处理完毕可以退出
func handleCLI() bool {
	args := os.Args[1:]
	has := func(flag string) bool {
		for _, a := range args {
			if strings.EqualFold(a, flag) {
				return true
			}
		}
		return false
	}

	switch {
	case has("--check"), has("--diagnose"):
		emitReport(runDiagnostics())
		return true

	case has("--enable-autostart"):
		if err := enableAutostart(); err != nil {
			emit("开启失败：" + err.Error())
			return true
		}
		emit("已开启开机自启 → " + autostartCommand())
		return true

	case has("--disable-autostart"):
		if err := disableAutostart(); err != nil {
			emit("关闭失败：" + err.Error())
			return true
		}
		emit("已关闭开机自启")
		return true

	case has("--help"), has("-h"), has("/?"):
		emit(helpText())
		return true
	}
	return false
}

func helpText() string {
	return strings.Join([]string{
		"WorkBuddy 积分托盘小工具 v" + appVersion,
		"",
		"用法：WorkBuddyStatus.exe [选项]",
		"",
		"  （无参数）              启动托盘图标，并在桌面右上角显示积分悬浮窗",
		"  --check, --diagnose     无界面诊断：打印凭据、令牌与接口连通性",
		"  --enable-autostart      开启开机自启（写入 HKCU 的 Run 键）",
		"  --disable-autostart     关闭开机自启",
		"  --help                  显示本帮助",
		"",
		"配置目录：" + core.ConfigDir(),
		"  config.json       可改刷新间隔、显示方式、悬浮窗前缀与位置",
		"  last-check.txt    诊断报告",
		"  last-error.log    运行日志",
	}, "\r\n")
}

// emitReport 把诊断报告输出到控制台（若已附加）并写入文件
func emitReport(text string) {
	if err := os.MkdirAll(core.ConfigDir(), 0o755); err == nil {
		_ = os.WriteFile(core.CheckReportPath(), []byte(text), 0o644)
	}
	emit("诊断完成，报告已写入：\r\n" + core.CheckReportPath() + "\r\n\r\n" + text)
}

// emit 输出一段文字：优先写到父进程的控制台，没有控制台就弹窗。
//
// 程序是 GUI 子系统（-H windowsgui），双击运行时本来就没有控制台，
// 所以命令行选项必须有弹窗兜底，否则用户会以为程序什么都没做。
func emit(text string) {
	if emitLine(text) {
		return
	}
	messageBox(appTitle, clipForBox(text), mbOK|mbIconInformation)
}

// clipForBox 消息框放不下太长内容，超长只留开头并提示看文件
func clipForBox(text string) string {
	const limit = 3000
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\r\n\r\n（内容过长已截断，完整内容见 " + core.CheckReportPath() + "）"
}

// emitLine 尝试写一行到父进程的控制台，返回是否成功
func emitLine(text string) bool {
	pAttach := kernel32.NewProc("AttachConsole")
	ret, _, _ := pAttach.Call(attachParentProcess)
	if ret == 0 {
		return false
	}

	name, err := syscall.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return false
	}
	runtime.KeepAlive(name)

	pCreateFile := kernel32.NewProc("CreateFileW")
	const (
		genericWrite = 0x40000000
		openExisting = 3
	)
	h, _, _ := pCreateFile.Call(
		uintptr(unsafe.Pointer(name)),
		genericWrite,
		0x00000003, // FILE_SHARE_READ | FILE_SHARE_WRITE
		0,
		openExisting,
		0x00000080, // FILE_ATTRIBUTE_NORMAL
		0,
	)
	if h == ^uintptr(0) || h == 0 {
		return false
	}

	f := os.NewFile(h, "CONOUT$")
	if f == nil {
		return false
	}
	defer f.Close()

	_, _ = f.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
	_, _ = f.WriteString("\r\n")
	return true
}
