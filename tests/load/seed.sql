-- Synthetic fixture for a new, disposable config_center database only.
INSERT INTO config_node
(id,game_id,region_id,node_type,env_type,market_type,code,name,status,config,created_at,updated_at)
VALUES
(1,NULL,NULL,'game',NULL,'domestic','load-game','Load Game',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP()),
(2,1,NULL,'region','test',NULL,'load-region','Load Region',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP()),
(3,1,2,'server_group',NULL,NULL,'load-group','Load Group',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP());

INSERT INTO sdk_node_config(node_id,config) VALUES
(1,JSON_OBJECT('gameSecret','runner-load-game-only')),
(2,JSON_OBJECT('payNotifyUrl','http://127.0.0.1:8290/game/notify'));

INSERT INTO sdk_channel_app
(id,game_id,code,name,status,config,test_server_group_id,created_at,updated_at)
VALUES(1,1,'load-app','Load App',1,JSON_OBJECT('appSecret','runner-load-platform-only','tsKey','load-ts'),3,UTC_TIMESTAMP(),UTC_TIMESTAMP());

-- Both signed outbound services must actually invoke the local publishing mock.
-- event_report is disabled unless the exact channel/environment binding exists.
INSERT INTO sdk_service_channel(service_id,channel_app_id,env,enabled,config)
SELECT s.id,a.id,'test',1,JSON_OBJECT()
FROM sdk_service s CROSS JOIN sdk_channel_app a
WHERE s.code IN ('event_report','user_check') AND s.enabled=1 AND a.code='load-app';

INSERT INTO config_subscriber(code,callback_url,signing_secret,enabled,created_at,updated_at)
VALUES('load-subscriber','http://127.0.0.1:8290/config/webhook','runner-load-subscriber-only-0123456789',1,UTC_TIMESTAMP(),UTC_TIMESTAMP());
