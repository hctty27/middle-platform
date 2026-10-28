-- Webhook 投递游标不仅要比较 revision，也必须比较数据集 epoch。
-- 旧投递记录的 acknowledged_epoch 为 NULL，迁移后将自动再次通知接入方。
ALTER TABLE config_delivery_state
    ADD COLUMN acknowledged_epoch CHAR(36) NULL;
