-- 目标数据库：config_center。部署新版本前备份，并在已有库手工执行一次。
-- 每个 config_subscriber 的密钥从仅用于 Webhook 改为双向同步签名密钥。\n-- 执行后需确认所有 signing_secret 至少 32 字节，并同步配置对应接入端的 subscriber.secret。
ALTER TABLE config_subscriber
    CHANGE COLUMN webhook_secret signing_secret VARCHAR(255) NOT NULL;
