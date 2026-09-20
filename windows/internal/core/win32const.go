package core

// NotifyIconData 对应 Win32 的 NOTIFYICONDATAW。
//
// 采用 V2 布局（字段截止到 dwInfoFlags，含 szInfo 气泡通知）。
// Go 结构体会自动插入与 C 相同的对齐填充，因此 sizeof 应当等于 944 字节
// （x64）。单元测试会校验该布局，避免在无法运行 Windows 程序时留下隐患。
type NotifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutVersion  uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
}

// NotifyIconDataSizeV2 x64 下 NOTIFYICONDATAW（V2）的字节数。
//
// 逐字段推算：cbSize 0 / hWnd 8 / uID 16 / uFlags 20 / uCallbackMessage 24 /
// hIcon 32 / szTip 40(256B) / dwState 296 / dwStateMask 300 /
// szInfo 304(512B) / uTimeout 816 / szInfoTitle 820(128B) / dwInfoFlags 948，
// 末尾按 8 字节对齐后为 952。
//
// 交叉验证：加上 GUID(16) 与 hBalloonIcon(8) 得到 V3 布局的 976 字节，
// 与公开资料中 x64 上 sizeof(NOTIFYICONDATAW) = 976 一致。
const NotifyIconDataSizeV2 = 952

// ---------------------------------------------------------------- 托盘常量

const (
	NIMAdd        = 0x00000000
	NIMModify     = 0x00000001
	NIMDelete     = 0x00000002
	NIMSetVersion = 0x00000004

	NIFMessage = 0x00000001
	NIFIcon    = 0x00000002
	NIFTIP     = 0x00000004
	NIFState   = 0x00000008
	NIFInfo    = 0x00000010
)

// ---------------------------------------------------------------- 窗口消息

const (
	WMDestroy       = 0x0002
	WMClose         = 0x0010
	WMPaint         = 0x000F
	WMEraseBkgnd    = 0x0014
	WMCommand       = 0x0111
	WMTimer         = 0x0113
	WMMove          = 0x0003
	WMMouseMove     = 0x0200
	WMLButtonDown   = 0x0201
	WMLButtonUp     = 0x0202
	WMLButtonDbl    = 0x0203
	WMRButtonUp     = 0x0205
	WMNCLButtonDown = 0x00A1
	WMNull          = 0x0000
	WMApp           = 0x8000
	WMContextMenu   = 0x007B

	HTCaption = 2
)

// ---------------------------------------------------------------- 菜单常量

const (
	MFString    = 0x00000000
	MFSeparator = 0x00000800
	MFGrayed    = 0x00000001
	MFDisabled  = 0x00000002
	MFChecked   = 0x00000008
	MFPopup     = 0x00000010

	TPMLeftAlign   = 0x0000
	TPMRightButton = 0x0002
	TPMReturnCmd   = 0x0100

	// IDCArrow 标准箭头光标资源 ID
	IDCArrow = 32512
)

// ---------------------------------------------------------------- 窗口样式

const (
	WSOverlapped = 0x00000000
	WSPopup      = 0x80000000
	WSTabStop    = 0x00010000

	// 窗口类样式
	CSHRedraw = 0x0002
	CSVRedraw = 0x0004

	WSEXTopmost    = 0x00000008
	WSEXToolWindow = 0x00000080
	WSEXLayered    = 0x00080000
	WSEXNoActivate = 0x08000000

	// ShowWindow 命令
	SWHide           = 0
	SWShowNoActivate = 4

	SWPNoSize     = 0x0001
	SWPNoMove     = 0x0002
	SWPNoActivate = 0x0010
	SWPShowWindow = 0x0040

	HWNDTopmost = ^uintptr(0) // (HWND)-1

	LWAAlpha = 0x00000002

	SPIGetWorkArea = 0x0030

	SMXScreen  = 0
	SMYScreen  = 1
	SMCXSmIcon = 49
	SMCYSmIcon = 50

	// GetDeviceCaps 索引
	LOGPIXELSY = 90
)

// ---------------------------------------------------------------- 绘制常量

const (
	BkTransparent = 1
	DTLeft        = 0x00000000
	DTCenter      = 0x00000001
	DTRight       = 0x00000002
	DTVCenter     = 0x00000004
	DTSingleLine  = 0x00000020
	DTNoPrefix    = 0x00000800

	// GetStockObject 索引
	NULLBRUSH      = 5
	NULLPEN        = 8
	DEFAULTGUIFONT = 17

	PSSolid = 0

	FWMedium = 500
	FWNormal = 400
)
