package core

import (
	"testing"
	"unsafe"
)

// Win32 结构体的布局校验。
//
// 这些结构体直接按字节传给系统 API（GetMessageW、CreateWindowExW 等），
// 字段偏移错了在 Windows 上是崩溃或静默失效，编译器不会报错。
// 由于无法在本机运行 exe，这里用编译期布局做等价验证。

func TestMsgLayout(t *testing.T) {
	var m Msg
	if got := unsafe.Sizeof(m); got != MsgSize {
		t.Fatalf("sizeof(MSG) = %d，期望 %d", got, MsgSize)
	}
	if off := unsafe.Offsetof(m.Hwnd); off != 0 {
		t.Errorf("MSG.hwnd 偏移 = %d，期望 0", off)
	}
	if off := unsafe.Offsetof(m.Message); off != 8 {
		t.Errorf("MSG.message 偏移 = %d，期望 8", off)
	}
	// wParam 需要 8 字节对齐，C 里中间有 4 字节填充
	if off := unsafe.Offsetof(m.WParam); off != 16 {
		t.Errorf("MSG.wParam 偏移 = %d，期望 16", off)
	}
	if off := unsafe.Offsetof(m.LParam); off != 24 {
		t.Errorf("MSG.lParam 偏移 = %d，期望 24", off)
	}
	if off := unsafe.Offsetof(m.Time); off != 32 {
		t.Errorf("MSG.time 偏移 = %d，期望 32", off)
	}
	if off := unsafe.Offsetof(m.Pt); off != 36 {
		t.Errorf("MSG.pt 偏移 = %d，期望 36", off)
	}
}

func TestWndClassExLayout(t *testing.T) {
	var w WndClassEx
	if got := unsafe.Sizeof(w); got != WndClassExSize {
		t.Fatalf("sizeof(WNDCLASSEXW) = %d，期望 %d", got, WndClassExSize)
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"cbSize", unsafe.Offsetof(w.CbSize), 0},
		{"style", unsafe.Offsetof(w.Style), 4},
		{"lpfnWndProc", unsafe.Offsetof(w.LpfnWndProc), 8},
		{"cbClsExtra", unsafe.Offsetof(w.CbClsExtra), 16},
		{"cbWndExtra", unsafe.Offsetof(w.CbWndExtra), 20},
		{"hInstance", unsafe.Offsetof(w.HInstance), 24},
		{"hIcon", unsafe.Offsetof(w.HIcon), 32},
		{"hCursor", unsafe.Offsetof(w.HCursor), 40},
		{"hbrBackground", unsafe.Offsetof(w.HbrBackground), 48},
		{"lpszMenuName", unsafe.Offsetof(w.LpszMenuName), 56},
		{"lpszClassName", unsafe.Offsetof(w.LpszClassName), 64},
		{"hIconSm", unsafe.Offsetof(w.HIconSm), 72},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("WNDCLASSEXW.%s 偏移 = %d，期望 %d", c.name, c.got, c.want)
		}
	}
}

func TestPaintStructLayout(t *testing.T) {
	var p PaintStruct
	if got := unsafe.Sizeof(p); got != PaintStructSize {
		t.Fatalf("sizeof(PAINTSTRUCT) = %d，期望 %d", got, PaintStructSize)
	}
	if off := unsafe.Offsetof(p.RcPaint); off != 12 {
		t.Errorf("PAINTSTRUCT.rcPaint 偏移 = %d，期望 12", off)
	}
	if off := unsafe.Offsetof(p.RgbReserved); off != 36 {
		t.Errorf("PAINTSTRUCT.rgbReserved 偏移 = %d，期望 36", off)
	}
}

func TestPointRectLayout(t *testing.T) {
	if got := unsafe.Sizeof(Point{}); got != PointSize {
		t.Errorf("sizeof(POINT) = %d，期望 %d", got, PointSize)
	}
	if got := unsafe.Sizeof(Rect{}); got != RectSize {
		t.Errorf("sizeof(RECT) = %d，期望 %d", got, RectSize)
	}
	if got := unsafe.Sizeof(Size{}); got != SizeSize {
		t.Errorf("sizeof(SIZE) = %d，期望 %d", got, SizeSize)
	}
	r := Rect{Left: 10, Top: 20, Right: 110, Bottom: 60}
	if r.Width() != 100 || r.Height() != 40 {
		t.Errorf("Rect 尺寸计算错误：%dx%d", r.Width(), r.Height())
	}
}
