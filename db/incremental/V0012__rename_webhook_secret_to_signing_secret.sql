-- 目标数据库：config_center。部署新版本前备份，并在已有库手工执行一次。
-- 每个 config_subscriber 的密钥从仅用于 Webhook 改为双向同步签名密钥。
ALTER TABLE config_subscriber
    CHANGE COLUMN webhook_secret signing_secret VARCHAR(255) NOT NULL;
