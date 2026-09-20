package core

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Credential 一份可用的 WorkBuddy 访问凭据
type Credential struct {
	AccessToken string
	UserID      string
	Nickname    string
	ExpiresAt   time.Time
	HasExpiry   bool
	Endpoint    string
	Source      string // 凭据来源，便于诊断
}

// DisplayName 账户展示名
func (c *Credential) DisplayName() string {
	if c.Nickname != "" {
		return c.Nickname
	}
	if c.UserID != "" {
		return c.UserID
	}
	return "WorkBuddy 用户"
}

// CandidateAuthDirs 返回按优先级排列的凭据目录候选
//
// 依据 WorkBuddy 桌面端源码中的 FilePathServiceImpl.getBasePath()：
//
//	darwin -> ~/Library/Application Support/CodeBuddyExtension
//	win32  -> %USERPROFILE%\AppData\Local\CodeBuddyExtension
//
// 再由 sharedDataPath = <base>/Data/Public，凭据文件是 <sharedDataPath>/auth/<id>.info
// 其中 authentication.id 固定为 workbuddy-desktop。
//
// 注意 Windows 用的是 AppData\Local（不是 Roaming），这里同时也把 Roaming
// 与旧版布局一并作为兜底，避免不同版本差异导致读不到。
func CandidateAuthDirs() []string {
	home := HomeDir()
	var dirs []string

	add := func(base string) {
		if base == "" {
			return
		}
		dirs = append(dirs, filepath.Join(base, "CodeBuddyExtension", "Data", "Public", "auth"))
		dirs = append(dirs, filepath.Join(base, "CodeBuddyExtension", "Data", "Public"))
	}

	add(os.Getenv("LOCALAPPDATA"))
	add(os.Getenv("APPDATA"))
	add(home)
	add(filepath.Join(home, ".local", "share"))

	// 兼容其它可能的布局
	dirs = append(dirs,
		filepath.Join(home, ".workbuddy", "auth"),
		filepath.Join(home, ".codebuddy", "auth"),
	)

	// 去重，保持顺序
	seen := map[string]bool{}
	out := dirs[:0]
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// LoadCredential 按「显式配置 -> 桌面端登录信息」的顺序获取凭据
func LoadCredential(cfg Config) (*Credential, error) {
	// 1) 配置或环境变量里显式给了令牌
	if tok := strings.TrimSpace(cfg.AccessToken); tok != "" {
		cred := &Credential{
			AccessToken: tok,
			UserID:      cfg.UserID,
			Endpoint:    cfg.Endpoint,
			Source:      "配置文件 / 环境变量",
		}
		if exp, ok := jwtExpiry(tok); ok {
			cred.ExpiresAt, cred.HasExpiry = exp, true
		}
		if err := validate(cred); err != nil {
			return nil, err
		}
		return cred, nil
	}

	// 2) 扫描桌面端登录信息
	var brokenPath string
	for _, dir := range CandidateAuthDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		type fileInfo struct {
			path string
			mod  time.Time
		}
		var infos []fileInfo
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".info") {
				continue
			}
			full := filepath.Join(dir, e.Name())
			mod := time.Time{}
			if st, err := os.Stat(full); err == nil {
				mod = st.ModTime()
			}
			infos = append(infos, fileInfo{full, mod})
		}
		// 最近修改的优先（多账号时通常是最新登录的那个）
		sort.SliceStable(infos, func(i, j int) bool { return infos[i].mod.After(infos[j].mod) })

		for _, fi := range infos {
			cred, err := parseAuthFile(fi.path, cfg.Endpoint)
			if err != nil {
				brokenPath = fi.path
				continue
			}
			if err := validate(cred); err != nil {
				return nil, err
			}
			return cred, nil
		}
	}

	if brokenPath != "" {
		return nil, newError(KindCredentialBroken, "无法从 "+brokenPath+" 中读出访问令牌")
	}
	return nil, newError(KindCredentialMiss,
		"未找到 WorkBuddy 登录信息（已查找："+strings.Join(CandidateAuthDirs(), "；")+"）")
}

// validate 检查令牌存在且未过期
func validate(c *Credential) error {
	if strings.TrimSpace(c.AccessToken) == "" {
		return newError(KindCredentialBroken, "访问令牌为空")
	}
	if c.HasExpiry && time.Now().After(c.ExpiresAt) {
		return newError(KindTokenExpired, c.ExpiresAt.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

// parseAuthFile 解析 workbuddy-desktop.info
//
// 结构形如：
//
//	{ "auth": { "accessToken": "...", "expiresAt": 1792... },
//	  "account": { "uid": "...", "nickname": "..." } }
func parseAuthFile(path, endpoint string) (*Credential, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root struct {
		Auth    map[string]json.RawMessage `json:"auth"`
		Account map[string]json.RawMessage `json:"account"`
	}
	if err := json.Unmarshal(buf, &root); err != nil {
		return nil, err
	}
	if root.Auth == nil {
		return nil, newError(KindCredentialBroken, "缺少 auth 字段")
	}
	token := asString(root.Auth["accessToken"])
	if token == "" {
		return nil, newError(KindCredentialBroken, "auth.accessToken 为空")
	}

	cred := &Credential{
		AccessToken: token,
		UserID:      firstNonEmpty(asString(root.Account["uid"]), asString(root.Account["userId"])),
		Nickname:    firstNonEmpty(asString(root.Account["nickname"]), asString(root.Account["userName"])),
		Endpoint:    endpoint,
		Source:      path,
	}

	if ms, ok := epochFromRaw(root.Auth["expiresAt"]); ok {
		cred.ExpiresAt, cred.HasExpiry = ms, true
	}
	if !cred.HasExpiry {
		if exp, ok := jwtExpiry(token); ok {
			cred.ExpiresAt, cred.HasExpiry = exp, true
		}
	}
	return cred, nil
}

// epochFromRaw 兼容秒与毫秒两种时间戳
func epochFromRaw(raw json.RawMessage) (time.Time, bool) {
	v := asFloat(raw)
	if v <= 0 {
		return time.Time{}, false
	}
	if v > 1e11 { // 毫秒
		v /= 1000
	}
	return time.Unix(int64(v), 0), true
}

// jwtExpiry 本地解析 JWT 的 exp 字段（不做签名校验）
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		// 退回到标准 base64 再试一次
		if payload, err = base64.StdEncoding.DecodeString(parts[1]); err != nil {
			return time.Time{}, false
		}
	}
	var claims struct {
		Exp json.RawMessage `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, false
	}
	v := asFloat(claims.Exp)
	if v <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(v), 0), true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
