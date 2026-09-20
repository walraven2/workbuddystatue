// mkrsrc 生成 Windows 资源目标文件（rsrc_windows_amd64.syso）。
//
// 为什么需要它：Go 链接器不会自己往 exe 里塞图标和清单。但只要有任何一个
// 目标文件带有名为 ".rsrc" 的节，cmd/link 就会把它原样搬进 PE，并顺带
// 填好资源数据目录 —— 见 cmd/link/internal/ld/pe.go 的 addpersrc()。
//
// 于是这里手写一个 24 行的 COFF 目标文件：
//
//	COFF 头 + 节头 + .rsrc 节数据 + 重定位表 + 符号表
//
// .rsrc 里的 IMAGE_RESOURCE_DATA_ENTRY.OffsetToData 必须是 RVA，而 RVA 只有
// 链接时才知道，所以每个这样的字段都挂一条 IMAGE_REL_AMD64_ADDR32 重定位，
// 段内偏移作为加数写在字段本身里（x64 COFF 的加数是内联的）。
//
// 用法：
//
//	go run ./tools/mkrsrc -ico Resources/AppIcon.ico \
//	    -manifest Resources/app.manifest -version 1.2.0 \
//	    -o rsrc_windows_amd64.syso
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"workbuddystatus/internal/core"
)

// 资源类型 ID
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langNeutral = 1033 // en-US；图标与清单对语言不敏感，取一个具体的即可
)

// COFF / PE 常量
const (
	coffMachineAmd64      = 0x8664
	sectionHeaderSize     = 40
	coffHeaderSize        = 20
	symbolEntrySize       = 18
	relocEntrySize        = 10
	symClassStatic        = 3
	imageRelAmd64Addr32   = 0x0002
	scnCntInitializedData = 0x00000040
	scnMemRead            = 0x40000000
)

func main() {
	var (
		icoPath      = flag.String("ico", "Resources/AppIcon.ico", "源 .ico 文件")
		manifestPath = flag.String("manifest", "Resources/app.manifest", "清单 XML 文件")
		outPath      = flag.String("o", "rsrc_windows_amd64.syso", "输出的目标文件")
		version      = flag.String("version", "1.2.0", "版本号，如 1.2.0")
		productName  = flag.String("product", "WorkBuddy 积分托盘小工具", "产品名称")
		companyName  = flag.String("company", "WorkBuddyStatus", "公司 / 作者名")
	)
	flag.Parse()

	if err := run(*icoPath, *manifestPath, *version, *productName, *companyName, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "生成失败："+err.Error())
		os.Exit(1)
	}
}

