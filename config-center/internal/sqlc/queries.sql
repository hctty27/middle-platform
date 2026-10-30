-- name: GetRevision :one
SELECT epoch, revision FROM config_revision WHERE id = 1;

-- name: ListChanges :many
SELECT revision, node_id, operation, created_at
FROM config_change_log
WHERE revision > ?
ORDER BY revision
LIMIT ?;

-- name: GetNode :one
SELECT id, node_type, game_id, region_id, server_group_id, code, name,
       status, config, remark, created_at, updated_at
FROM config_node WHERE id = ?;

-- name: ListNodes :many
SELECT id, node_type, game_id, region_id, server_group_id, code, name,
       status, config, remark, created_at, updated_at
FROM config_node ORDER BY id;

-- name: LockRevision :one
SELECT revision FROM config_revision WHERE id = 1 FOR UPDATE;

-- name: IncrementRevision :exec
UPDATE config_revision SET revision = revision + 1 WHERE id = 1;
