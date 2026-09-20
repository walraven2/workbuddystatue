package core

// 本文件放 Win32 结构体定义。之所以放在 core 而不是 package main，
// 是为了能在 macOS 上跑布局单元测试——这些结构体是直接按字节
// 传给系统 API 的，偏移或长度写错在 Windows 上会直接崩或者静默失效，
// 而这类错误无法靠编译器发现。

// Point 对应 Win32 POINT
type Point struct {
	X int32
	Y int32
}

// Rect 对应 Win32 RECT
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// Size 对应 Win32 SIZE
type Size struct {
	CX int32
	CY int32
}

// Width 矩形宽度
func (r Rect) Width() int32 { return r.Right - r.Left }

// Height 矩形高度
func (r Rect) Height() int32 { return r.Bottom - r.Top }

// Msg 对应 Win32 MSG
//
// 尾部在 C 里还有一个 DWORD lPrivate，Go 结构体的对齐填充会自动补上
// （44 -> 48），因此尺寸与官方定义一致。
type Msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
}

// WndClassEx 对应 Win32 WNDCLASSEXW
type WndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// PaintStruct 对应 Win32 PAINTSTRUCT
type PaintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     Rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

// 以下为各结构体在 x64 下的期望尺寸
const (
	PointSize       = 8
	RectSize        = 16
	SizeSize        = 8
	MsgSize         = 48
	WndClassExSize  = 80
	PaintStructSize = 72
)
