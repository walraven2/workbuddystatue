// Package core 汇集与平台无关的业务逻辑：接口数据结构、解析、凭据读取、
// 配置读写与格式化。
//
// 之所以单独拆包，是为了让这些逻辑能在 macOS / Linux 上直接跑单元测试
// （GUI 与 Win32 互操作留在 package main，只在 windows 上编译）。
package core

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- 数据结构

// CreditPackage 单个积分套餐
type CreditPackage struct {
	Code   string
	Total  float64
	Remain float64
	Used   float64
	Frozen float64
	Unit   string
}

// CreditSummary 积分汇总
type CreditSummary struct {
	Packages         []CreditPackage
	IsPaidUser       bool
	SubscriptionCode string
}

// TotalRemain 所有套餐剩余积分之和
func (s *CreditSummary) TotalRemain() float64 {
	var sum float64
	for _, p := range s.Packages {
		sum += p.Remain
	}
	return sum
}

// TotalCapacity 所有套餐总容量之和
func (s *CreditSummary) TotalCapacity() float64 {
	var sum float64
	for _, p := range s.Packages {
		sum += p.Total
	}
	return sum
}

// TotalUsed 所有套餐已用量之和
func (s *CreditSummary) TotalUsed() float64 {
	var sum float64
	for _, p := range s.Packages {
		sum += p.Used
	}
	return sum
}

// ActivePackages 仍有额度的套餐，按剩余量降序
func (s *CreditSummary) ActivePackages() []CreditPackage {
	out := make([]CreditPackage, 0, len(s.Packages))
	for _, p := range s.Packages {
		if p.Remain > 0.0001 {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Remain > out[j].Remain })
	return out
}

// ExhaustedPackages 已用尽的套餐
func (s *CreditSummary) ExhaustedPackages() []CreditPackage {
	out := make([]CreditPackage, 0)
	for _, p := range s.Packages {
		if p.Remain <= 0.0001 {
			out = append(out, p)
		}
	}
	return out
}

// UsageRatio 剩余占比，0..1
func (s *CreditSummary) UsageRatio() float64 {
	total := s.TotalCapacity()
	if total <= 0 {
		return 0
	}
	r := s.TotalRemain() / total
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	return r
}

// CheckinStatus 签到状态
type CheckinStatus struct {
	Active          bool
	TodayCheckedIn  bool
	StreakDays      int
	DailyCredit     float64
	TodayCredit     float64
	WeekCheckinDays int
}

// ---------------------------------------------------------------- 套餐名映射

// packageNames 对应 WorkBuddy 桌面端内置的 COMMODITY_CODES
var packageNames = map[string]string{
	"TCACA_code_001_PqouKr6QWV": "免费版",
	"TCACA_code_002_AkiJS3ZHF5": "专业版（月）",
	"TCACA_code_003_FAnt7lcmRT": "专业版（年）",
	"TCACA_code_005_maRGyrHhw1": "专业版 Plus（月）",
	"TCACA_code_006_DbXS0lrypC": "专业版试用",
	"TCACA_code_007_nzdH5h4Nl0": "成长计划（活动）",
	"TCACA_code_008_cfWoLwvjU4": "专业版（按日）",
	"TCACA_code_009_0XmEQc2xOf": "积分加油包",
	"TCACA_code_023_4xbGhMrE6q": "青春版",
	"TCACA_code_026_BaESVICNoi": "高级版",
	"TCACA_code_027_0FCGVA6vSa": "旗舰版",
	"TCACA_code_028_NtpWi0jzXs": "奖励积分 A",
	"TCACA_code_029_6wCGEWquYy": "奖励积分 B",
	"TCACA_code_030_BjSt89qTvr": "奖励积分 C",
	"TCACA_code_035_ArVxJcGDsm": "专业版（国际）",
	"TCACA_code_036_lupO5WgNdG": "积分包（国际）",
	"TCACA_code_037_WxOD3MpI2o": "奖励积分（国际）",
	"TCACA_code_038_OhvqZtiPKr": "积分包 D",
	"TCACA_code_039_KRcQj7wUat": "专业版试用（月）",
	"TCACA_code_040_mi9rCYg46x": "专业版试用（年）",
}

