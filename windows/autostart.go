//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// 开机自启通过 HKCU 下的 Run 键实现，不需要管理员权限。
//
// 之所以手写 advapi32 调用而不是用 golang.org/x/sys/windows/registry，
// 是为了让整个程序只依赖 Go 标准库——这样在任何机器上交叉编译都
// 不必联网拉模块。
const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002

	regSZ = 1

	runKeyPath         = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName = "WorkBuddyStatus"

	errorSuccess = 0
)

// exePath 当前可执行文件的绝对路径
func exePath() string {
	p, err := os.Executable()
	if err != nil || p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

// autostartCommand 写进注册表的那条命令。路径含空格，必须加引号。
func autostartCommand() string {
	p := exePath()
	if p == "" {
		return ""
	}
	return `"` + p + `"`
}

// workbuddyCandidates WorkBuddy 桌面端可能的安装位置
func workbuddyCandidates() []string {
	home := os.Getenv("USERPROFILE")
	var out []string
	add := func(base string, parts ...string) {
		if strings.TrimSpace(base) == "" {
			return
		}
		out = append(out, filepath.Join(append([]string{base}, parts...)...))
	}

	add(os.Getenv("LOCALAPPDATA"), "Programs", "WorkBuddy", "WorkBuddy.exe")
	add(os.Getenv("LOCALAPPDATA"), "WorkBuddy", "WorkBuddy.exe")
	add(os.Getenv("PROGRAMFILES"), "WorkBuddy", "WorkBuddy.exe")
	add(os.Getenv("PROGRAMFILES(X86)"), "WorkBuddy", "WorkBuddy.exe")
	add(home, "AppData", "Local", "Programs", "WorkBuddy", "WorkBuddy.exe")

	// 去掉空项，避免 os.Stat("") 之类无意义调用
	kept := out[:0]
	for _, p := range out {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return kept
}

// openRunKey 打开（必要时创建）Run 键
func openRunKey(access uintptr) (uintptr, error) {
	sub, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return 0, err
	}
	var hKey uintptr
	var disposition uint32
	ret, _, _ := pRegCreateKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(sub)),
		0, 0, 0, // Reserved / lpClass / dwOptions
		access,
		0, // lpSecurityAttributes
		uintptr(unsafe.Pointer(&hKey)),
		uintptr(unsafe.Pointer(&disposition)),
	)
	runtime.KeepAlive(sub)
	if ret != errorSuccess {
		return 0, fmt.Errorf("打开注册表键失败（错误码 %d）", ret)
	}
	return hKey, nil
}

// enableAutostart 写入 Run 键
func enableAutostart() error {
	cmd := autostartCommand()
	if cmd == "" {
		return errors.New("无法确定程序所在路径")
	}

	hKey, err := openRunKey(keySetValue | keyQueryValue)
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(hKey)

	name, err := syscall.UTF16PtrFromString(autostartValueName)
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(cmd)
	if err != nil {
		return err
	}

	ret, _, _ := pRegSetValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(name)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2), // 含结尾 NUL，字节数
	)
	runtime.KeepAlive(name)
	runtime.KeepAlive(data)
	if ret != errorSuccess {
		return fmt.Errorf("写入注册表失败（错误码 %d）", ret)
	}
	return nil
}

// disableAutostart 删除 Run 键里的对应项
func disableAutostart() error {
	hKey, err := openRunKey(keySetValue | keyQueryValue)
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(hKey)

	name, err := syscall.UTF16PtrFromString(autostartValueName)
	if err != nil {
		return err
	}
	ret, _, _ := pRegDeleteValueW.Call(hKey, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)

	const errorFileNotFound = 2
	if ret != errorSuccess && ret != errorFileNotFound {
		return fmt.Errorf("删除注册表项失败（错误码 %d）", ret)
	}
	return nil
}

// autostartEnabled 查询 Run 键里是否已有本程序的项
func autostartEnabled() bool {
	hKey, err := openRunKey(keyQueryValue)
	if err != nil {
		return false
	}
	defer pRegCloseKey.Call(hKey)

	name, err := syscall.UTF16PtrFromString(autostartValueName)
	if err != nil {
		return false
	}
	// 只问类型和长度，不接数据
	var typ uint32
	var size uint32
	ret, _, _ := pRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	runtime.KeepAlive(name)
	return ret == errorSuccess
}