func run(icoPath, manifestPath, version, productName, companyName, outPath string) error {
	rawICO, err := os.ReadFile(icoPath)
	if err != nil {
		return fmt.Errorf("读取图标失败：%w", err)
	}
	images, err := core.ParseICO(rawICO)
	if err != nil {
		return fmt.Errorf("解析图标失败：%w", err)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("读取清单失败：%w", err)
	}

	// ── 组装资源树：类型 -> 名称 -> 语言
	iconType := &resNode{id: rtIcon}
	for i, img := range images {
		iconType.children = append(iconType.children,
			langNode(uint32(i+1), img.Data))
	}

	groupType := &resNode{id: rtGroupIcon}
	groupType.children = append(groupType.children,
		langNode(1, buildGroupIcon(images)))

	versionType := &resNode{id: rtVersion}
	versionType.children = append(versionType.children,
		langNode(1, buildVersionInfo(version, productName, companyName)))

	manifestType := &resNode{id: rtManifest}
	manifestType.children = append(manifestType.children,
		langNode(1, manifest))

	root := &resNode{children: []*resNode{iconType, groupType, versionType, manifestType}}

	// ── 计算布局
	blob, relocs := layout(root)

	// ── 写 COFF
	obj, err := buildCOFF(blob, relocs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil && filepath.Dir(outPath) != "." {
		return err
	}
	if err := os.WriteFile(outPath, obj, 0o644); err != nil {
		return err
	}

	fmt.Printf("已生成 %s\n", outPath)
	fmt.Printf("  图标帧      : %d 个（%s）\n", len(images), frameList(images))
	fmt.Printf("  .rsrc 大小  : %d 字节\n", len(blob))
	fmt.Printf("  重定位      : %d 条\n", len(relocs))
	fmt.Printf("  目标文件    : %d 字节\n", len(obj))
	fmt.Printf("  版本资源    : %s\n", version)
	return nil
}

func frameList(images []core.ICOImage) string {
	var parts []string
	for _, im := range images {
		parts = append(parts, strconv.Itoa(im.Width))
	}
	return strings.Join(parts, "/")
}

// ---------------------------------------------------------------- 资源树

// resNode 资源目录树里的一个节点。
//
// 树固定三层：类型（3/14/16/24）-> 名称（ID）-> 语言（叶子）。
type resNode struct {
	id       uint32
	children []*resNode
	data     []byte

	dirOff       uint32 // IMAGE_RESOURCE_DIRECTORY 在节内的偏移
	dataEntryOff uint32 // IMAGE_RESOURCE_DATA_ENTRY 在节内的偏移（仅叶子）
	payloadOff   uint32 // 实际数据在节内的偏移（仅叶子）
}

func (n *resNode) isDir() bool { return len(n.children) > 0 }

// langNode 造一个「名称 -> 语言」两层结构，语言层是叶子
func langNode(name uint32, data []byte) *resNode {
	return &resNode{
		id:       name,
		children: []*resNode{{id: langNeutral, data: data}},
	}
}

// layout 为每个节点分配偏移，并把目录、数据项、载荷依次写进一个缓冲区。
// 返回的 relocs 指出哪些 4 字节字段需要链接器回填 RVA。
func layout(root *resNode) ([]byte, []coffReloc) {
	// 第一遍：BFS 给所有目录节点分配位置，同时按 BFS 顺序收集叶子
	var (
		dirCursor uint32
		leaves    []*resNode
		queue     = []*resNode{root}
	)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		n.dirOff = dirCursor
		dirCursor += 16 + 8*uint32(len(n.children))

		for _, c := range n.children {
			if c.isDir() {
				queue = append(queue, c)
			} else {
				leaves = append(leaves, c)
			}
		}
	}

	// 第二遍：数据项紧接目录，然后是对齐后的载荷
	entryCursor := dirCursor
	for _, l := range leaves {
		l.dataEntryOff = entryCursor
		entryCursor += 16
	}
	payloadCursor := align4(entryCursor)
	for _, l := range leaves {
		l.payloadOff = payloadCursor
		payloadCursor += align4(uint32(len(l.data)))
	}

	blob := make([]byte, payloadCursor)
	var relocs []coffReloc

	// 写目录
	queue = []*resNode{root}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		kids := append([]*resNode(nil), n.children...)
		sortByID(kids)

		off := n.dirOff
		// IMAGE_RESOURCE_DIRECTORY：Characteristics/TimeDateStamp 全 0，
		// 版本号也留空，只用 NumberOfIdEntries
		binary.LittleEndian.PutUint16(blob[off+12:], 0)                 // NumberOfNamedEntries
		binary.LittleEndian.PutUint16(blob[off+14:], uint16(len(kids))) // NumberOfIdEntries
		off += 16

		for _, c := range kids {
			binary.LittleEndian.PutUint32(blob[off:], c.id)
			if c.isDir() {
				// 高位为 1 表示指向下一层目录
				binary.LittleEndian.PutUint32(blob[off+4:], 0x80000000|c.dirOff)
				queue = append(queue, c)
			} else {
				// 高位为 0 表示指向 IMAGE_RESOURCE_DATA_ENTRY
				binary.LittleEndian.PutUint32(blob[off+4:], c.dataEntryOff)
			}
			off += 8
		}
	}

	// 写数据项与载荷
	for _, l := range leaves {
		de := l.dataEntryOff
		binary.LittleEndian.PutUint32(blob[de:], l.payloadOff) // 加数，等链接器补 RVA
		binary.LittleEndian.PutUint32(blob[de+4:], uint32(len(l.data)))
		binary.LittleEndian.PutUint32(blob[de+8:], 0)  // CodePage
		binary.LittleEndian.PutUint32(blob[de+12:], 0) // Reserved

		relocs = append(relocs, coffReloc{off: de, addend: l.payloadOff})

		copy(blob[l.payloadOff:], l.data)
	}

	return blob, relocs
}

func sortByID(nodes []*resNode) {
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j-1].id > nodes[j].id; j-- {
			nodes[j-1], nodes[j] = nodes[j], nodes[j-1]
		}
	}
}

func align4(n uint32) uint32 { return (n + 3) &^ 3 }

// ---------------------------------------------------------------- 图标组

// buildGroupIcon 生成 RT_GROUP_ICON 的载荷（GRPICONDIR）
func buildGroupIcon(images []core.ICOImage) []byte {
	var b bytes.Buffer
	writeU16(&b, 0)                   // idReserved
	writeU16(&b, 1)                   // idType = 图标
	writeU16(&b, uint16(len(images))) // idCount

	for i, im := range images {
		b.WriteByte(byte(dimByte(im.Width)))
		b.WriteByte(byte(dimByte(im.Height)))
		b.WriteByte(im.ColorCount)
		b.WriteByte(0) // bReserved
		writeU16(&b, im.Planes)
		writeU16(&b, im.BitCount)
		writeU32(&b, uint32(len(im.Data)))
		writeU16(&b, uint16(i+1)) // 对应 RT_ICON 的 ID
	}
	return b.Bytes()
}

