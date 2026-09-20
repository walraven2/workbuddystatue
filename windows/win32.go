//go:build windows

package main

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"workbuddystatus/internal/core"
)

// 剪贴板相关的失败原因，供菜单里弹提示用
var (
	errClipboardOpen  = errors.New("无法打开剪贴板（可能被其它程序占用）")
	errClipboardMem   = errors.New("分配剪贴板内存失败")
	errClipboardWrite = errors.New("写入剪贴板失败")
)

// ---------------------------------------------------------------- DLL 与过程

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
)

var (
	// 窗口
	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pDestroyWindow              = user32.NewProc("DestroyWindow")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pGetClientRect              = user32.NewProc("GetClientRect")
	pInvalidateRect             = user32.NewProc("InvalidateRect")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")

	// 消息循环
	pGetMessageW      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessageW = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pPostMessageW     = user32.NewProc("PostMessageW")
	pSendMessageW     = user32.NewProc("SendMessageW")

	// 鼠标与光标
	pGetCursorPos   = user32.NewProc("GetCursorPos")
	pReleaseCapture = user32.NewProc("ReleaseCapture")
	pSetCapture     = user32.NewProc("SetCapture")
	pLoadCursorW    = user32.NewProc("LoadCursorW")

	// 剪贴板
	pOpenClipboard    = user32.NewProc("OpenClipboard")
	pCloseClipboard   = user32.NewProc("CloseClipboard")
	pEmptyClipboard   = user32.NewProc("EmptyClipboard")
	pSetClipboardData = user32.NewProc("SetClipboardData")

	// 全局内存（剪贴板要用 GMEM_MOVEABLE 分配）
	pGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	pGlobalLock   = kernel32.NewProc("GlobalLock")
	pGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	pGlobalFree   = kernel32.NewProc("GlobalFree")

	// 注册表
	pRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	pRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey      = advapi32.NewProc("RegCloseKey")

	// 菜单
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")

	// 图标
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pDestroyIcon              = user32.NewProc("DestroyIcon")

	// 定时器与对话框
	pSetTimer    = user32.NewProc("SetTimer")
	pKillTimer   = user32.NewProc("KillTimer")
	pMessageBoxW = user32.NewProc("MessageBoxW")

	// 系统度量
	pGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pSetWindowRgn          = user32.NewProc("SetWindowRgn")

	// 绘制
	pBeginPaint            = user32.NewProc("BeginPaint")
	pEndPaint              = user32.NewProc("EndPaint")
	pFillRect              = user32.NewProc("FillRect")
	pDrawTextW             = user32.NewProc("DrawTextW")
	pRoundRect             = gdi32.NewProc("RoundRect")
	pCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	pCreatePen             = gdi32.NewProc("CreatePen")
	pCreateRoundRectRgn    = gdi32.NewProc("CreateRoundRectRgn")
	pGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	pGetGlyphIndicesW      = gdi32.NewProc("GetGlyphIndicesW")
	pGetDeviceCaps         = gdi32.NewProc("GetDeviceCaps")
	pSelectObject          = gdi32.NewProc("SelectObject")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pGetStockObject        = gdi32.NewProc("GetStockObject")
	pSetBkMode             = gdi32.NewProc("SetBkMode")
	pSetTextColor          = gdi32.NewProc("SetTextColor")
	pCreateFontW           = gdi32.NewProc("CreateFontW")

	// 托盘
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pShellExecuteW    = shell32.NewProc("ShellExecuteW")

	// 内核
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")
	pRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")
)

// ---------------------------------------------------------------- 辅助封装

func getModuleHandle() uintptr {
	h, _, _ := pGetModuleHandleW.Call(0)
	return h
}

func createMutex(name string) (uintptr, uint32) {
	p, _ := syscall.UTF16PtrFromString(name)
	runtime.KeepAlive(p)
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(p)))
	// 已存在时 GetLastError 返回 ERROR_ALREADY_EXISTS (183)
	code := uint32(0)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok {
			code = uint32(errno)
		}
	}
	return h, code
}

// appendMenu 往菜单里追加一项；text 为空表示分隔符
func appendMenu(hMenu uintptr, flags uintptr, id uintptr, text string) {
	var p *uint16
	if text != "" {
		p, _ = syscall.UTF16PtrFromString(text)
	}
	pAppendMenuW.Call(hMenu, flags, id, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}

// createIconFromBytes 由 ICO 中的一帧数据创建 HICON
func createIconFromBytes(data []byte, size int) uintptr {
	if len(data) == 0 {
		return 0
	}
	h, _, _ := pCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		1,          // fIcon = TRUE
		0x00030000, // dwVersion
		uintptr(size), uintptr(size),
		0, // LR_DEFAULTCOLOR
	)
	runtime.KeepAlive(data)
	return h
}

// messageBox 弹一个最简提示框
func messageBox(title, text string, flags uintptr) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	runtime.KeepAlive(t)
	runtime.KeepAlive(m)
}

