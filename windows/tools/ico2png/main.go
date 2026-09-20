// Command ico2png 把 .ico 里的指定尺寸帧导出成 PNG。
//
// 纯粹用于开发期自检：确认生成的图标在小尺寸下没有糊掉、透明区域没有黑边。
// 运行期不需要它。
//
// 用法：
//
//	go run ./tools/ico2png <输入.ico> <输出.png> [尺寸=32]
package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"

	"workbuddystatus/internal/core"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "用法：ico2png <输入.ico> <输出.png> [尺寸=32]")
		os.Exit(2)
	}
	size := 32
	if len(os.Args) > 3 {
		v, err := strconv.Atoi(os.Args[3])
		if err != nil {
			fmt.Fprintf(os.Stderr, "尺寸参数无效: %v\n", err)
			os.Exit(2)
		}
		size = v
	}

	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal("读取 ICO 失败: %v", err)
	}
	images, err := core.ParseICO(raw)
	if err != nil {
		fatal("解析 ICO 失败: %v", err)
	}
	fmt.Printf("ICO 共 %d 帧:", len(images))
	for _, im := range images {
		kind := "BMP"
		if im.IsPNG {
			kind = "PNG"
		}
		fmt.Printf(" %dx%d(%s)", im.Width, im.Height, kind)
	}
	fmt.Println()

	frame, ok := core.PickBMP(images, size)
	if !ok {
		fatal("没有找到可用帧")
	}
	if frame.IsPNG {
		// PNG 帧直接落盘即可
		if err := os.WriteFile(os.Args[2], frame.Data, 0o644); err != nil {
			fatal("写入失败: %v", err)
		}
		fmt.Printf("已导出 %dx%d PNG 帧 -> %s\n", frame.Width, frame.Height, os.Args[2])
		return
	}

	img, err := decodeBMPFrame(frame)
	if err != nil {
		fatal("解码 BMP 帧失败: %v", err)
	}
	out, err := os.Create(os.Args[2])
	if err != nil {
		fatal("创建输出失败: %v", err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		fatal("编码 PNG 失败: %v", err)
	}
	fmt.Printf("已导出 %dx%d BMP 帧 -> %s\n", frame.Width, frame.Height, os.Args[2])
}

// decodeBMPFrame 把 ICO 里的 BMP 帧还原成 image.Image
func decodeBMPFrame(frame core.ICOImage) (image.Image, error) {
	data := frame.Data
	if len(data) < 40 {
		return nil, fmt.Errorf("帧数据过短")
	}
	w := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(data[8:12]))) / 2 // 含 AND 掩码，除以 2
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("帧尺寸异常 %dx%d", w, h)
	}
	pixels := data[40:]
	if len(pixels) < w*h*4 {
		return nil, fmt.Errorf("像素数据不足：需要 %d，实际 %d", w*h*4, len(pixels))
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		srcRow := (h - 1 - y) * w * 4 // BMP 自下而上
		for x := 0; x < w; x++ {
			i := srcRow + x*4
			img.SetNRGBA(x, y, color.NRGBA{
				R: pixels[i+2], G: pixels[i+1], B: pixels[i], A: pixels[i+3],
			})
		}
	}
	return img, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "错误: "+format+"\n", args...)
	os.Exit(1)
}
