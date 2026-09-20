package core

import "testing"

func TestComputeMetrics(t *testing.T) {
	cases := []struct {
		dpi    int32
		height int32
		padX   int32
		minW   int32
		radius int32
		fontPx int32
		note   string
	}{
		{96, 36, 14, 76, 11, 16, "100%：原样"},
		{120, 45, 18, 95, 14, 20, "125%"},
		{144, 54, 21, 114, 17, 24, "150%"},
		{192, 72, 28, 152, 22, 32, "200%"},
		{288, 108, 42, 228, 33, 48, "300%"},
		// 低于 100% 不做缩小，避免在高分屏之外的窄屏上糊掉
		{72, 36, 14, 76, 11, 16, "75%：夹到 100%"},
		// 高于 300% 不再放大，否则挂件会占满屏幕
		{480, 108, 42, 228, 33, 48, "500%：夹到 300%"},
		// 异常值当作 96 处理
		{0, 36, 14, 76, 11, 16, "0：按 100% 兜底"},
	}
	for _, c := range cases {
		m := ComputeMetrics(c.dpi)
		if m.Height != c.height || m.PadX != c.padX || m.MinW != c.minW ||
			m.Radius != c.radius || m.FontPx != c.fontPx {
			t.Errorf("ComputeMetrics(%d) [%s] = %+v，期望 height=%d padX=%d minW=%d radius=%d fontPx=%d",
				c.dpi, c.note, m, c.height, c.padX, c.minW, c.radius, c.fontPx)
		}
	}
}

// TestComputeMetricsConsistency 检查缩放后各尺寸之间的比例关系仍然成立。
// 比例关系一旦破坏，悬浮窗就会出现「文字撑破药丸」这类问题。
func TestComputeMetricsConsistency(t *testing.T) {
	for _, dpi := range []int32{96, 110, 120, 133, 144, 168, 192, 216, 240, 288} {
		m := ComputeMetrics(dpi)

		if m.MinW >= m.MaxW {
			t.Errorf("dpi=%d：minW(%d) 应小于 maxW(%d)", dpi, m.MinW, m.MaxW)
		}
		// 圆角半径不能超过高度的一半，否则 RoundRect 画出来是椭圆形
		if m.Radius*2 > m.Height {
			t.Errorf("dpi=%d：半径 %d 超过高度的一半（%d）", dpi, m.Radius, m.Height/2)
		}
		// 字号要留出上下留白
		if m.FontPx >= m.Height {
			t.Errorf("dpi=%d：字号 %d 不应大于等于高度 %d", dpi, m.FontPx, m.Height)
		}
		// 左右内边距之和不能超过最小宽度，否则最小状态下文字无处可放
		if m.PadX*2 >= m.MinW {
			t.Errorf("dpi=%d：左右内边距合计 %d 不应大于等于最小宽度 %d", dpi, m.PadX*2, m.MinW)
		}
	}
}

// TestComputeMetricsMonotonic 缩放系数单调递增：DPI 越高尺寸只能越大或持平
func TestComputeMetricsMonotonic(t *testing.T) {
	prev := ComputeMetrics(96)
	for dpi := int32(97); dpi <= 300; dpi++ {
		cur := ComputeMetrics(dpi)
		if cur.Height < prev.Height || cur.FontPx < prev.FontPx {
			t.Fatalf("dpi=%d 时尺寸反而变小了：%+v -> %+v", dpi, prev, cur)
		}
		prev = cur
	}
}
