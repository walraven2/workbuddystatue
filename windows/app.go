//go:build windows

package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"workbuddystatus/internal/core"
)

// 内置图标。用 go:embed 打进 exe，运行时不再依赖外部文件。
//
//go:embed Resources/AppIcon.ico
var iconData []byte

// ---------------------------------------------------------------- 常量

const (
	trayIconID = 1

	badgeAlpha = 246

	// 配色（深色 HUD 风格，任何壁纸下都清晰）
	badgeBgR, badgeBgG, badgeBgB             = 32, 34, 38
	badgeBdR, badgeBdG, badgeBdB             = 62, 66, 74
	badgeFgR, badgeFgG, badgeFgB             = 243, 245, 249
	badgeWarnFgR, badgeWarnFgG, badgeWarnFgB = 255, 176, 96
)

// 菜单命令 ID。全部集中在这里，方便对照。
const (
	cmdRefresh         = 1001 // 立即刷新
	cmdOpenApp         = 1002 // 打开 WorkBuddy
	cmdOpenConfig      = 1003 // 打开配置目录
	cmdCopy            = 1004 // 复制余额明细
	cmdToggleBadge     = 1005 // 显示 / 隐藏桌面悬浮窗
	cmdToggleAutostart = 1006 // 开机自启开关
	cmdQuit            = 1007 // 退出

	cmdIntervalBase = 1100 // 1100 + 选项下标
	cmdDisplayBase  = 1200 // 1200 + core.DisplayMode
	cmdPackageBase  = 1300 // 1300 + 套餐下标（仅用于展示）
)

// intervalChoices 自动刷新间隔选项
var intervalChoices = []struct {
	Label   string
	Seconds int
}{
	{"仅手动刷新", 0},
	{"每 1 分钟", 60},
	{"每 5 分钟", 300},
	{"每 15 分钟", 900},
	{"每 30 分钟", 1800},
	{"每 1 小时", 3600},
}

var displayModes = []core.DisplayMode{
	core.DisplayValue,
	core.DisplayPercent,
	core.DisplayIconOnly,
}

// ---------------------------------------------------------------- 窗口过程

// wndProcCallback 必须保存在包级变量里。syscall.NewCallback 生成的是
// 一段可被系统调用的代码，如果只把它存在局部变量里，随时可能被 GC 回收，
// 之后系统回调进来就会直接崩溃。
var wndProcCallback uintptr

func init() {
	wndProcCallback = syscall.NewCallback(wndProc)
}

func defWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// wndProc 隐藏窗口与悬浮窗共用的窗口过程，按 hwnd 区分归属
func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	a := app
	if a == nil {
		return defWindowProc(hwnd, msg, wParam, lParam)
	}

	switch msg {
	case core.WMDestroy:
		// 只有隐藏窗口负责结束消息循环
		if hwnd == a.hwndHidden {
			pPostQuitMessage.Call(0)
		}
		return 0
	case core.WMCommand:
		a.handleCommand(uint32(loword(wParam)))
		return 0
	case core.WMTimer:
		a.refreshAsync()
		return 0
	case msgTrayCallback:
		a.handleTrayCallback(lParam)
		return 0
	case msgRefreshDone:
		a.applyBadge()
		return 0
	}

	if hwnd == a.hwndBadge {
		switch msg {
		case core.WMEraseBkgnd:
			// 自己整块重绘，跳过系统擦背景以免闪烁
			return 1
		case core.WMPaint:
			a.paintBadge(hwnd)
			return 0
		case core.WMLButtonDown:
			a.beginDrag()
			return 0
		case core.WMMouseMove:
			a.dragMove()
			return 0
		case core.WMLButtonUp:
			a.endDrag()
			return 0
		case core.WMRButtonUp, core.WMContextMenu:
			a.showMenu()
			return 0
		}
	}

	return defWindowProc(hwnd, msg, wParam, lParam)
}

// ---------------------------------------------------------------- App

