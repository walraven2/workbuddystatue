// Command makeico 程序化生成 Windows 图标（.ico）。
//
// 为什么自己画而不是缩放一张 PNG：16x16 是托盘的实际显示尺寸，
// 直接缩放大图会让闪电糊成一团。这里对每个尺寸独立渲染，
// 小尺寸用「无阴影 + 加粗闪电 + 更小留白」的简化版，保证清晰度。
//
// 渲染采用 4 倍超采样后面积平均下采样，边缘自带抗锯齿。
// 全部尺寸都用 BMP(DIB) 帧而非 PNG 帧，因为 CreateIconFromResourceEx
// 在部分 Windows 版本上对 PNG 帧支持不佳。
//
// 用法：
//
//	go run ./tools/makeico <输出.ico>
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
)

// 需要生成的尺寸。16/20/24/32 供托盘，48 以上供资源管理器。
var targetSizes = []int{16, 20, 24, 32, 48, 64, 128, 256}

// 超采样倍数（每个方向）
const superSample = 4

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法：makeico <输出.ico>")
		os.Exit(2)
	}
	outPath := os.Args[1]

	var payloads [][]byte
	for _, size := range targetSizes {
		img := renderIcon(size)
		payload := encodeBMPFrame(img)
		payloads = append(payloads, payload)
		style := "详细版"
		if size <= 32 {
			style = "简化版"
		}
		fmt.Printf("  %3dx%-3d  %-6s %6d 字节\n", size, size, style, len(payload))
	}

	var buf bytes.Buffer
	if err := writeICO(&buf, targetSizes, payloads); err != nil {
		fmt.Fprintf(os.Stderr, "错误: 生成 ICO 失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "错误: 写入失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已生成: %s（%d 字节，%d 个尺寸）\n", outPath, buf.Len(), len(targetSizes))
}

// ---------------------------------------------------------------- 绘制

// lightningBolt 归一化坐标的闪电多边形（顺序闭合）
//
// 细长竖直的经典闪电造型，与 macOS 版图标的观感保持一致：
// 上端偏右、向左下折、中段转折、下端尖角。
var lightningBolt = [][2]float64{
	{0.588, 0.140},
	{0.330, 0.548},
	{0.468, 0.548},
	{0.412, 0.860},
	{0.672, 0.452},
	{0.534, 0.452},
}

// renderIcon 渲染指定边长的图标
func renderIcon(size int) *image.NRGBA {
	big := size * superSample
	flat := size <= 32

	// 归一化布局参数
	margin := 0.090
	radiusRatio := 0.2245
	boltScale := 1.0
	if flat {
		// 小尺寸：略微加大闪电并收紧留白，让图形更「实」，
		// 但不能太夸张，否则 16px 下闪电会糊成一团白块。
		margin = 0.070
		radiusRatio = 0.250
		boltScale = 1.18
	}

	side := float64(big)
	inset := side * margin
	x0, y0 := inset, inset
	x1, y1 := side-inset, side-inset
	radius := (x1 - x0) * radiusRatio

	canvas := image.NewNRGBA(image.Rect(0, 0, big, big))

	shadowOffset := side * 0.022
	shadowBlur := side * 0.030

	for py := 0; py < big; py++ {
		fy := float64(py) + 0.5
		for px := 0; px < big; px++ {
			fx := float64(px) + 0.5

			insideBody := sdfRoundRect(fx, fy, x0, y0, x1, y1, radius) <= 0

			// —— 主体 ——
			if insideBody {
				// 垂直渐变：琥珀 -> 深橙
				t := clamp01((fy - y0) / (y1 - y0))
				base := color.NRGBA{
					R: uint8(247 - t*3),
					G: uint8(190 - t*52),
					B: uint8(70 - t*38),
					A: 255,
				}
				// 左上角高光，增加体积感
				hl := clamp01(1 - math.Hypot(fx-x0, fy-y0)/(side*0.85))
				base = lighten(base, hl*0.22)

				if insideBolt(fx, fy, x0, y0, x1, y1, boltScale) {
					base = color.NRGBA{255, 255, 255, 255}
				}
				canvas.SetNRGBA(px, py, base)
				continue
			}

			// —— 阴影（仅详细版，且只画在主体之外）——
			//
			// 注意顺序：必须先判定主体。阴影的有符号距离对主体内部同样为负，
			// 若先判阴影并 continue，主体就永远画不出来（曾因此让 256px 全灰）。
			// 真实投影本就是被不透明主体盖住的，所以「只在主体外绘制」与正确结果等价。
			if !flat {
				d := sdfRoundRect(fx, fy-shadowOffset, x0, y0, x1, y1, radius)
				if d < shadowBlur {
					alpha := 1 - clamp01(d/shadowBlur)
					canvas.SetNRGBA(px, py, blend(color.NRGBA{0, 0, 0, uint8(alpha * 70)}, canvas.NRGBAAt(px, py)))
				}
			}
		}
	}

	return downsample(canvas, size)
}

// insideBolt 判断点是否落在闪电多边形内（围绕图形中心按 scale 缩放）
func insideBolt(px, py, x0, y0, x1, y1, scale float64) bool {
	w := x1 - x0
	h := y1 - y0
	cx := (x0 + x1) / 2
	cy := (y0 + y1) / 2

	pts := make([][2]float64, len(lightningBolt))
	for i, p := range lightningBolt {
		nx := (p[0]-0.5)*scale + 0.5
		ny := (p[1]-0.5)*scale + 0.5
		pts[i] = [2]float64{cx + (nx-0.5)*w, cy + (ny-0.5)*h}
	}
	return insidePolygon(pts, px, py)
}

// sdfRoundRect 圆角矩形的有符号距离场（< 0 表示在内部）
func sdfRoundRect(px, py, x0, y0, x1, y1, r float64) float64 {
	cx := (x0 + x1) / 2
	cy := (y0 + y1) / 2
	hw := (x1 - x0) / 2
	hh := (y1 - y0) / 2

	qx := math.Abs(px-cx) - (hw - r)
	qy := math.Abs(py-cy) - (hh - r)

	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - r
}

// insidePolygon 奇偶规则的点在多边形内判定
func insidePolygon(pts [][2]float64, px, py float64) bool {
	inside := false
	j := len(pts) - 1
	for i := 0; i < len(pts); i++ {
		xi, yi := pts[i][0], pts[i][1]
		xj, yj := pts[j][0], pts[j][1]
		if (yi > py) != (yj > py) {
			xCross := (xj-xi)*(py-yi)/(yj-yi) + xi
			if px < xCross {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

// ---------------------------------------------------------------- 颜色与缩放

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lighten(c color.NRGBA, amount float64) color.NRGBA {
	mix := func(v uint8) uint8 {
		return uint8(float64(v) + (255-float64(v))*amount + 0.5)
	}
	return color.NRGBA{mix(c.R), mix(c.G), mix(c.B), c.A}
}

// blend 把 src 按自身 alpha 叠在 dst 上（两者均为非预乘）
func blend(src, dst color.NRGBA) color.NRGBA {
	sa := float64(src.A) / 255
	da := float64(dst.A) / 255
	outA := sa + da*(1-sa)
	if outA == 0 {
		return color.NRGBA{}
	}
	mix := func(s, d uint8) uint8 {
		return uint8((float64(s)*sa + float64(d)*da*(1-sa)) / outA)
	}
	return color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), uint8(outA*255 + 0.5)}
}

// downsample 面积平均下采样到 size x size（预乘空间累加，避免边缘发黑）
func downsample(src *image.NRGBA, size int) *image.NRGBA {
	factor := superSample
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	sb := src.Bounds()

	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			var sumR, sumG, sumB, sumA uint64
			for sy := 0; sy < factor; sy++ {
				for sx := 0; sx < factor; sx++ {
					c := src.NRGBAAt(sb.Min.X+dx*factor+sx, sb.Min.Y+dy*factor+sy)
					a := uint64(c.A)
					sumR += uint64(c.R) * a / 255
					sumG += uint64(c.G) * a / 255
					sumB += uint64(c.B) * a / 255
					sumA += a
				}
			}
			n := uint64(factor * factor)
			avgA := sumA / n
			var r, g, b uint64
			if avgA > 0 {
				r = sumR / n * 255 / avgA
				g = sumG / n * 255 / avgA
				b = sumB / n * 255 / avgA
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{clamp8(r), clamp8(g), clamp8(b), clamp8(avgA)})
		}
	}
	return dst
}

func clamp8(v uint64) uint8 {
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ---------------------------------------------------------------- ICO 编码

// encodeBMPFrame 把图像编码成 ICO 内的 BMP 帧
//
// 结构：BITMAPINFOHEADER(40) + XOR 位图(32bpp BGRA, 自下而上) + AND 掩码。
// biHeight 写实际高度的两倍（XOR 与 AND 各占一份）。
func encodeBMPFrame(img *image.NRGBA) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	var buf bytes.Buffer

	header := make([]byte, 40)
	binary.LittleEndian.PutUint32(header[0:], 40)
	binary.LittleEndian.PutUint32(header[4:], uint32(w))
	binary.LittleEndian.PutUint32(header[8:], uint32(h*2))
	binary.LittleEndian.PutUint16(header[12:], 1)  // biPlanes
	binary.LittleEndian.PutUint16(header[14:], 32) // biBitCount
	binary.LittleEndian.PutUint32(header[16:], 0)  // biCompression = BI_RGB
	binary.LittleEndian.PutUint32(header[20:], uint32(w*h*4))
	buf.Write(header)

	// XOR 位图
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}

	// AND 掩码，位为 1 表示透明
	stride := ((w + 31) / 32) * 4
	mask := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		srcY := b.Min.Y + (h - 1 - y)
		for x := 0; x < w; x++ {
			if img.NRGBAAt(b.Min.X+x, srcY).A == 0 {
				mask[y*stride+x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	buf.Write(mask)

	return buf.Bytes()
}

// writeICO 按 ICONDIR + ICONDIRENTRY[] + 数据区 组装文件
func writeICO(w io.Writer, sizes []int, payloads [][]byte) error {
	if len(sizes) != len(payloads) {
		return fmt.Errorf("尺寸与数据数量不匹配")
	}

	if err := binary.Write(w, binary.LittleEndian, uint16(0)); err != nil { // reserved
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // type = ICON
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(len(sizes))); err != nil {
		return err
	}

	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		payload := payloads[i]

		dw, dh := byte(size), byte(size)
		if size >= 256 {
			dw, dh = 0, 0 // 0 表示 256
		}
		entry := make([]byte, 16)
		entry[0] = dw
		entry[1] = dh
		entry[2] = 0                                 // 调色板颜色数（真彩色填 0）
		entry[3] = 0                                 // reserved
		binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
		binary.LittleEndian.PutUint16(entry[6:], 32) // bitCount
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(payload)))
		binary.LittleEndian.PutUint32(entry[12:], uint32(offset))

		if _, err := w.Write(entry); err != nil {
			return err
		}
		offset += len(payload)
	}

	for _, payload := range payloads {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}
