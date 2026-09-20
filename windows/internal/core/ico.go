package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ICOImage ICO 文件中的一帧图像
type ICOImage struct {
	Width  int
	Height int
	// 以下三项来自 ICONDIRENTRY，写 RT_GROUP_ICON 时需要原样带上
	ColorCount uint8
	Planes     uint16
	BitCount   uint16
	// Data 为 ICONIMAGE 原始字节，可直接交给 CreateIconFromResourceEx：
	// BMP 帧是 BITMAPINFOHEADER + 像素 + AND 掩码；PNG 帧是完整 PNG 数据。
	Data  []byte
	IsPNG bool
}

// icoHeaderSize ICONDIR 固定 6 字节
const icoHeaderSize = 6

// icoDirEntrySize ICONDIRENTRY 固定 16 字节
const icoDirEntrySize = 16

// ParseICO 解析 .ico 文件，返回其中所有帧
func ParseICO(data []byte) ([]ICOImage, error) {
	if len(data) < icoHeaderSize {
		return nil, errors.New("ICO 数据过短")
	}
	reserved := binary.LittleEndian.Uint16(data[0:2])
	kind := binary.LittleEndian.Uint16(data[2:4])
	count := int(binary.LittleEndian.Uint16(data[4:6]))

	if reserved != 0 {
		return nil, errors.New("ICO reserved 字段非 0")
	}
	if kind != 1 {
		return nil, fmt.Errorf("不是 ICO 文件（type=%d）", kind)
	}
	if count <= 0 {
		return nil, errors.New("ICO 中没有图像")
	}

	images := make([]ICOImage, 0, count)
	for i := 0; i < count; i++ {
		off := icoHeaderSize + i*icoDirEntrySize
		if off+icoDirEntrySize > len(data) {
			return nil, errors.New("ICO 目录项越界")
		}
		e := data[off : off+icoDirEntrySize]

		// 尺寸字段为 0 表示 256
		w := int(e[0])
		h := int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}

		size := int(binary.LittleEndian.Uint32(e[8:12]))
		imgOff := int(binary.LittleEndian.Uint32(e[12:16]))
		if imgOff < 0 || size < 0 || imgOff+size > len(data) {
			return nil, fmt.Errorf("ICO 第 %d 帧数据越界", i)
		}
		payload := data[imgOff : imgOff+size]

		images = append(images, ICOImage{
			Width:      w,
			Height:     h,
			ColorCount: e[2],
			Planes:     binary.LittleEndian.Uint16(e[4:6]),
			BitCount:   binary.LittleEndian.Uint16(e[6:8]),
			Data:       payload,
			IsPNG:      len(payload) >= 8 && payload[0] == 0x89 && payload[1] == 'P' && payload[2] == 'N' && payload[3] == 'G',
		})
	}
	if len(images) == 0 {
		return nil, errors.New("ICO 中没有可用帧")
	}
	return images, nil
}

// Pick 选择最适合指定尺寸的帧：优先精确匹配，其次不小于目标的最小帧，
// 最后回退到最大帧。
func Pick(images []ICOImage, size int) (ICOImage, bool) {
	if len(images) == 0 {
		return ICOImage{}, false
	}
	var exact *ICOImage
	var smallestLarger *ICOImage
	var largest *ICOImage

	for i := range images {
		im := &images[i]
		switch {
		case im.Width == size:
			if exact == nil {
				exact = im
			}
		case im.Width > size:
			if smallestLarger == nil || im.Width < smallestLarger.Width {
				smallestLarger = im
			}
		}
		if largest == nil || im.Width > largest.Width {
			largest = im
		}
	}
	switch {
	case exact != nil:
		return *exact, true
	case smallestLarger != nil:
		return *smallestLarger, true
	case largest != nil:
		return *largest, true
	}
	return ICOImage{}, false
}

// PickBMP 与 Pick 相同，但优先返回 BMP 帧
//
// CreateIconFromResourceEx 在部分 Windows 版本上对 PNG 帧支持不佳，
// 因此运行时加载图标时优先取 BMP 帧。
func PickBMP(images []ICOImage, size int) (ICOImage, bool) {
	var bmp []ICOImage
	for _, im := range images {
		if !im.IsPNG {
			bmp = append(bmp, im)
		}
	}
	if len(bmp) > 0 {
		return Pick(bmp, size)
	}
	return Pick(images, size)
}
