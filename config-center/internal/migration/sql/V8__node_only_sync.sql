-- V8：配置同步范围仅为 game、region、server_group 节点。
-- 渠道应用和绑定继续保留权威业务数据，不再纳入节点版本和回调。
-- 仅切换 epoch 触发新的三级节点快照，不修改 revision。
UPDATE config_revision SET epoch=UUID() WHERE id=1;
