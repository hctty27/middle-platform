// Package remote 实现与权威配置中心兼容的 HTTP 同步协议。
package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

// 限制权威端响应体，避免错误网关或异常配置耗尽接入进程内存。
const maxResponseBytes = 64 << 20

// Node 保留远程节点的 JSON 字段及日期格式。
type Node struct {
	ID        int64   `json:"id"`
	NodeType  string  `json:"nodeType"`
	EnvType   *string `json:"envType"`
	GameID    *int64  `json:"gameId"`
	RegionID  *int64  `json:"regionId"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Status    int     `json:"status"`
	Config    string  `json:"config"`
	Remark    *string `json:"remark"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// Revision 唯一标识权威数据集及其单调递增版本。
type Revision struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
}

// Event 只携带变更版本和节点 ID；节点最终状态需要重新获取。
type Event struct {
	Revision  int64  `json:"revision"`
	NodeID    int64  `json:"nodeId"`
	Operation string `json:"operation"`
}

// Snapshot 是服务端同一数据库版本的一致性快照。
type Snapshot struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
	Nodes    []Node `json:"nodes"`
}

// Client 对配置中心的所有同步读取请求执行 HMAC-SHA256 签名。
type Client struct {
	base   string
	secret string
	http   *http.Client
}

// New 创建带请求签名的配置中心 HTTP 客户端。
func New(base string, connectTimeout, requestTimeout time.Duration, secret string) *Client {
	return &Client{
		base:   base,
		secret: secret,
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				DialContext:     (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext,
				MaxIdleConns:    16,
				IdleConnTimeout: 60 * time.Second,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	return c.call(ctx, http.MethodGet, path, nil, target)
}

// call 统一签署请求，并处理 HTTP 状态、响应大小和严格 JSON 解码。
func (c *Client) call(ctx context.Context, method, path string, body []byte, target any) error {
	if len([]byte(c.secret)) < 32 {
		return fmt.Errorf("配置同步 HMAC-SHA256 密钥至少需要 32 字节")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("生成配置同步 nonce 失败: %w", err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := hex.EncodeToString(random[:])
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return fmt.Errorf("配置同步查询参数不合法: %w", err)
	}
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", signature.SignHMACRequest(
		c.secret, req.Method, req.URL.EscapedPath(), query.Encode(), timestamp, nonce, body,
	))

	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求配置中心失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("配置中心请求 %s 失败，HTTP %d: %s", path, response.StatusCode, msg)
	}
	dec := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("接口 %s 响应格式错误: %w", path, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("接口 %s 响应包含多余内容", path)
	}
	return nil
}

// Head 查询当前 epoch 和 revision。
func (c *Client) Head(ctx context.Context) (Revision, error) {
	var out Revision
	err := c.get(ctx, "/api/config/sync/revision", &out)
	return out, err
}

// Changes 按 revision 递增获取增量日志，最多每批 200 条。
func (c *Client) Changes(ctx context.Context, after int64) ([]Event, error) {
	var out []Event
	q := url.Values{"afterRevision": {strconv.FormatInt(after, 10)}, "limit": {"200"}}
	err := c.get(ctx, "/api/config/sync/changes?"+q.Encode(), &out)
	return out, err
}

// Batch 获取指定 ID 的最新节点，已删除的 ID 不会出现在结果里。
func (c *Client) Batch(ctx context.Context, ids []int64) ([]Node, error) {
	body, err := json.Marshal(struct {
		IDs []int64 `json:"ids"`
	}{ids})
	if err != nil {
		return nil, err
	}
	var out []Node
	err = c.call(ctx, http.MethodPost, "/api/config/nodes/batch-get", body, &out)
	return out, err
}

// Full 返回与 epoch、revision 对齐的一致性全量快照。
func (c *Client) Full(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	err := c.get(ctx, "/api/config/sync/snapshot", &out)
	return out, err
}