// PackageDisplayName 把套餐代码转成可读名称，未知代码走兜底
func PackageDisplayName(code string) string {
	if name, ok := packageNames[code]; ok {
		return name
	}
	parts := strings.Split(code, "_")
	for i, p := range parts {
		if p == "code" && i+1 < len(parts) {
			return "套餐 " + parts[i+1]
		}
	}
	return code
}

// ---------------------------------------------------------------- 类型转换
//
// 该接口的数字字段全部以字符串下发（如 "1371.48000616"），
// 但不同环境也可能给数字或布尔，因此统一做宽松转换。

func asFloat(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	if s == "" || s == "null" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func asInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	if s == "" || s == "null" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(v)
}

func asBool(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	s := strings.ToLower(strings.Trim(strings.TrimSpace(string(raw)), `"`))
	return s == "true" || s == "1"
}

func asString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

// ---------------------------------------------------------------- 解析

// envelope 是所有接口的统一外层
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func parseEnvelope(body []byte) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, newError(KindDecoding, "响应不是合法 JSON："+truncate(err.Error(), 160))
	}
	return &env, nil
}

// ParseSummary 解析 /billing/meter/get-user-resource-summary 的响应
func ParseSummary(body []byte) (*CreditSummary, error) {
	env, err := parseEnvelope(body)
	if err != nil {
		return nil, err
	}
	if env.Code != 0 {
		return nil, apiErrorFromCode(env.Code, env.Msg)
	}

	var data struct {
		Packages                []map[string]json.RawMessage `json:"Packages"`
		IsPaidUser              json.RawMessage              `json:"IsPaidUser"`
		SubscriptionPackageCode string                       `json:"SubscriptionPackageCode"`
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, newError(KindDecoding, "响应缺少 data 字段")
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, newError(KindDecoding, "data 结构解析失败："+truncate(err.Error(), 160))
	}

	summary := &CreditSummary{
		IsPaidUser:       asBool(data.IsPaidUser),
		SubscriptionCode: data.SubscriptionPackageCode,
	}
	for _, item := range data.Packages {
		summary.Packages = append(summary.Packages, CreditPackage{
			Code:   asString(item["PackageCode"]),
			Total:  asFloat(item["CycleTotalCapacity"]),
			Remain: asFloat(item["CycleRemainCapacity"]),
			Used:   asFloat(item["CycleUsedCapacity"]),
			Frozen: asFloat(item["CycleFrozenCapacity"]),
			Unit:   asString(item["CapacityUnit"]),
		})
	}
	return summary, nil
}

// ParseCheckin 解析 /billing/meter/checkin-activity-status 的响应
func ParseCheckin(body []byte) (*CheckinStatus, error) {
	env, err := parseEnvelope(body)
	if err != nil {
		return nil, err
	}
	if env.Code != 0 {
		return nil, apiErrorFromCode(env.Code, env.Msg)
	}

	var data struct {
		Active          json.RawMessage `json:"active"`
		TodayCheckedIn  json.RawMessage `json:"today_checked_in"`
		StreakDays      json.RawMessage `json:"streak_days"`
		DailyCredit     json.RawMessage `json:"daily_credit"`
		TodayCredit     json.RawMessage `json:"today_credit"`
		WeekCheckinDays json.RawMessage `json:"week_checkin_days"`
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, newError(KindDecoding, "响应缺少 data 字段")
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, newError(KindDecoding, "data 结构解析失败："+truncate(err.Error(), 160))
	}

	return &CheckinStatus{
		Active:          asBool(data.Active),
		TodayCheckedIn:  asBool(data.TodayCheckedIn),
		StreakDays:      asInt(data.StreakDays),
		DailyCredit:     asFloat(data.DailyCredit),
		TodayCredit:     asFloat(data.TodayCredit),
		WeekCheckinDays: asInt(data.WeekCheckinDays),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
