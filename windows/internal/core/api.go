package core

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 接口路径（与 macOS 版一致）
const (
	pathSummary = "/billing/meter/get-user-resource-summary"
	pathCheckin = "/billing/meter/checkin-activity-status"
)

// UserAgent 请求标识
const UserAgent = "WorkBuddyStatus/1.2 (Windows)"

// Client 积分接口客户端
type Client struct {
	Endpoint string
	HTTP     *http.Client
}

// NewClient 构造客户端；timeout <= 0 时使用 20 秒
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		Endpoint: DefaultEndpoint,
		HTTP: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        4,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// FetchSummary 拉取积分汇总
func (c *Client) FetchSummary(cred *Credential) (*CreditSummary, error) {
	var summary *CreditSummary
	err := c.post(cred, pathSummary, func(body []byte) error {
		s, err := ParseSummary(body)
		if err != nil {
			return err
		}
		summary = s
		return nil
	})
	return summary, err
}

// FetchCheckin 拉取签到状态
func (c *Client) FetchCheckin(cred *Credential) (*CheckinStatus, error) {
	var status *CheckinStatus
	err := c.post(cred, pathCheckin, func(body []byte) error {
		s, err := ParseCheckin(body)
		if err != nil {
			return err
		}
		status = s
		return nil
	})
	return status, err
}

// post 发起一次 POST 请求并把响应正文交给 parse 处理
func (c *Client) post(cred *Credential, path string, parse func([]byte) error) error {
	base := strings.TrimSpace(c.Endpoint)
	if base == "" {
		base = DefaultEndpoint
	}
	url := strings.TrimRight(base, "/") + path

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return newError(KindEndpoint, "接口地址无效："+url)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "zh")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	if cred.UserID != "" {
		req.Header.Set("X-User-Id", cred.UserID)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return newError(KindNetwork, "网络请求失败："+err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return newError(KindNetwork, "读取响应失败："+err.Error())
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return newError(KindUnauthorized, "HTTP "+strconv.Itoa(resp.StatusCode))
	}
	if resp.StatusCode != http.StatusOK {
		return newError(KindHTTP, fmt.Sprintf("HTTP %d：%s", resp.StatusCode, truncate(string(body), 200)))
	}
	return parse(body)
}

// ProbeEndpoint 只做连通性探测，用于诊断输出
func (c *Client) ProbeEndpoint(cred *Credential) (int, time.Duration, error) {
	base := strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	if base == "" {
		base = DefaultEndpoint
	}
	url := base + pathSummary

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return 0, 0, newError(KindEndpoint, "接口地址无效："+url)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	if cred.UserID != "" {
		req.Header.Set("X-User-Id", cred.UserID)
	}

	start := time.Now()
	resp, err := c.HTTP.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return 0, elapsed, newError(KindNetwork, err.Error())
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode, elapsed, nil
}