// App 托盘应用的全部状态
type App struct {
	mu sync.Mutex

	// 窗口与 GDI 资源
	hwndHidden uintptr
	hwndBadge  uintptr
	hIcon      uintptr
	fontBadge  uintptr
	fontMenu   uintptr

	// 按当前 DPI 换算好的界面尺寸
	m core.Metrics

	cfg    core.Config
	client *core.Client

	// 最近一次成功拉取的结果
	cred        *core.Credential
	summary     *core.CreditSummary
	checkin     *core.CheckinStatus
	lastErr     string
	lastKind    string
	lastUpdated time.Time

	isLoading bool

	displayText string
	prefix      string

	// 悬浮窗状态
	shown      bool
	positioned bool
	badgeW     int32
	badgeX     int32
	badgeY     int32
	dragging   bool
	dragDX     int32
	dragDY     int32

	timerRunning bool
}

func newApp() *App {
	m := core.ComputeMetrics(systemDPI())
	return &App{
		m:      m,
		badgeW: m.MinW,
	}
}

// init 建好窗口、字体、图标与托盘，然后立刻拉一次数据
func (a *App) init() error {
	a.cfg = core.LoadConfig()
	a.client = core.NewClient(20 * time.Second)
	a.client.Endpoint = a.cfg.Endpoint

	core.Log("DPI 缩放基准：%d DPI，悬浮窗高度 %d px，字号 %d px",
		systemDPI(), a.m.Height, a.m.FontPx)

	// 字体要先建好，前缀字形探测需要用到
	a.createFonts()
	a.prefix = a.resolvePrefix(a.cfg.BadgePrefix)

	if err := registerWindowClass(hiddenClassName); err != nil {
		return fmt.Errorf("注册隐藏窗口类失败：%w", err)
	}
	if err := registerWindowClass(badgeClassName); err != nil {
		return fmt.Errorf("注册悬浮窗窗口类失败：%w", err)
	}

	a.hwndHidden = createWindow(0, hiddenClassName, appTitle,
		core.WSOverlapped, 0, 0, 0, 0, 0)
	if a.hwndHidden == 0 {
		return errors.New("创建隐藏窗口失败")
	}

	a.loadIcon()

	a.hwndBadge = createWindow(
		core.WSEXTopmost|core.WSEXLayered|core.WSEXToolWindow|core.WSEXNoActivate,
		badgeClassName, appTitle, core.WSPopup,
		0, 0, a.badgeW, a.m.Height, 0)
	if a.hwndBadge == 0 {
		return errors.New("创建悬浮窗失败")
	}
	pSetLayeredWindowAttributes.Call(a.hwndBadge, 0, badgeAlpha, core.LWAAlpha)

	if err := a.addTrayIcon(); err != nil {
		return fmt.Errorf("添加托盘图标失败：%w", err)
	}

	if a.cfg.ShowBadge {
		// applyBadge 内部会摆好位置并按需显示
		a.applyBadge()
	}

	a.applyTimer()
	a.refreshAsync()
	return nil
}