// dimByte 尺寸字段是单字节，256 用 0 表示
func dimByte(v int) int {
	if v >= 256 {
		return 0
	}
	return v
}

// ---------------------------------------------------------------- 版本资源

// buildVersionInfo 生成 RT_VERSION 的载荷（VS_VERSIONINFO）
func buildVersionInfo(version, productName, companyName string) []byte {
	major, minor, patch, build := parseVersion(version)

	// VS_FIXEDFILEINFO，13 个 DWORD = 52 字节
	var fixed bytes.Buffer
	writeU32(&fixed, 0xFEEF04BD)                      // dwSignature
	writeU32(&fixed, 0x00010000)                      // dwStrucVersion
	writeU32(&fixed, uint32(major)<<16|uint32(minor)) // dwFileVersionMS
	writeU32(&fixed, uint32(patch)<<16|uint32(build)) // dwFileVersionLS
	writeU32(&fixed, uint32(major)<<16|uint32(minor)) // dwProductVersionMS
	writeU32(&fixed, uint32(patch)<<16|uint32(build)) // dwProductVersionLS
	writeU32(&fixed, 0x3F)                            // dwFileFlagsMask
	writeU32(&fixed, 0)                               // dwFileFlags
	writeU32(&fixed, 0x00040004)                      // dwFileOS = VOS_NT_WINDOWS32
	writeU32(&fixed, 1)                               // dwFileType = VFT_APP
	writeU32(&fixed, 0)                               // dwFileSubtype
	writeU32(&fixed, 0)                               // dwFileDateMS
	writeU32(&fixed, 0)                               // dwFileDateLS

	// 语言 0x0804（简体中文）+ 代码页 0x04B0（Unicode）
	const langAndCodepage = "080404B0"

	strTable := verBlock("StringFileInfo", 0, 1, nil, [][]byte{
		// StringTable 的键就是「语言 + 代码页」
		verBlock(langAndCodepage, 0, 1, nil, [][]byte{
			verString("CompanyName", companyName),
			verString("FileDescription", productName),
			verString("FileVersion", version),
			verString("InternalName", "WorkBuddyStatus"),
			verString("OriginalFilename", "WorkBuddyStatus.exe"),
			verString("ProductName", productName),
			verString("ProductVersion", version),
			verString("LegalCopyright", "仅供个人使用"),
		}),
	})

	var trans bytes.Buffer
	writeU32(&trans, 0x080404B0) // 与上面 StringTable 的键一致
	varFileInfo := verBlock("VarFileInfo", 0, 1, nil, [][]byte{
		verBlock("Translation", 4, 0, trans.Bytes(), nil),
	})

	return verBlock("VS_VERSION_INFO", 52, 0, fixed.Bytes(),
		[][]byte{strTable, varFileInfo})
}

// verString 生成一个 String 节点。wValueLength 按 UTF-16 字符数（含结尾 NUL）计。
func verString(key, value string) []byte {
	encoded := utf16Bytes(value)
	valueLen := len(encoded)/2 + 1 // 字符串 + 结尾 NUL
	return verBlock(key, valueLen, 1, encoded, nil)
}

// verBlock 按 VS_VERSIONINFO 的格式拼一个节点
//
// 布局：wLength(2) wValueLength(2) wType(2) szKey(UTF-16 NUL 结尾)
// 对齐到 4 -> Value -> 对齐到 4 -> 子节点…
func verBlock(key string, valueLen, typ int, value []byte, children [][]byte) []byte {
	var b bytes.Buffer

	writeU16(&b, 0) // wLength，先占位
	writeU16(&b, uint16(valueLen))
	writeU16(&b, uint16(typ))
	b.Write(utf16Bytes(key))
	writeU16(&b, 0) // 键的结尾 NUL
	padTo4(&b)

	if value != nil {
		b.Write(value)
		padTo4(&b)
	}
	for _, c := range children {
		b.Write(c)
		padTo4(&b)
	}

	out := b.Bytes()
	binary.LittleEndian.PutUint16(out[0:2], uint16(len(out)))
	return out
}

func padTo4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func utf16Bytes(s string) []byte {
	var b bytes.Buffer
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			writeU16(&b, uint16(0xD800+(r>>10)))
			writeU16(&b, uint16(0xDC00+(r&0x3FF)))
			continue
		}
		writeU16(&b, uint16(r))
	}
	return b.Bytes()
}

