# 配置中心数据库整理（V9）

## 正式表（8 张）

| 分类 | 表名 | 用途 |
|---|---|---|
| 业务 | `config_node` | 游戏、大区、区服组 |
| 业务 | `config_channel_app` | 游戏级渠道应用 |
| 业务 | `config_channel_app_binding` | 区服组绑定及覆盖配置 |
| 同步 | `config_revision` | epoch 和 revision |
| 同步 | `config_node_change_log` | 三级节点增量日志 |
| Webhook | `config_subscriber` | 接入方及回调配置 |
| Webhook | `config_subscriber_delivery_state` | ACK、租约与失败重试 |
| 元数据 | `config_schema_migrations` | 数据库结构版本 |

V9 只调整两张表名并增加中文表注释，沿用原有数据、索引、外键和业务字段。
数据库升级前请备份并停止所有旧版 `config-center` 实例；升级成功后只能使用 V9 对应的服务版本。
`config-center-access` 的本地数据库不需要变更，它只维护三级节点副本与同步游标。

## 升级检查

```sql
SELECT DATABASE();
SELECT version, dirty FROM config_schema_migrations WHERE id = 1;
SELECT table_name, table_comment
FROM information_schema.tables
WHERE table_schema = DATABASE()
ORDER BY table_name;
```

正常应为 `version=9` 且 `dirty=0`。两张新表存在且旧名称不再存在。
旧表名只留在 V1–V8 的不可变历史迁移文件、升级兼容校验和历史回归测试中。

## V4/V6 归档清理（必须人工执行）

`config_node_v4_legacy_archive` 和 `config_node_v6_channel_app_archive` 是不可从现有业务表完整重建的历史数据。
V9 不自动删除这些表；如需让数据库界面只保留 8 张表，请先完整备份、验证可恢复，再手动清理。

1. 核对实际连接的是权威配置中心库，不是 `config_access` 接入副本库；确认 V9 迁移完成。
2. 记录归档表行数，并将存在的归档表分别导出到库外持久存储。以下示例适用于 PowerShell 或 CMD，使用 MySQL 客户端的 `--result-file` 避免重定向编码问题。
3. 将导出文件恢复到临时库核验数据行数，长期保存备份。
4. 确认不再需要历史审计后才执行下面的手动删除语句。

```powershell
mysqldump --host=127.0.0.1 --port=3306 --user=root --password --single-transaction --set-gtid-purged=OFF --default-character-set=utf8mb4 --result-file=config-node-v6-archive.sql config_center config_node_v6_channel_app_archive
```

```sql
SELECT COUNT(*) FROM config_node_v6_channel_app_archive;
-- V4 归档存在时也要单独导出、验证后再考虑删除。
-- 只在备份恢复验证通过后手工执行：
DROP TABLE config_node_v6_channel_app_archive;
-- DROP TABLE config_node_v4_legacy_archive;
```

没有对应归档表时不需要创建空表。不得修改 `config_schema_migrations.version` 或 `dirty` 来绕过校验。
如果 V9 的 DDL 部分成功后中断，必须先人工核对真实表结构与迁移状态，**不要直接重复执行 RENAME TABLE 或重置 dirty**。