// run 消息循环；GetMessage 返回 0 表示收到 WM_QUIT
func (a *App) run() {
	var msg core.Msg
	for {
		ret, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		code := int32(ret)
		if code <= 0 { // 0 = WM_QUIT，-1 = 出错
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		runtime.KeepAlive(&msg)
	}
	a.cleanup()
}

func (a *App) cleanup() {
	a.stopTimer()
	a.removeTrayIcon()
	if a.hIcon != 0 {
		pDestroyIcon.Call(a.hIcon)
		a.hIcon = 0
	}
	if a.hwndBadge != 0 {
		pDestroyWindow.Call(a.hwndBadge)
		a.hwndBadge = 0
	}
	if a.hwndHidden != 0 {
		pDestroyWindow.Call(a.hwndHidden)
		a.hwndHidden = 0
	}
	a.destroyFonts()
	core.Log("已退出")
}

// ---------------------------------------------------------------- 字体

func (a *App) createFonts() {
	a.fontBadge = createFont(-a.m.FontPx, core.FWMedium)
	a.fontMenu = createFont(-a.m.FontMenu, core.FWNormal)
}

func (a *App) destroyFonts() {
	if a.fontBadge != 0 {
		pDeleteObject.Call(a.fontBadge)
		a.fontBadge = 0
	}
	if a.fontMenu != 0 {
		pDeleteObject.Call(a.fontMenu)
		a.fontMenu = 0
	}
}

// createFont 建一个字体；失败时回落到系统默认 GUI 字体
func createFont(px, weight int32) uintptr {
	face, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	h, _, _ := pCreateFontW.Call(
		uintptr(uint32(px)), // 负值表示字符高度（逻辑单位）
		0,                   // 宽度自适应
		0, 0,                // 倾斜 / 书写方向
		uintptr(weight),
		0, 0, 0, // 斜体 / 下划线 / 删除线
		1, // DEFAULT_CHARSET
		0, // OUT_DEFAULT_PRECIS
		0, // CLIP_DEFAULT_PRECIS
		5, // CLEARTYPE_QUALITY
		0, // DEFAULT_PITCH | FF_DONTCARE
		uintptr(unsafe.Pointer(face)),
	)
	runtime.KeepAlive(face)
	if h == 0 {
		h, _, _ = pGetStockObject.Call(core.DEFAULTGUIFONT)
	}
	return h
}

// resolvePrefix 校验前缀的首个可见字符在当前字体里是否有字形。
//
// ⚡（U+26A1）在部分中文字体里缺失，会渲染成方框。探测到缺失就换成
// 「积分 」，宁可朴素一点也不要有方块。
func (a *App) resolvePrefix(want string) string {
	if strings.TrimSpace(want) == "" {
		want = core.DefaultPrefix
	}
	for _, r := range want {
		if r == ' ' {
			continue
		}
		if fontHasGlyph(a.fontBadge, r) {
			return want
		}
		core.Log("字体缺少字形 %q，前缀回退为「积分 」", string(r))
		return "积分 "
	}
	return want
}

// ---------------------------------------------------------------- 图标

func (a *App) loadIcon() {
	images, err := core.ParseICO(iconData)
	if err != nil {
		core.Log("解析内置图标失败: %v", err)
		return
	}
	// 托盘图标按系统小图标尺寸取帧；取不到就退回 32
	size := int(getSystemMetrics(core.SMCXSmIcon))
	if size <= 0 {
		size = 32
	}
	if img, ok := core.PickBMP(images, size); ok {
		a.hIcon = createIconFromBytes(img.Data, img.Width)
	}
	if a.hIcon == 0 {
		if img, ok := core.PickBMP(images, 32); ok {
			a.hIcon = createIconFromBytes(img.Data, img.Width)
		}
	}
	if a.hIcon == 0 {
		core.Log("创建托盘图标失败，托盘将没有图标")
	}
}

// ---------------------------------------------------------------- 托盘

func (a *App) nid() core.NotifyIconData {
	return core.NotifyIconData{
		CbSize:           core.NotifyIconDataSizeV2,
		HWnd:             a.hwndHidden,
		UID:              trayIconID,
		UFlags:           core.NIFMessage | core.NIFIcon | core.NIFTIP,
		UCallbackMessage: msgTrayCallback,
		HIcon:            a.hIcon,
	}
}

func (a *App) addTrayIcon() error {
	nid := a.nid()
	copyUTF16(nid.SzTip[:], a.trayTip("启动中…"))
	ret, _, _ := pShellNotifyIconW.Call(core.NIMAdd, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
	if ret == 0 {
		return errors.New("Shell_NotifyIcon 返回失败")
	}
	return nil
}

func (a *App) removeTrayIcon() {
	nid := a.nid()
	pShellNotifyIconW.Call(core.NIMDelete, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
}

func (a *App) updateTrayTip(tip string) {
	nid := a.nid()
	nid.UFlags = core.NIFTIP
	copyUTF16(nid.SzTip[:], tip)
	pShellNotifyIconW.Call(core.NIMModify, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
}

// trayTip 鼠标悬停时看到的长文本
func (a *App) trayTip(fallback string) string {
	if a.summary == nil {
		if a.lastErr != "" {
			return "WorkBuddy 积分 · " + a.lastErr
		}
		return "WorkBuddy 积分 · " + fallback
	}
	tip := core.BalanceLine(a.summary)
	if c := core.CheckinLine(a.checkin); c != "" {
		tip += "\r\n" + c
	}
	if !a.lastUpdated.IsZero() {
		tip += "\r\n更新于 " + a.lastUpdated.Format("15:04:05") + "（北京时间）"
	}
	return tip
}

func (a *App) handleTrayCallback(lParam uintptr) {
	switch uint32(lParam) {
	case core.WMLButtonUp, core.WMRButtonUp, core.WMContextMenu:
		a.showMenu()
	case core.WMLButtonDbl:
		a.openWorkBuddy()
	}
}

// ---------------------------------------------------------------- 数据

// refreshAsync 在后台协程里拉数据，完成后 PostMessage 回主线程
//
// 网络请求绝不能放在窗口过程里跑，否则期间整个界面（连同托盘菜单）
// 都会卡住不动。
func (a *App) refreshAsync() {
	a.mu.Lock()
	if a.isLoading {
		a.mu.Unlock()
		return
	}
	a.isLoading = true
	cfg := a.cfg
	client := a.client
	hwnd := a.hwndHidden
	a.mu.Unlock()

	go func() {
		// 后台协程里的 panic 不会被 main 的 recover 兜住，会直接杀掉进程，
		// 托盘图标也跟着消失。这里自己兜一层。
		defer func() {
			if r := recover(); r != nil {
				core.Log("刷新协程崩溃: %v", r)
				a.mu.Lock()
				a.lastErr, a.lastKind = "内部错误："+fmt.Sprint(r), ""
				a.isLoading = false
				a.mu.Unlock()
				pPostMessageW.Call(hwnd, msgRefreshDone, 0, 0)
			}
		}()

		cred, summary, checkin, kind, msg := fetchAll(client, cfg)

		a.mu.Lock()
		if cred != nil {
			a.cred = cred
		}
		if summary != nil {
			a.summary, a.lastErr, a.lastKind = summary, "", ""
		} else {
			a.lastErr, a.lastKind = msg, kind
		}
		if checkin != nil {
			a.checkin = checkin
		}
		a.lastUpdated = time.Now()
		a.isLoading = false
		a.mu.Unlock()

		// 唤回 UI 线程统一更新界面
		pPostMessageW.Call(hwnd, msgRefreshDone, 0, 0)
	}()
}

// fetchAll 依次取凭据、积分汇总与签到状态
//
// client 复用同一个实例，这样连接能跨刷新保持，省掉每次的 TLS 握手。
func fetchAll(client *core.Client, cfg core.Config) (cred *core.Credential,
	summary *core.CreditSummary, checkin *core.CheckinStatus, kind string, msg string) {

	c, err := core.LoadCredential(cfg)
	if err != nil {
		e := core.AsError(err)
		return nil, nil, nil, e.Kind, e.UserMessage()
	}
	cred = c

	if client == nil {
		client = core.NewClient(20 * time.Second)
		client.Endpoint = cfg.Endpoint
	}

	s, err := client.FetchSummary(cred)
	if err != nil {
		e := core.AsError(err)
		core.Log("拉取积分失败: %s", e.Detail)
		return cred, nil, nil, e.Kind, e.UserMessage()
	}
	summary = s

	// 签到状态失败不影响主流程
	if cs, err := client.FetchCheckin(cred); err == nil {
		checkin = cs
	}
	return cred, summary, checkin, "", ""
}

// applyBadge 依据最新数据重排悬浮窗并刷新托盘提示（在 UI 线程调用）
func (a *App) applyBadge() {
	a.mu.Lock()
	text := a.badgeTextLocked()
	tip := a.trayTip("")
	show := a.cfg.ShowBadge
	a.mu.Unlock()

	a.updateTrayTip(tip)
	if !show {
		return
	}
	a.relayout(text)
	if !a.shown {
		pShowWindow.Call(a.hwndBadge, core.SWShowNoActivate)
		a.shown = true
	}
}

// badgeTextLocked 悬浮窗与托盘上那串短文本，需持有 a.mu
func (a *App) badgeTextLocked() string {
	if a.summary == nil {
		if a.lastErr != "" {
			return a.errorBadgeText()
		}
		return strings.TrimRight(a.prefix, " ")
	}
	return core.StatusText(a.cfg.DisplayMode, a.summary, a.prefix)
}

// errorBadgeText 出错时用一句极短的文案代替数字
func (a *App) errorBadgeText() string {
	switch a.lastKind {
	case core.KindCredentialMiss, core.KindCredentialBroken, core.KindTokenExpired:
		return "⚠ 未登录"
	case core.KindUnauthorized:
		return "⚠ 登录失效"
	case core.KindNetwork:
		return "⚠ 离线"
	case core.KindEndpoint:
		return "⚠ 地址无效"
	default:
		return "⚠ 出错"
	}
}

// ---------------------------------------------------------------- 悬浮窗

// relayout 按文本重新计算尺寸与位置
//
// 「不抖动」原则：只有尺寸真的变了才调 SetWindowPos 改大小，
// 已经摆好的位置不会被刷新动作推走。
func (a *App) relayout(text string) {
	text = strings.TrimSpace(text)

	sz := measureText(text, a.fontBadge)
	w := sz.CX + a.m.PadX*2
	if w < a.m.MinW {
		w = a.m.MinW
	}
	if w > a.m.MaxW {
		w = a.m.MaxW
	}
	h := a.m.Height

	a.mu.Lock()
	oldW := a.badgeW
	a.displayText = text
	a.badgeW = w

	needPlace := !a.positioned
	if needPlace {
		x, y := a.defaultBadgePos(w)
		a.badgeX, a.badgeY = x, y
		a.positioned = true
	}
	x, y := a.badgeX, a.badgeY
	a.mu.Unlock()

	switch {
	case needPlace:
		pSetWindowPos.Call(a.hwndBadge, core.HWNDTopmost,
			uintptr(uint32(x)), uintptr(uint32(y)),
			uintptr(uint32(w)), uintptr(uint32(h)),
			core.SWPNoActivate)
		setRoundedRegion(a.hwndBadge, w, h, a.m.Radius)
	case oldW != w:
		pSetWindowPos.Call(a.hwndBadge, core.HWNDTopmost, 0, 0,
			uintptr(uint32(w)), uintptr(uint32(h)),
			core.SWPNoMove|core.SWPNoActivate)
		setRoundedRegion(a.hwndBadge, w, h, a.m.Radius)
	}
	pInvalidateRect.Call(a.hwndBadge, 0, 0)
}

// defaultBadgePos 计算悬浮窗初始坐标：优先用保存过的位置，否则贴右上角
func (a *App) defaultBadgePos(w int32) (int32, int32) {
	wa := workArea()

	if a.cfg.HasBadgePos {
		return a.clampBadgePos(int32(a.cfg.BadgeX), int32(a.cfg.BadgeY), w, wa)
	}
	return wa.Right - w - a.m.Margin, wa.Top + a.m.Margin
}

// clampBadgePos 把坐标限制在工作区内，避免换了显示器之后窗口跑到看不见的地方
func (a *App) clampBadgePos(x, y, w int32, wa core.Rect) (int32, int32) {
	if x < wa.Left {
		x = wa.Left
	}
	if x+w > wa.Right {
		x = wa.Right - w
	}
	if y < wa.Top {
		y = wa.Top
	}
	if y+a.m.Height > wa.Bottom {
		y = wa.Bottom - a.m.Height
	}
	return x, y
}

// beginDrag 左键按下：记住光标相对窗口左上角的偏移，并抓取鼠标
func (a *App) beginDrag() {
	var pt core.Point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	runtime.KeepAlive(&pt)

	a.mu.Lock()
	a.dragging = true
	a.dragDX = pt.X - a.badgeX
	a.dragDY = pt.Y - a.badgeY
	a.mu.Unlock()

	pSetCapture.Call(a.hwndBadge)
}

// dragMove 拖动过程中跟随光标
func (a *App) dragMove() {
	a.mu.Lock()
	dragging := a.dragging
	dx, dy := a.dragDX, a.dragDY
	w := a.badgeW
	a.mu.Unlock()
	if !dragging {
		return
	}

	var pt core.Point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	runtime.KeepAlive(&pt)

	x, y := pt.X-dx, pt.Y-dy
	x, y = a.clampBadgePos(x, y, w, workArea())

	a.mu.Lock()
	a.badgeX, a.badgeY = x, y
	a.mu.Unlock()

	pSetWindowPos.Call(a.hwndBadge, core.HWNDTopmost,
		uintptr(uint32(x)), uintptr(uint32(y)), 0, 0,
		core.SWPNoSize|core.SWPNoActivate)
}

// endDrag 松手：释放鼠标并把位置写入配置
func (a *App) endDrag() {
	a.mu.Lock()
	if !a.dragging {
		a.mu.Unlock()
		return
	}
	a.dragging = false
	x, y := a.badgeX, a.badgeY
	a.mu.Unlock()

	pReleaseCapture.Call()
	if err := core.SaveBadgePosition(int(x), int(y)); err != nil {
		core.Log("保存悬浮窗位置失败: %v", err)
	}
}

// paintBadge 画那颗圆角药丸
func (a *App) paintBadge(hwnd uintptr) {
	var ps core.PaintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	runtime.KeepAlive(&ps)
	if hdc == 0 {
		return
	}
	defer func() {
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		runtime.KeepAlive(&ps)
	}()

	a.mu.Lock()
	text := a.displayText
	warn := a.lastErr != "" && a.summary == nil
	a.mu.Unlock()

	var rc core.Rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	runtime.KeepAlive(&rc)

	// 背景与描边。四个 GDI 对象各自判断是否由我们创建：只有自己建的才能删，
	// 系统库存对象删了会出问题。
	brush, _, _ := pCreateSolidBrush.Call(colorRef(badgeBgR, badgeBgG, badgeBgB))
	if brush == 0 {
		brush, _, _ = pGetStockObject.Call(core.NULLBRUSH)
	} else {
		defer pDeleteObject.Call(brush)
	}
	pen, _, _ := pCreatePen.Call(core.PSSolid, 1, colorRef(badgeBdR, badgeBdG, badgeBdB))
	if pen == 0 {
		pen, _, _ = pGetStockObject.Call(core.NULLPEN)
	} else {
		defer pDeleteObject.Call(pen)
	}

	oldBrush, _, _ := pSelectObject.Call(hdc, brush)
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	// RoundRect 的两个尾参是圆角椭圆的宽高，即半径的两倍
	d := uintptr(uint32(a.m.Radius * 2))
	pRoundRect.Call(hdc, 0, 0, uintptr(rc.Right), uintptr(rc.Bottom), d, d)
	pSelectObject.Call(hdc, oldBrush)
	pSelectObject.Call(hdc, oldPen)

	if text == "" {
		return
	}

	// 文本
	fgR, fgG, fgB := uint8(badgeFgR), uint8(badgeFgG), uint8(badgeFgB)
	if warn {
		fgR, fgG, fgB = uint8(badgeWarnFgR), uint8(badgeWarnFgG), uint8(badgeWarnFgB)
	}
	oldFont, _, _ := pSelectObject.Call(hdc, a.fontBadge)
	defer pSelectObject.Call(hdc, oldFont)

	pSetBkMode.Call(hdc, core.BkTransparent)
	pSetTextColor.Call(hdc, colorRef(fgR, fgG, fgB))

	u16 := utf16.Encode([]rune(text))
	pDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(&u16[0])),
		uintptr(len(u16)),
		uintptr(unsafe.Pointer(&rc)),
		core.DTCenter|core.DTVCenter|core.DTSingleLine|core.DTNoPrefix)
	runtime.KeepAlive(u16)
}

// ---------------------------------------------------------------- 菜单

func (a *App) showMenu() {
	a.mu.Lock()
	cred, summary, checkin := a.cred, a.summary, a.checkin
	lastErr, updated := a.lastErr, a.lastUpdated
	loading := a.isLoading
	showBadge := a.cfg.ShowBadge
	interval := a.cfg.RefreshInterval
	mode := a.cfg.DisplayMode
	a.mu.Unlock()

	hMenu, _, _ := pCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer pDestroyMenu.Call(hMenu)

	// ── 信息区（灰色不可点，只用来展示）
	account := "👤 未登录"
	if cred != nil {
		account = "👤 " + cred.DisplayName()
	}
	appendMenu(hMenu, core.MFString|core.MFDisabled, 0, account)
	appendMenu(hMenu, core.MFString|core.MFDisabled, 0, core.BalanceLine(summary))

	if line := core.CheckinLine(checkin); line != "" {
		appendMenu(hMenu, core.MFString|core.MFDisabled, 0, line)
	}

	switch {
	case loading:
		appendMenu(hMenu, core.MFString|core.MFDisabled, 0, "🕘 正在刷新…")
	case lastErr != "":
		appendMenu(hMenu, core.MFString|core.MFDisabled, 0, "🕘 "+lastErr)
	case !updated.IsZero():
		appendMenu(hMenu, core.MFString|core.MFDisabled, 0,
			"🕘 更新于 "+updated.Format("15:04:05"))
	}

	appendMenu(hMenu, core.MFSeparator, 0, "")

	// ── 套餐明细
	if summary != nil && len(summary.ActivePackages()) > 0 {
		hSub, _, _ := pCreatePopupMenu.Call()
		for i, p := range summary.ActivePackages() {
			appendMenu(hSub, core.MFString|core.MFDisabled,
				uintptr(cmdPackageBase+i),
				fmt.Sprintf("%s：%s / %s", core.PackageDisplayName(p.Code),
					core.Number(p.Remain), core.Number(p.Total)))
		}
		for _, p := range summary.ExhaustedPackages() {
			appendMenu(hSub, core.MFString|core.MFDisabled, 0,
				"（已用尽）"+core.PackageDisplayName(p.Code))
		}
		appendMenu(hMenu, core.MFString|core.MFPopup, hSub, "📦 积分套餐")
	}

	// ── 刷新间隔
	hInterval, _, _ := pCreatePopupMenu.Call()
	for i, c := range intervalChoices {
		flags := uintptr(core.MFString)
		if c.Seconds == interval {
			flags |= core.MFChecked
		}
		appendMenu(hInterval, flags, uintptr(cmdIntervalBase+i), c.Label)
	}
	appendMenu(hMenu, core.MFString|core.MFPopup, hInterval, "⏱ 自动刷新")

	// ── 显示方式
	hDisplay, _, _ := pCreatePopupMenu.Call()
	for _, m := range displayModes {
		flags := uintptr(core.MFString)
		if m == mode {
			flags |= core.MFChecked
		}
		appendMenu(hDisplay, flags, uintptr(cmdDisplayBase)+uintptr(m), m.Label())
	}
	appendMenu(hMenu, core.MFString|core.MFPopup, hDisplay, "🎚 显示方式")

	// ── 开关
	appendMenu(hMenu, toggleFlags(showBadge), cmdToggleBadge, "🖥 桌面悬浮窗")
	appendMenu(hMenu, toggleFlags(autostartEnabled()), cmdToggleAutostart, "🚀 开机自启")

	appendMenu(hMenu, core.MFSeparator, 0, "")

	appendMenu(hMenu, core.MFString, cmdRefresh, "🔄 立即刷新")
	appendMenu(hMenu, core.MFString, cmdOpenApp, "🪟 打开 WorkBuddy")
	appendMenu(hMenu, core.MFString, cmdCopy, "📋 复制余额明细")
	appendMenu(hMenu, core.MFString, cmdOpenConfig, "📁 打开配置目录")

	appendMenu(hMenu, core.MFSeparator, 0, "")
	appendMenu(hMenu, core.MFString, cmdQuit, "退出")

	// ── 弹出
	var pt core.Point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	runtime.KeepAlive(&pt)

	// 不先设为前台窗口的话，点击别处菜单不会自动消失
	pSetForegroundWindow.Call(a.hwndHidden)
	cmd, _, _ := pTrackPopupMenu.Call(hMenu,
		core.TPMLeftAlign|core.TPMReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), 0, a.hwndHidden, 0)
	pPostMessageW.Call(a.hwndHidden, core.WMNull, 0, 0)

	if cmd != 0 {
		a.handleCommand(uint32(cmd))
	}
}

func toggleFlags(on bool) uintptr {
	if on {
		return core.MFString | core.MFChecked
	}
	return core.MFString
}

// handleCommand 处理菜单命令
func (a *App) handleCommand(id uint32) {
	switch {
	case id == cmdRefresh:
		a.refreshAsync()
	case id == cmdOpenApp:
		a.openWorkBuddy()
	case id == cmdOpenConfig:
		if err := shellExecute("open", core.ConfigDir()); err != nil {
			messageBox(appTitle, "打开配置目录失败：\n"+err.Error(), mbOK|mbIconWarning)
		}
	case id == cmdCopy:
		a.copySummary()
	case id == cmdToggleBadge:
		a.toggleBadge()
	case id == cmdToggleAutostart:
		a.toggleAutostart()
	case id == cmdQuit:
		a.quit()
	case id >= cmdIntervalBase && id < cmdIntervalBase+uint32(len(intervalChoices)):
		choice := intervalChoices[id-cmdIntervalBase]
		a.setInterval(choice.Seconds)
	case id >= cmdDisplayBase && id < cmdDisplayBase+uint32(len(displayModes)):
		mode := core.DisplayMode(id - cmdDisplayBase)
		a.setDisplayMode(mode)
	}
}

func (a *App) toggleBadge() {
	a.mu.Lock()
	a.cfg.ShowBadge = !a.cfg.ShowBadge
	show := a.cfg.ShowBadge
	a.mu.Unlock()

	if err := core.UpdateConfig(map[string]any{"showBadge": show}); err != nil {
		core.Log("保存 showBadge 失败: %v", err)
	}

	if show {
		// 重新按「初始位置」摆放，避免上次的坐标已经不在屏幕内
		a.mu.Lock()
		a.positioned = false
		a.mu.Unlock()
		a.applyBadge()
	} else {
		pShowWindow.Call(a.hwndBadge, core.SWHide)
		a.shown = false
	}
}

func (a *App) toggleAutostart() {
	on := autostartEnabled()
	var err error
	if on {
		err = disableAutostart()
	} else {
		err = enableAutostart()
	}
	if err != nil {
		core.Log("切换开机自启失败: %v", err)
		messageBox(appTitle, "设置开机自启失败：\n"+err.Error(), mbOK|mbIconWarning)
	}
}

func (a *App) setInterval(seconds int) {
	a.mu.Lock()
	a.cfg.RefreshInterval = seconds
	a.mu.Unlock()

	if err := core.UpdateConfig(map[string]any{"refreshInterval": seconds}); err != nil {
		core.Log("保存 refreshInterval 失败: %v", err)
	}
	a.applyTimer()
}

func (a *App) setDisplayMode(mode core.DisplayMode) {
	a.mu.Lock()
	a.cfg.DisplayMode = mode
	a.mu.Unlock()

	if err := core.UpdateConfig(map[string]any{"displayMode": mode.Key()}); err != nil {
		core.Log("保存 displayMode 失败: %v", err)
	}
	a.applyBadge()
}

// applyTimer 按当前配置重建自动刷新定时器
func (a *App) applyTimer() {
	a.mu.Lock()
	sec := a.cfg.RefreshInterval
	running := a.timerRunning
	a.mu.Unlock()

	if running {
		pKillTimer.Call(a.hwndHidden, timerRefresh)
		a.timerRunning = false
	}
	if sec <= 0 {
		return
	}
	if r, _, _ := pSetTimer.Call(a.hwndHidden, timerRefresh, uintptr(sec*1000), 0); r != 0 {
		a.timerRunning = true
	}
}

func (a *App) stopTimer() {
	if a.timerRunning {
		pKillTimer.Call(a.hwndHidden, timerRefresh)
		a.timerRunning = false
	}
}

// ---------------------------------------------------------------- 动作

func (a *App) copySummary() {
	a.mu.Lock()
	name := ""
	if a.cred != nil {
		name = a.cred.DisplayName()
	}
	text := core.SummaryText(name, a.summary, a.lastErr)
	a.mu.Unlock()

	if err := setClipboardText(a.hwndHidden, text); err != nil {
		core.Log("复制到剪贴板失败: %v", err)
		messageBox(appTitle, "复制失败：\n"+err.Error(), mbOK|mbIconWarning)
		return
	}
	a.notify("已复制", "余额明细已复制到剪贴板")
}

// notify 弹一个托盘气泡。不支持气泡时静默忽略。
func (a *App) notify(title, text string) {
	nid := a.nid()
	nid.UFlags = core.NIFInfo
	nid.DwInfoFlags = 0x00000001 // NIIF_INFO
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], text)
	pShellNotifyIconW.Call(core.NIMModify, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
}

// openWorkBuddy 启动 WorkBuddy 桌面端。找不到安装位置时退回打开主目录。
func (a *App) openWorkBuddy() {
	for _, candidate := range workbuddyCandidates() {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		if err := shellExecute("open", candidate); err == nil {
			return
		}
	}
	// 交给协议处理器
	if err := shellExecute("open", "workbuddy://"); err != nil {
		messageBox(appTitle,
			"没能找到 WorkBuddy 主程序。\n\n请手动打开 WorkBuddy，或把它固定在任务栏后从那里启动。",
			mbOK|mbIconInformation)
	}
}

func (a *App) quit() {
	core.Log("用户从菜单退出")
	a.cleanup()
	pDestroyWindow.Call(a.hwndHidden)
	pPostQuitMessage.Call(0)
}