const (
	mbOK              = 0x00000000
	mbIconInformation = 0x00000040
	mbIconWarning     = 0x00000030
	mbIconError       = 0x00000010
	mbSetForeground   = 0x00010000
)

// shellExecute 用系统默认方式打开文件 / 文件夹 / 网址
func shellExecute(verb, target string) error {
	v, _ := syscall.UTF16PtrFromString(verb)
	t, _ := syscall.UTF16PtrFromString(target)
	ret, _, err := pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(t)),
		0, 0, 1 /* SW_SHOWNORMAL */)
	runtime.KeepAlive(v)
	runtime.KeepAlive(t)
	// ShellExecute 成功时返回值 > 32
	if ret <= 32 {
		return err
	}
	return nil
}

// workArea 返回主显示器的工作区（排除任务栏）
func workArea() core.Rect {
	r := core.Rect{}
	pSystemParametersInfoW.Call(core.SPIGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	if r.Right == 0 && r.Bottom == 0 {
		// 兜底：用屏幕尺寸
		w, _, _ := pGetSystemMetrics.Call(core.SMXScreen)
		h, _, _ := pGetSystemMetrics.Call(core.SMYScreen)
		r = core.Rect{Left: 0, Top: 0, Right: int32(w), Bottom: int32(h)}
	}
	runtime.KeepAlive(&r)
	return r
}

// copyUTF16 把 Go 字符串写入固定长度的 UTF-16 缓冲区，并保证以 0 结尾
func copyUTF16(dst []uint16, s string) {
	src, err := syscall.UTF16FromString(s)
	if err != nil {
		// 含 NUL 等非法字符时退化为空串
		src = []uint16{0}
	}
	if len(src) > len(dst) {
		copy(dst, src[:len(dst)-1])
	} else {
		copy(dst, src)
		// 清掉残留
		for i := len(src); i < len(dst); i++ {
			dst[i] = 0
		}
	}
	dst[len(dst)-1] = 0 // 强制 NUL 结尾
}

func loword(v uintptr) uint32 { return uint32(v & 0xffff) }

// getSystemMetrics 读一个系统度量值（失败返回 0）
func getSystemMetrics(index uintptr) int32 {
	v, _, _ := pGetSystemMetrics.Call(index)
	return int32(v)
}

// systemDPI 取主显示器的 DPI（96 = 100%）。
//
// 走 GetDeviceCaps 而不是 GetDpiForSystem：前者从 Windows 2000 起就有，
// 后者是 Win10 1607 才加的。列表式 LazyProc 在函数不存在时会直接 panic，
// 用老 API 就不必为兼容性额外加判断。
func systemDPI() int32 {
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return 96
	}
	defer func() { pReleaseDC.Call(0, hdc) }()

	dpi := int32(0)
	if r, _, _ := pGetDeviceCaps.Call(hdc, core.LOGPIXELSY); r > 0 {
		dpi = int32(r)
	}
	if dpi < 72 || dpi > 480 {
		return 96
	}
	return dpi
}

// ---------------------------------------------------------------- 字形检测

// fontHasGlyph 判断字体里是否有某个字符的字形
//
// 用 GGI_MARK_NONEXISTING_GLYPHS 让 GetGlyphIndicesW 对缺失字形返回 0xFFFF。
// 图标前缀默认是 ⚡（U+26A1），在部分字体下会渲染成「豆腐块」，
// 启动时先探测一次就能避免出现方块。
func fontHasGlyph(font uintptr, r rune) bool {
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return true // 探测不了就当作可用，避免误伤
	}
	defer func() { pReleaseDC.Call(0, hdc) }()

	old, _, _ := pSelectObject.Call(hdc, font)
	defer func() { pSelectObject.Call(hdc, old) }()

	r16 := utf16.Encode([]rune{r})
	if len(r16) == 0 {
		return true
	}
	var glyph uint16
	ret, _, _ := pGetGlyphIndicesW.Call(
		hdc,
		uintptr(unsafe.Pointer(&r16[0])),
		uintptr(len(r16)),
		uintptr(unsafe.Pointer(&glyph)),
		ggiMarkNonExistingGlyphs,
	)
	runtime.KeepAlive(r16)
	if ret == 0 {
		return true
	}
	return glyph != 0xffff
}

// ---------------------------------------------------------------- 剪贴板

const (
	cfUnicodeText            = 13
	gmemMoveable             = 0x0002
	ggiMarkNonExistingGlyphs = 0x0001
)

