package core

// 界面尺寸的基准值，都是 96 DPI（100% 缩放）下的像素。
//
// 放在 core 里而不是界面层，是为了能在 macOS 上跑单元测试：
// 取整和缩放比例算错了只会让挂件大小不对，编译器完全不会提醒。
const (
	BaseBadgeHeight = 36
	BaseBadgePadX   = 14
	BaseBadgeMinW   = 76
	BaseBadgeMaxW   = 320
	BaseBadgeRadius = 11
	BaseBadgeMargin = 24
	BaseBadgeFontPx = 16
	BaseMenuFontPx  = 14
)

// Metrics 按当前 DPI 换算之后的一组界面尺寸
type Metrics struct {
	Height   int32
	PadX     int32
	MinW     int32
	MaxW     int32
	Radius   int32
	Margin   int32
	FontPx   int32
	FontMenu int32
}

// ComputeMetrics 把 96 DPI 基准尺寸换算到指定 DPI。
//
// dpi 会被夹到 [96, 288]：低于 100% 不做缩小（会糊），
// 高于 300% 不再放大（挂件会占满屏幕）。
func ComputeMetrics(dpi int32) Metrics {
	s := float64(dpi) / 96.0
	if s < 1 {
		s = 1
	}
	if s > 3 {
		s = 3
	}
	at := func(v int) int32 { return int32(float64(v)*s + 0.5) }
	return Metrics{
		Height:   at(BaseBadgeHeight),
		PadX:     at(BaseBadgePadX),
		MinW:     at(BaseBadgeMinW),
		MaxW:     at(BaseBadgeMaxW),
		Radius:   at(BaseBadgeRadius),
		Margin:   at(BaseBadgeMargin),
		FontPx:   at(BaseBadgeFontPx),
		FontMenu: at(BaseMenuFontPx),
	}
}
