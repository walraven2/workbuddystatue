package core

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// ---------------------------------------------------------------- 内存布局
//
// 这是整个 Windows 版本里最容易「编译通过但运行炸」的地方：
// NOTIFYICONDATAW 是直接按字节传给 Shell_NotifyIconW 的，
// 一旦字段偏移或总长度错了，托盘图标就注册不上。
// 由于无法在 macOS 上运行 exe，这里用编译期结构体布局做等价校验。

func TestNotifyIconDataLayout(t *testing.T) {
	var nid NotifyIconData

	if got := unsafe.Sizeof(nid); got != NotifyIconDataSizeV2 {
		t.Fatalf("sizeof(NOTIFYICONDATAW) = %d，期望 %d", got, NotifyIconDataSizeV2)
	}

	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"cbSize", unsafe.Offsetof(nid.CbSize), 0},
		{"hWnd", unsafe.Offsetof(nid.HWnd), 8},
		{"uID", unsafe.Offsetof(nid.UID), 16},
		{"uFlags", unsafe.Offsetof(nid.UFlags), 20},
		{"uCallbackMessage", unsafe.Offsetof(nid.UCallbackMessage), 24},
		{"hIcon", unsafe.Offsetof(nid.HIcon), 32},
		{"szTip", unsafe.Offsetof(nid.SzTip), 40},
		{"dwState", unsafe.Offsetof(nid.DwState), 296},
		{"dwStateMask", unsafe.Offsetof(nid.DwStateMask), 300},
		{"szInfo", unsafe.Offsetof(nid.SzInfo), 304},
		{"uTimeout/uVersion", unsafe.Offsetof(nid.UTimeoutVersion), 816},
		{"szInfoTitle", unsafe.Offsetof(nid.SzInfoTitle), 820},
		{"dwInfoFlags", unsafe.Offsetof(nid.DwInfoFlags), 948},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("偏移 %s = %d，期望 %d", c.name, c.got, c.want)
		}
	}

	// V3 在 V2 基础上追加 GUID(16) + hBalloonIcon(8)，合计 976 —— 与公开资料一致
	if v3 := NotifyIconDataSizeV2 + 16 + 8; v3 != 976 {
		t.Errorf("推导出的 V3 尺寸 %d，期望 976", v3)
	}
}

// ---------------------------------------------------------------- 接口解析

const sampleSummary = `{
  "code": 0,
  "msg": "OK",
  "requestId": "69b37ed3-cc10-435d-be38-5af4e7663720",
  "data": {
    "Packages": [
      {"PackageCode":"TCACA_code_007_nzdH5h4Nl0","CycleTotalCapacity":"7100","CycleRemainCapacity":"1371.48000616","CycleUsedCapacity":"5728.51999384","CycleFrozenCapacity":"0","CapacityUnit":"credits"},
      {"PackageCode":"TCACA_code_026_BaESVICNoi","CycleTotalCapacity":"4000","CycleRemainCapacity":"0","CycleUsedCapacity":"4000","CycleFrozenCapacity":"0","CapacityUnit":"credits"},
      {"PackageCode":"TCACA_code_028_NtpWi0jzXs","CycleTotalCapacity":"5000","CycleRemainCapacity":"0","CycleUsedCapacity":"5000","CycleFrozenCapacity":"0","CapacityUnit":"credits"},
      {"PackageCode":"TCACA_code_029_6wCGEWquYy","CycleTotalCapacity":"142","CycleRemainCapacity":"142","CycleUsedCapacity":"0","CycleFrozenCapacity":"0","CapacityUnit":"credits"}
    ],
    "SubscriptionPackageCode": "TCACA_code_026_BaESVICNoi",
    "IsPaidUser": true,
    "IsProtectedPriceUser": false
  }
}`

