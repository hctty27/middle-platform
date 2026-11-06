package api

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strconv"
    "strings"
    "github.com/hctty27/middle-platform/sdk-core/internal/configschema"
)

type adminStore struct{ db *sql.DB }

func registerAdminRoutes(mux *http.ServeMux, db *sql.DB) {
    h := &adminStore{db: db}
    mux.HandleFunc("GET /api/core/admin/config-schema/{nodeType}", getConfigSchema)
    mux.HandleFunc("GET /api/core/admin/node-configs/{id}", h.getNodeConfig)
    mux.HandleFunc("PUT /api/core/admin/node-configs/{id}", h.putNodeConfig)
    registerChannelAdminRoutes(mux, h)
    registerServiceAdminRoutes(mux, h)
}

func adminID(r *http.Request) (int64, bool) {
    id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
    return id, err == nil && id > 0
}

func adminError(w http.ResponseWriter, status int, message string) {
    writeJSON(w, status, map[string]string{"error": message})
}

func adminDecode(w http.ResponseWriter, r *http.Request, target any) bool {
    defer r.Body.Close()
    d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
    if err := d.Decode(target); err != nil {
        adminError(w, http.StatusBadRequest, "请求 JSON 不合法")
        return false
    }
    if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
        adminError(w, http.StatusBadRequest, "请求只能包含一个 JSON 对象")
        return false
    }
    return true
}

func jsonObject(raw string) (map[string]json.RawMessage, bool) {
    if len(raw) > 64<<10 { return nil, false }
    d := json.NewDecoder(strings.NewReader(raw))
    var obj map[string]json.RawMessage
    if err := d.Decode(&obj); err != nil || obj == nil { return nil, false }
    if err := d.Decode(new(any)); !errors.Is(err, io.EOF) { return nil, false }
    return obj, true
}

// 配置读写沿用现有接口，字段规则与对外 Schema 保持一致。
func validSDKNodeConfig(kind, raw string) bool {
    if _, ok := jsonObject(raw); !ok { return false }
    return configschema.Validate(kind, raw) == nil
}

func (h *adminStore) getNodeConfig(w http.ResponseWriter, r *http.Request) {
    id, ok := adminID(r)
    if !ok { adminError(w, 400, "节点 ID 不合法"); return }
    var kind string
    var raw []byte
    err := h.db.QueryRowContext(r.Context(), `SELECT n.node_type,s.config FROM config_node n
      LEFT JOIN sdk_node_config s ON s.node_id=n.id WHERE n.id=?`, id).Scan(&kind, &raw)
    if errors.Is(err, sql.ErrNoRows) { adminError(w, 404, "节点不存在"); return }
    if err != nil { adminError(w, 500, "查询 SDK 节点配置失败"); return }
    if len(raw)==0 { raw=[]byte("{}") }
    writeJSON(w, 200, map[string]any{"nodeId": id, "nodeType": kind, "config": string(raw)})
}

func (h *adminStore) putNodeConfig(w http.ResponseWriter, r *http.Request) {
    id, ok := adminID(r)
    if !ok { adminError(w, 400, "节点 ID 不合法"); return }
    var input struct { Config string `json:"config"` }
    if !adminDecode(w, r, &input) { return }
    ctx := r.Context()
    tx, err := h.db.BeginTx(ctx, nil)
    if err != nil { adminError(w, 500, "开启数据库事务失败"); return }
    defer tx.Rollback()
    // 与基础节点删除共用版本行锁，但不递增节点同步 revision。
    var revision int64
    if err = tx.QueryRowContext(ctx, "SELECT revision FROM config_revision WHERE id=1 FOR UPDATE").Scan(&revision); err != nil {
        adminError(w, 500, "获取配置写入锁失败"); return
    }
    var kind string
    err = tx.QueryRowContext(ctx, "SELECT node_type FROM config_node WHERE id=? FOR UPDATE", id).Scan(&kind)
    if errors.Is(err, sql.ErrNoRows) { adminError(w, 404, "节点不存在"); return }
    if err != nil { adminError(w, 500, "查询节点失败"); return }
    if !validSDKNodeConfig(kind, input.Config) {
        adminError(w, 400, "SDK 配置必须是合法 JSON 对象，且字段必须与节点类型匹配")
        return
    }
    _, err = tx.ExecContext(ctx, `INSERT INTO sdk_node_config(node_id,config)
        VALUES(?,?) ON DUPLICATE KEY UPDATE config=VALUES(config),updated_at=UTC_TIMESTAMP(3)`, id, input.Config)
    if err != nil { adminError(w, 500, "保存 SDK 节点配置失败"); return }
    if err = tx.Commit(); err != nil { adminError(w, 500, "提交 SDK 配置失败"); return }
    writeJSON(w, 200, map[string]any{"nodeId": id, "nodeType": kind, "config": input.Config})
}

// 常用的可空字段工具。
func nullableID(p *int64) any { if p == nil { return nil }; return *p }
func nullableText(s *string) any { if s == nil { return nil }; return *s }
func copyRaw(b []byte) json.RawMessage { return append(json.RawMessage(nil), bytes.TrimSpace(b)...) }
func txLockRevision(ctx context.Context, tx *sql.Tx) error {
    var revision int64
    return tx.QueryRowContext(ctx, "SELECT revision FROM config_revision WHERE id=1 FOR UPDATE").Scan(&revision)
}