// setClipboardText 把文本写进剪贴板
//
// 剪贴板是全局独占资源，别的程序（输入法、剪贴板管理器）可能正占着，
// 因此失败时短暂重试几次再放弃。
func setClipboardText(owner uintptr, text string) error {
	var opened bool
	for attempt := 0; attempt < 5; attempt++ {
		if r, _, _ := pOpenClipboard.Call(owner); r != 0 {
			opened = true
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !opened {
		return errClipboardOpen
	}
	defer func() { pCloseClipboard.Call() }()

	pEmptyClipboard.Call()

	u16, err := syscall.UTF16FromString(strings.ReplaceAll(text, "\n", "\r\n"))
	if err != nil {
		return err
	}
	hMem, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(u16)*2))
	if hMem == 0 {
		return errClipboardMem
	}
	ptr, _, _ := pGlobalLock.Call(hMem)
	if ptr == 0 {
		pGlobalFree.Call(hMem)
		return errClipboardMem
	}
	// 用 RtlMoveMemory 把 Go 切片的内容搬进系统分配的全局内存。
	//
	// 这里刻意不走 (unsafe.Pointer)(uintptr) 那条路：GlobalLock 给的是裸地址，
	// 转换回 Go 指针会绕过 GC 的安全保证。交给系统函数拷贝则只需要
	// 「Go 指针 -> uintptr」这个允许的方向。
	if len(u16) > 0 {
		pRtlMoveMemory.Call(ptr,
			uintptr(unsafe.Pointer(&u16[0])),
			uintptr(len(u16)*2))
		runtime.KeepAlive(u16)
	}
	pGlobalUnlock.Call(hMem)

	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, hMem); r == 0 {
		pGlobalFree.Call(hMem)
		return errClipboardWrite
	}
	// SetClipboardData 成功后内存所有权归系统，不能再 GlobalFree
	return nil
}

// ---------------------------------------------------------------- 窗口

// registerWindowClass 注册窗口类；已注册时视为成功
func registerWindowClass(name string) error {
	className, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	cursor, _, _ := pLoadCursorW.Call(0, core.IDCArrow)

	wc := core.WndClassEx{
		CbSize:        uint32(unsafe.Sizeof(core.WndClassEx{})),
		LpfnWndProc:   wndProcCallback,
		HInstance:     getModuleHandle(),
		HCursor:       cursor,
		LpszClassName: className,
	}
	atom, _, callErr := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	runtime.KeepAlive(&wc)
	runtime.KeepAlive(className)

	const errorClassAlreadyExists = 1410
	if atom == 0 && callErr != nil {
		if errno, ok := callErr.(syscall.Errno); ok && uint32(errno) == errorClassAlreadyExists {
			return nil
		}
		return callErr
	}
	return nil
}

func createWindow(exStyle uintptr, className, title string, style uintptr,
	x, y, w, h int32, parent uintptr) uintptr {

	cn, _ := syscall.UTF16PtrFromString(className)
	tn, _ := syscall.UTF16PtrFromString(title)

	hwnd, _, _ := pCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(cn)),
		uintptr(unsafe.Pointer(tn)),
		style,
		uintptr(uint32(x)), uintptr(uint32(y)),
		uintptr(uint32(w)), uintptr(uint32(h)),
		parent, 0, getModuleHandle(), 0,
	)
	runtime.KeepAlive(cn)
	runtime.KeepAlive(tn)
	return hwnd
}

// ---------------------------------------------------------------- 绘制辅助

// measureText 用指定字体测量文本像素尺寸；失败时回落到粗略估算
func measureText(text string, font uintptr) core.Size {
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return estimateTextSize(text)
	}
	defer func() { pReleaseDC.Call(0, hdc) }()

	old, _, _ := pSelectObject.Call(hdc, font)
	defer func() { pSelectObject.Call(hdc, old) }()

	u16 := utf16.Encode([]rune(text))
	if len(u16) == 0 {
		return core.Size{}
	}
	var size core.Size
	ret, _, _ := pGetTextExtentPoint32W.Call(
		hdc,
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)),
		uintptr(unsafe.Pointer(&size)),
	)
	runtime.KeepAlive(u16)
	if ret == 0 || size.CX <= 0 {
		return estimateTextSize(text)
	}
	return size
}

// estimateTextSize 在测量失败时的兜底宽度估算
func estimateTextSize(text string) core.Size {
	var w int32
	for _, r := range text {
		switch {
		case r < 0x80: // ASCII
			w += 8
		case r <= 0xffff: // CJK 等宽字符
			w += 14
		default: // emoji 等
			w += 16
		}
	}
	if w <= 0 {
		w = 40
	}
	return core.Size{CX: w, CY: 18}
}

// setRoundedRegion 把窗口裁剪成圆角矩形；失败时保持原样（不影响可用性）
func setRoundedRegion(hwnd uintptr, w, h, radius int32) {
	rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1),
		uintptr(radius*2), uintptr(radius*2))
	if rgn == 0 {
		return
	}
	// SetWindowRgn 成功后区域归系统所有，不能再 DeleteObject
	pSetWindowRgn.Call(hwnd, rgn, 1)
}

// colorRef 把 RGB 组装成 Win32 的 COLORREF（0x00BBGGRR）
func colorRef(r, g, b uint8) uintptr {
	return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16)
}
