# 数据库 V11 基线与历史归档清理

## 权威库正式表

| 表 | 用途 |
|---|---|
| config_node | 游戏、大区、区服组 |
| config_channel_app | 渠道应用和三环境绑定 |
| config_revision | 全局 epoch、revision |
| config_node_change_log | 节点增量日志 |
| config_subscriber | 订阅方和回调信息 |
| config_subscriber_delivery_state | 投递游标、租约与重试 |
| config_schema_migrations | 当前数据库结构版本 |

新增的空库直接执行 `cmd/migrate` 初始化 `internal/migration/sql/schema.sql`（V11 基线）；不需要回放历史 V1～V11 SQL。迁移脚本通过 `go:embed` 内置于发布二进制中。

现有 V11 数据库不会重复初始化，`config_schema_migrations` 表必须保留。低于 V11 或没有历史表的非空数据库不能直接使用新的迁移程序，须先备份并使用基线重构前的 Git 历史代码升级到 V11。

```sql
SELECT DATABASE();
SELECT version, dirty FROM config_schema_migrations WHERE id = 1;
SELECT table_name, table_comment
FROM information_schema.tables
WHERE table_schema = DATABASE()
ORDER BY table_name;
```

预期 version=11、dirty=0，并且没有 `config_channel_app_binding` 等旧版运行态表。

## 历史归档表

已完成 V11 升级的历史库可能仍有 `config_node_v4_legacy_archive`、`config_node_v6_channel_app_archive`；它们不属于新库基线，但保留的数据可能无法从当前业务库恢复。

绝不能由迁移器自动删除。若明确不需要历史审计，应先单独备份、验证恢复和行数，再人工删除归档表。缺少归档表不影响 V11 运行态校验。

```powershell
mysqldump --host=127.0.0.1 --port=3306 --user=root --password --single-transaction --set-gtid-purged=OFF --default-character-set=utf8mb4 --result-file=config-node-v6-archive.sql config_center config_node_v6_channel_app_archive
```

数据库 `schema.sql` 只用于空库初始化，不包含业务数据。接入副本使用自己独立的 `internal/store/sql/schema.sql`，不能混用。MySQL DDL 非完整事务；dirty=1 时必须人工恢复，不能通过修改版本字段或删除迁移记录来“修复”。
