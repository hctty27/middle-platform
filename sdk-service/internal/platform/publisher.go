package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hitall.com/middle-platform/shared/logging"
	"hitall.com/middle-platform/shared/signature"
)

// ErrPublishEndpointMissing 表示当前平台服务尚未配置接收地址。
var ErrPublishEndpointMissing = errors.New("平台接收地址未配置")

// ErrPublishRejected 表示请求平台失败或平台未确认业务成功。
var ErrPublishRejected = errors.New("平台拒绝请求或平台调用失败")

// Publisher 复用 HTTP 客户端并按服务代码选择平台上报地址。
type Publisher struct {
	client         *http.Client
	eventClient    *http.Client
	endpoints      map[string]string
	accessEnabled bool
	slowThreshold time.Duration
}

// PublisherOptions controls separate transport pools, without adding retries or
// changing the platform protocol. A zero connection cap means unlimited.
type PublisherOptions struct {
	IsolateConnections bool
	MaxIdlePerHost     int
	EventMaxConns      int
	AccessEnabled bool
	SlowThreshold time.Duration
}

// NewPublisherWithOptions keeps the existing endpoint validation and allows
// separate, reusable HTTP transports for user checks and outbound events.
func NewPublisherWithOptions(eventURL, subscribeURL, userCheckURL string, options PublisherOptions) (*Publisher, error) {
	if options.MaxIdlePerHost < 1 || options.MaxIdlePerHost > 256 ||
		options.EventMaxConns < 0 || options.EventMaxConns > 4096 ||
		(!options.IsolateConnections && options.EventMaxConns != 0) ||
		options.SlowThreshold <= 0 {
		return nil, errors.New("平台连接池或日志采样配置不合法")
	}
	p, err := NewPublisher(eventURL, subscribeURL, userCheckURL)
	if err != nil {
		return nil, err
	}
	makeClient := func(capacity int) *http.Client {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConnsPerHost = options.MaxIdlePerHost
		transport.MaxIdleConns = max(100, options.MaxIdlePerHost*2)
		transport.MaxConnsPerHost = capacity
		return &http.Client{
			Transport:     transport,
			Timeout:       5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	p.client = makeClient(0)
	p.eventClient = p.client
	if options.IsolateConnections {
		p.eventClient = makeClient(options.EventMaxConns)
	}
	p.accessEnabled = options.AccessEnabled
	p.slowThreshold = options.SlowThreshold
	return p, nil
}

// CloseIdleConnections releases owned transport pools on shutdown. The legacy
// constructor uses the shared default transport, which must not be closed here.
func (p *Publisher) CloseIdleConnections() {
	if p == nil || p.client == nil || p.client.Transport == nil {
		return
	}
	p.client.CloseIdleConnections()
	if p.eventClient != nil && p.eventClient != p.client {
		p.eventClient.CloseIdleConnections()
	}
}

// NewPublisher 校验平台接口地址的协议、路径和其他限制，禁止请求重定向。
func NewPublisher(eventURL, subscribeURL string, userCheckURL ...string) (*Publisher, error) {
	checkURL := ""
	if len(userCheckURL) > 0 {
		checkURL = userCheckURL[0]
	}
	endpoints := map[string]string{
		"event_report":      eventURL,
		"subscribe_message": subscribeURL,
		"user_check":        checkURL,
	}
	paths := map[string]string{
		"event_report":      "/report/event",
		"subscribe_message": "/game/subscribe/send-message",
		"user_check":        "/cp/user/check",
	}
	for code, raw := range endpoints {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
			u.Fragment != "" || u.Opaque != "" || u.Path != paths[code] ||
			strings.TrimSpace(raw) != raw {
			return nil, fmt.Errorf("平台 %s 接口地址不合法", code)
		}
	}
	return &Publisher{
		endpoints: endpoints,
		accessEnabled: true, // Keep legacy constructor behavior for callers without config.
		client: &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Send 使用渠道 AppSecret 为平台请求签名，并校验 HTTP 及平台业务响应。
// 平台签名仅包含密钥与指定 Header 值，业务 Body 不参与签名。
func (p *Publisher) Send(ctx context.Context, code, appID, secret string, body []byte) error {
	if code != "event_report" && code != "subscribe_message" {
		return ErrPublishEndpointMissing
	}
	_, err := p.send(ctx, code, appID, secret, body)
	return err
}

func (p *Publisher) CheckUser(ctx context.Context, appID, secret string, body []byte) (bool, error) {
	return p.send(ctx, "user_check", appID, secret, body)
}

func (p *Publisher) send(ctx context.Context, code, appID, secret string, body []byte) (success bool, publishErr error) {
	start := time.Now()
	status := 0
	var businessCode *int
	failure := ""
	path := map[string]string{
		"event_report": "/report/event", "subscribe_message": "/game/subscribe/send-message", "user_check": "/cp/user/check",
	}[code]
	logger := logging.FromContext(ctx)
	defer func() {
		elapsed := time.Since(start)
		// One accessEnabled switch controls normal inbound and outbound request logs.
		// Failed or slow requests are never suppressed.
		if publishErr == nil && status >= http.StatusOK && status < http.StatusMultipleChoices &&
			p != nil && !p.accessEnabled && elapsed < p.slowThreshold {
			return
		}
		attrs := []any{"service_code", code, "app_id", appID, "method", http.MethodPost,
			"path", path, "request_bytes", len(body), "http_status", status, "duration_ms", elapsed.Milliseconds()}
		if businessCode != nil {
			attrs = append(attrs, "business_code", *businessCode)
		}
		if publishErr != nil {
			attrs = append(attrs, "failure", failure)
			if failure == "nonce_generation" || failure == "request_creation" {
				logger.ErrorContext(ctx, "平台接口请求失败", attrs...)
			} else {
				logger.WarnContext(ctx, "平台接口请求失败", attrs...)
			}
		} else if p != nil && p.slowThreshold > 0 && elapsed >= p.slowThreshold {
			logger.WarnContext(ctx, "平台接口请求过慢", attrs...)
		} else {
			logger.InfoContext(ctx, "平台接口请求完成", attrs...)
		}
	}()
	failure = "endpoint_missing"
	if p == nil || p.endpoints[code] == "" {
		return false, ErrPublishEndpointMissing
	}
	failure = "credentials_missing"
	if appID == "" || secret == "" {
		return false, errors.New("平台签名密钥或 AppID 缺失")
	}
	failure = "nonce_generation"
	var bytesNonce [8]byte
	if _, err := rand.Read(bytesNonce[:]); err != nil {
		return false, fmt.Errorf("生成平台请求 nonce 失败: %w", err)
	}
	nonce := hex.EncodeToString(bytesNonce[:])
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	failure = "request_creation"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoints[code], bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("%w: 创建请求失败: %v", ErrPublishRejected, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-app-id", appID)
	req.Header.Set("x-timestamp", timestamp)
	req.Header.Set("x-nonce", nonce)
	// 平台开放服务仅签 Header 四值，不包含业务 Body。
	req.Header.Set("x-signature", signature.SignValues(secret, appID, timestamp, nonce))
	failure = "transport"
	client := p.client
	if code != "user_check" && p.eventClient != nil {
		client = p.eventClient
	}
	response, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrPublishRejected, err)
	}
	defer response.Body.Close()
	status = response.StatusCode
	failure = "http_or_response_read"
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8*1024+1))
	loggedBody := raw
	truncated := len(raw) > 8*1024
	if truncated {
		loggedBody = raw[:8*1024]
	}
	if logger.Enabled(ctx, slog.LevelDebug) {
		logger.DebugContext(ctx, "平台接口请求和响应", "service_code", code, "http_status", status,
			"request_body", string(body), "response_body", string(loggedBody),
			"response_truncated", truncated, "response_read_error", err != nil)
	}
	if err != nil || len(raw) > 8*1024 || response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w: 平台 HTTP %d 或响应读取失败", ErrPublishRejected, response.StatusCode)
	}
	var result struct {
		Code *int  `json:"code"`
		Data *bool `json:"data"`
	}
	failure = "invalid_response"
	if json.Unmarshal(raw, &result) != nil || result.Code == nil {
		return false, fmt.Errorf("%w: 平台响应不合法", ErrPublishRejected)
	}
	businessCode = result.Code
	failure = "business_rejected"
	if code == "user_check" && *result.Code == 1021003 && result.Data == nil {
		return false, nil
	}
	if *result.Code != 0 || result.Data == nil || !*result.Data {
		return false, fmt.Errorf("%w: 平台业务返回不成功", ErrPublishRejected)
	}
	return true, nil
}