// parseVersion 把 "1.2.3" 拆成四个数字，缺失的位补 0
func parseVersion(v string) (int, int, int, int) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	var nums [4]int
	for i := 0; i < len(parts) && i < 4; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nums[3]
}

// ---------------------------------------------------------------- COFF 输出

type coffReloc struct {
	off    uint32
	addend uint32
}

// buildCOFF 把 .rsrc 节数据包成一个最小可用的 x64 COFF 目标文件
func buildCOFF(section []byte, relocs []coffReloc) ([]byte, error) {
	sectionSize := align4(uint32(len(section)))

	dataOff := uint32(coffHeaderSize + sectionHeaderSize)
	relocOff := dataOff + sectionSize
	relocCount := uint32(len(relocs))
	symOff := relocOff + relocCount*relocEntrySize

	// 符号表：1 个节符号 + 1 条辅助记录
	const symCount = 2

	total := symOff + symCount*symbolEntrySize + 4 // 末尾是 4 字节的空字符串表
	out := make([]byte, total)

	// ── COFF 头
	binary.LittleEndian.PutUint16(out[0:], coffMachineAmd64)
	binary.LittleEndian.PutUint16(out[2:], 1) // NumberOfSections
	binary.LittleEndian.PutUint32(out[4:], 0) // TimeDateStamp：留 0 保证可复现
	binary.LittleEndian.PutUint32(out[8:], symOff)
	binary.LittleEndian.PutUint32(out[12:], symCount)
	binary.LittleEndian.PutUint16(out[16:], 0) // SizeOfOptionalHeader
	binary.LittleEndian.PutUint16(out[18:], 0) // Characteristics

	// ── 节头
	sh := out[coffHeaderSize : coffHeaderSize+sectionHeaderSize]
	copy(sh[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(sh[8:], 0)            // VirtualSize
	binary.LittleEndian.PutUint32(sh[12:], 0)           // VirtualAddress
	binary.LittleEndian.PutUint32(sh[16:], sectionSize) // SizeOfRawData
	binary.LittleEndian.PutUint32(sh[20:], dataOff)     // PointerToRawData
	binary.LittleEndian.PutUint32(sh[24:], relocOff)    // PointerToRelocations
	binary.LittleEndian.PutUint32(sh[28:], 0)           // PointerToLinenumbers
	binary.LittleEndian.PutUint16(sh[32:], uint16(relocCount))
	binary.LittleEndian.PutUint16(sh[34:], 0) // NumberOfLinenumbers
	binary.LittleEndian.PutUint32(sh[36:], scnCntInitializedData|scnMemRead)

	// ── 节数据
	copy(out[dataOff:], section)

	// ── 重定位表：每条把「节内偏移 = addend」告诉链接器
	for i, r := range relocs {
		p := out[relocOff+uint32(i)*relocEntrySize:]
		binary.LittleEndian.PutUint32(p[0:], r.off)
		binary.LittleEndian.PutUint32(p[4:], 0) // 指向下面第 0 个符号（节符号）
		binary.LittleEndian.PutUint16(p[8:], imageRelAmd64Addr32)
	}

	// ── 符号表
	// 第 0 项：节符号。StorageClass=STATIC、Type=0、名字以 '.' 开头，
	// 正好命中 ldpe.go 的 issect()，链接器会把它认成节符号。
	sym := out[symOff:]
	copy(sym[0:8], ".rsrc\x00\x00\x00")
	binary.LittleEndian.PutUint32(sym[8:], 0)  // Value
	binary.LittleEndian.PutUint16(sym[12:], 1) // SectionNumber（1 起）
	binary.LittleEndian.PutUint16(sym[14:], 0) // Type
	sym[16] = symClassStatic
	sym[17] = 1 // NumberOfAuxSymbols

	// 第 1 项：辅助节定义
	aux := sym[symbolEntrySize:]
	binary.LittleEndian.PutUint32(aux[0:], sectionSize) // Length
	binary.LittleEndian.PutUint16(aux[4:], uint16(relocCount))
	binary.LittleEndian.PutUint16(aux[6:], 0) // NumberOfLinenumbers
	binary.LittleEndian.PutUint32(aux[8:], 0) // CheckSum
	// 其余（Number/Selection/bReserved/HighNumber）保持 0

	if uint32(len(out)) != total {
		return nil, fmt.Errorf("输出长度异常：%d != %d", len(out), total)
	}
	return out, nil
}

// ---------------------------------------------------------------- 小工具

func writeU16(b *bytes.Buffer, v uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	b.Write(buf[:])
}

func writeU32(b *bytes.Buffer, v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}
