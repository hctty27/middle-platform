"""Generate only deterministic synthetic SQL for disposable runner fixtures."""
import os

GAMES = 16
APPS = 256


def generate():
    nodes = []
    secrets = []
    apps = []
    for g in range(GAMES):
        game, region, group = 100 + 3*g, 101 + 3*g, 102 + 3*g
        nodes.extend([
            f"({game},NULL,NULL,'game',NULL,'domestic','load-game-{g}','Fixture Game {g}',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP())",
            f"({region},{game},NULL,'region','test',NULL,'load-region-{g}','Fixture Region {g}',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP())",
            f"({group},{game},{region},'server_group',NULL,NULL,'load-group-{g}','Fixture Group {g}',1,JSON_OBJECT(),UTC_TIMESTAMP(),UTC_TIMESTAMP())",
        ])
        secrets.append(f"({game},JSON_OBJECT('gameSecret','runner-load-game-only-{g}'))")
    for i in range(APPS):
        g = i % GAMES
        apps.append(f"({1000+i},{100+3*g},'load-app-{i}','Fixture App {i}',1,JSON_OBJECT('appSecret','runner-load-platform-only-{i}','tsKey','load-ts-{i}'),{102+3*g},UTC_TIMESTAMP(),UTC_TIMESTAMP())")
    return "\n".join([
        "START TRANSACTION;",
        "INSERT INTO config_node (id,game_id,region_id,node_type,env_type,market_type,code,name,status,config,created_at,updated_at) VALUES " + ",\n".join(nodes) + ";",
        "INSERT INTO sdk_node_config (node_id,config) VALUES " + ",\n".join(secrets) + ";",
        "INSERT INTO sdk_channel_app (id,game_id,code,name,status,config,test_server_group_id,created_at,updated_at) VALUES " + ",\n".join(apps) + ";",
        "INSERT INTO sdk_service_channel(service_id,channel_app_id,env,enabled,config) SELECT s.id,a.id,'test',1,JSON_OBJECT() FROM sdk_service s CROSS JOIN sdk_channel_app a WHERE s.code IN ('event_report','user_check') AND s.enabled=1 AND a.id BETWEEN 1000 AND 1255;",
        "COMMIT;",
    ])


if __name__ == "__main__":
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
        raise SystemExit("Disposable GitHub-hosted runner required")
    print(generate())