func TestParseSummary(t *testing.T) {
	s, err := ParseSummary([]byte(sampleSummary))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(s.Packages) != 4 {
		t.Fatalf("套餐数 = %d，期望 4", len(s.Packages))
	}
	if !s.IsPaidUser {
		t.Error("IsPaidUser 应为 true")
	}
	if s.SubscriptionCode != "TCACA_code_026_BaESVICNoi" {
		t.Errorf("SubscriptionPackageCode 解析错误：%q", s.SubscriptionCode)
	}

	if got, want := s.TotalRemain(), 1371.48000616+142; abs(got-want) > 1e-6 {
		t.Errorf("剩余合计 = %v，期望 %v", got, want)
	}
	if got, want := s.TotalCapacity(), 7100.0+4000+5000+142; abs(got-want) > 1e-6 {
		t.Errorf("总量合计 = %v，期望 %v", got, want)
	}
	if got, want := s.TotalUsed(), 5728.51999384+4000+5000; abs(got-want) > 1e-6 {
		t.Errorf("已用合计 = %v，期望 %v", got, want)
	}

	active := s.ActivePackages()
	if len(active) != 2 {
		t.Fatalf("有效套餐数 = %d，期望 2", len(active))
	}
	// 按剩余量降序，成长计划在前
	if active[0].Code != "TCACA_code_007_nzdH5h4Nl0" {
		t.Errorf("排序错误，首个是 %q", active[0].Code)
	}
	if exhausted := s.ExhaustedPackages(); len(exhausted) != 2 {
		t.Errorf("已用尽套餐数 = %d，期望 2", len(exhausted))
	}

	// 1513.48 / 16242 ≈ 9.3%
	if r := s.UsageRatio(); r < 0.09 || r > 0.10 {
		t.Errorf("使用率 = %v，期望落在 9%%~10%%", r)
	}
}

func TestParseSummaryErrorCode(t *testing.T) {
	_, err := ParseSummary([]byte(`{"code":40301,"msg":"token invalid"}`))
	if err == nil {
		t.Fatal("应当返回错误")
	}
	if AsError(err).Kind != KindUnauthorized {
		t.Errorf("错误种类 = %q，期望 %q", AsError(err).Kind, KindUnauthorized)
	}
}

func TestParseSummaryNumericAndBoolVariants(t *testing.T) {
	// 接口理论上可能把数字给成 JSON number、布尔给成字符串
	body := `{"code":0,"data":{"Packages":[{"PackageCode":"X","CycleTotalCapacity":100,"CycleRemainCapacity":42.5,"CycleUsedCapacity":57.5}],"IsPaidUser":"true"}}`
	s, err := ParseSummary([]byte(body))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !s.IsPaidUser {
		t.Error(`IsPaidUser="true" 应解析为 true`)
	}
	if len(s.Packages) != 1 || s.Packages[0].Remain != 42.5 {
		t.Fatalf("数字型字段解析错误：%+v", s.Packages)
	}
}

func TestParseSummaryInvalidJSON(t *testing.T) {
	_, err := ParseSummary([]byte(`<html>502 Bad Gateway</html>`))
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
	if AsError(err).Kind != KindDecoding {
		t.Errorf("错误种类 = %q，期望 %q", AsError(err).Kind, KindDecoding)
	}
}

func TestParseCheckin(t *testing.T) {
	body := `{"code":0,"data":{"active":true,"today_checked_in":true,"streak_days":2,"daily_credit":"20","today_credit":"20","week_checkin_days":2}}`
	c, err := ParseCheckin([]byte(body))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !c.Active || !c.TodayCheckedIn || c.StreakDays != 2 || c.WeekCheckinDays != 2 {
		t.Fatalf("解析结果不符：%+v", c)
	}
	if c.DailyCredit != 20 {
		t.Errorf("daily_credit = %v，期望 20", c.DailyCredit)
	}
}

// ---------------------------------------------------------------- 格式化

func TestNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1494, "1,494"},
		{1513, "1,513"},
		{16242, "16,242"},
		// 与 macOS 版一致：>= 100 时不显示小数（菜单里是「剩余 1,507 / 16,242」）
		{1371.48000616, "1,371"},
		{99.5, "99.50"},
		{0, "0.00"},
		{142, "142"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, c := range cases {
		if got := Number(c.in); got != c.want {
			t.Errorf("Number(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestCompactNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1494, "1494"},
		{9, "9.00"},
		{42.5, "42.5"},
		{12345, "1.23万"},
		{250000000, "2.5亿"},
	}
	for _, c := range cases {
		if got := CompactNumber(c.in); got != c.want {
			t.Errorf("CompactNumber(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestGroupThousands(t *testing.T) {
	cases := map[string]string{
		"1":        "1",
		"12":       "12",
		"123":      "123",
		"1234":     "1,234",
		"12345":    "12,345",
		"123456":   "123,456",
		"1234567":  "1,234,567",
		"1234.56":  "1,234.56",
		"-1234567": "-1,234,567",
		"0":        "0",
	}
	for in, want := range cases {
		if got := groupThousands(in); got != want {
			t.Errorf("groupThousands(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestStatusText(t *testing.T) {
	s := &CreditSummary{Packages: []CreditPackage{{Total: 16242, Remain: 1513.48}}}
	if got := StatusText(DisplayValue, s, "⚡ "); got != "⚡ 1513" {
		t.Errorf("数值模式 = %q，期望 %q", got, "⚡ 1513")
	}
	if got := StatusText(DisplayPercent, s, "⚡ "); got != "⚡ 9%" {
		t.Errorf("百分比模式 = %q，期望 %q", got, "⚡ 9%")
	}
	if got := StatusText(DisplayIconOnly, s, "⚡ "); got != "⚡" {
		t.Errorf("仅图标模式 = %q，期望 %q", got, "⚡")
	}
	if got := StatusText(DisplayValue, nil, "⚡ "); got != "⚡" {
		t.Errorf("无数据时应只显示前缀，得到 %q", got)
	}
	if got := StatusText(DisplayValue, s, ""); got != DefaultPrefix+"1513" {
		t.Errorf("空前缀应回落到默认值，得到 %q", got)
	}
}

func TestDisplayModeRoundTrip(t *testing.T) {
	for _, m := range []DisplayMode{DisplayValue, DisplayPercent, DisplayIconOnly} {
		if got := ParseDisplayMode(m.Key()); got != m {
			t.Errorf("ParseDisplayMode(%q) = %v，期望 %v", m.Key(), got, m)
		}
	}
	if ParseDisplayMode("乱码") != DisplayValue {
		t.Error("无法识别的模式应回落到数值模式")
	}
}

func TestPackageDisplayName(t *testing.T) {
	if got := PackageDisplayName("TCACA_code_007_nzdH5h4Nl0"); got != "成长计划（活动）" {
		t.Errorf("已知代码映射错误：%q", got)
	}
	if got := PackageDisplayName("TCACA_code_999_abcdef"); got != "套餐 999" {
		t.Errorf("未知代码兜底错误：%q", got)
	}
	if got := PackageDisplayName("garbage"); got != "garbage" {
		t.Errorf("无法识别时应原样返回：%q", got)
	}
}

// ---------------------------------------------------------------- 凭据

func TestParseAuthFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workbuddy-desktop.info")
	content := `{
	  "auth": {"accessToken":"eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3OTI4ODk5ODB9.sig","expiresAt":1792889980000,"tokenType":"bearer"},
	  "account": {"uid":"6b48922b-d782-47fb-8409-fe04c7e24d77","nickname":"测试用户"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cred, err := parseAuthFile(path, DefaultEndpoint)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if cred.UserID != "6b48922b-d782-47fb-8409-fe04c7e24d77" {
		t.Errorf("uid 解析错误：%q", cred.UserID)
	}
	if cred.Nickname != "测试用户" {
		t.Errorf("nickname 解析错误：%q", cred.Nickname)
	}
	if !cred.HasExpiry {
		t.Error("应解析出过期时间")
	}
	// 1792889980 秒 -> 2026-10-25 前后
	if y := cred.ExpiresAt.UTC().Year(); y != 2026 {
		t.Errorf("毫秒时间戳换算错误，年份 = %d", y)
	}
	if cred.DisplayName() != "测试用户" {
		t.Errorf("DisplayName = %q", cred.DisplayName())
	}
}

func TestParseAuthFileMissingToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workbuddy-desktop.info")
	if err := os.WriteFile(path, []byte(`{"auth":{},"account":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseAuthFile(path, DefaultEndpoint); err == nil {
		t.Fatal("缺少 accessToken 时应报错")
	}
}

func TestJWTExpiry(t *testing.T) {
	// payload = {"exp":1792889980}
	token := "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3OTI4ODk5ODB9.sig"
	exp, ok := jwtExpiry(token)
	if !ok {
		t.Fatal("应能解析出 exp")
	}
	if exp.Unix() != 1792889980 {
		t.Errorf("exp = %d，期望 1792889980", exp.Unix())
	}
	if _, ok := jwtExpiry("not-a-jwt"); ok {
		t.Error("非 JWT 不应解析成功")
	}
}

func TestCredentialDirsContainWindowsLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
	t.Setenv("APPDATA", `C:\Users\tester\AppData\Roaming`)
	dirs := CandidateAuthDirs()

	want := filepath.Join(`C:\Users\tester\AppData\Local`, "CodeBuddyExtension", "Data", "Public", "auth")
	found := false
	for _, d := range dirs {
		if d == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("候选目录中缺少 LocalAppData 路径 %q，实际为：%v", want, dirs)
	}

	// 去重检查
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("候选目录重复：%q", d)
		}
		seen[d] = true
	}
}

// ---------------------------------------------------------------- ICO

// buildTestICO 构造一个含 BMP 帧与 PNG 帧的最小 ICO
func buildTestICO() []byte {
	bmpPayload := make([]byte, 48)
	binary.LittleEndian.PutUint32(bmpPayload[0:4], 40)  // BITMAPINFOHEADER 大小
	binary.LittleEndian.PutUint32(bmpPayload[4:8], 16)  // width
	binary.LittleEndian.PutUint32(bmpPayload[8:12], 32) // height（含掩码）

	pngPayload := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

	var out []byte
	out = append(out, 0, 0, 1, 0, 2, 0) // reserved=0 type=1 count=2

	headerLen := 6 + 2*16
	bmpOff := headerLen
	pngOff := bmpOff + len(bmpPayload)

	bmpEntry := make([]byte, 16)
	bmpEntry[0] = 16
	bmpEntry[1] = 16
	binary.LittleEndian.PutUint16(bmpEntry[4:6], 1)
	binary.LittleEndian.PutUint16(bmpEntry[6:8], 32)
	binary.LittleEndian.PutUint32(bmpEntry[8:12], uint32(len(bmpPayload)))
	binary.LittleEndian.PutUint32(bmpEntry[12:16], uint32(bmpOff))

	pngEntry := make([]byte, 16)
	pngEntry[0] = 0 // 0 表示 256
	pngEntry[1] = 0
	binary.LittleEndian.PutUint16(pngEntry[4:6], 1)
	binary.LittleEndian.PutUint16(pngEntry[6:8], 32)
	binary.LittleEndian.PutUint32(pngEntry[8:12], uint32(len(pngPayload)))
	binary.LittleEndian.PutUint32(pngEntry[12:16], uint32(pngOff))

	out = append(out, bmpEntry...)
	out = append(out, pngEntry...)
	out = append(out, bmpPayload...)
	out = append(out, pngPayload...)
	return out
}

func TestParseICO(t *testing.T) {
	imgs, err := ParseICO(buildTestICO())
	if err != nil {
		t.Fatalf("解析 ICO 失败：%v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("帧数 = %d，期望 2", len(imgs))
	}
	if imgs[0].Width != 16 || imgs[0].IsPNG {
		t.Errorf("第一帧应为 16x16 BMP：%+v", imgs[0])
	}
	if imgs[1].Width != 256 || !imgs[1].IsPNG {
		t.Errorf("第二帧应为 256x256 PNG（尺寸 0 解释为 256）：%+v", imgs[1])
	}
	// ICONDIRENTRY 里的元数据要原样透传，写 RT_GROUP_ICON 时要用
	if imgs[0].Planes != 1 || imgs[0].BitCount != 32 {
		t.Errorf("planes/bitCount 应透传自目录项：%+v", imgs[0])
	}
}

func TestPick(t *testing.T) {
	imgs, err := ParseICO(buildTestICO())
	if err != nil {
		t.Fatal(err)
	}
	// 请求 32：没有精确匹配，应回退到不小于目标的最小帧（256 的 PNG）
	got, ok := Pick(imgs, 32)
	if !ok || got.Width != 256 {
		t.Errorf("Pick(32) = %+v, ok=%v；期望回退到 256", got, ok)
	}
	// 请求 16：精确匹配
	got, ok = Pick(imgs, 16)
	if !ok || got.Width != 16 {
		t.Errorf("Pick(16) = %+v, ok=%v", got, ok)
	}
	// 优先取 BMP
	got, ok = PickBMP(imgs, 32)
	if !ok || got.IsPNG {
		t.Errorf("PickBMP(32) 应返回 BMP 帧，得到 %+v", got)
	}
}

func TestParseICOBadInput(t *testing.T) {
	if _, err := ParseICO([]byte{1, 2, 3}); err == nil {
		t.Error("过短数据应报错")
	}
	if _, err := ParseICO([]byte{0, 0, 9, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Error("type 非 1 应报错")
	}
}

// ---------------------------------------------------------------- 摘要文本

func TestSummaryText(t *testing.T) {
	s, err := ParseSummary([]byte(sampleSummary))
	if err != nil {
		t.Fatal(err)
	}
	text := SummaryText("测试用户", s, "")
	for _, want := range []string{"账户：测试用户", "剩余积分：1,513", "成长计划（活动）", "奖励积分 B"} {
		if !strings.Contains(text, want) {
			t.Errorf("摘要缺少 %q，实际为：\n%s", want, text)
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
