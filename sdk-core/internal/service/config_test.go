package service

import (
    "context"
    "encoding/json"
    "errors"
    "reflect"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/mapping"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type fakeReader struct {
    app model.ChannelApp
    group *model.BoundGroup
    err error
    calls int
    env string
}

func (f *fakeReader) Lookup(_ context.Context, _, env string) (model.ChannelApp,*model.BoundGroup,error) {
    f.calls++
    f.env=env
    return f.app,f.group,f.err
}

func serviceWithReader(t *testing.T,reader Reader) *ConfigService {
    t.Helper()
    resolver,err:=mapping.LoadDefault()
    if err!=nil { t.Fatal(err) }
    return NewConfigService(reader,resolver)
}

func TestGetUsesGlobalMappingRegardlessOfEnvironment(t *testing.T) {
    for _,env:=range []string{"prod","test","review"} {
        f:=&fakeReader{
            app:model.ChannelApp{
                Code:"huawei_10001",Name:"Huawei",GameCode:"game_1",
                GameConfig: json.RawMessage("{}"), Config:json.RawMessage(`{"tsKey":"ts-123","enabled":true}`),
            },
            group:&model.BoundGroup{
                RegionCode:"r1",RegionConfig:json.RawMessage("{}"),ServerGroupCode:"s-"+env,Config:json.RawMessage("{}"),
            },
        }
        result,err:=serviceWithReader(t,f).Get(context.Background(),env,"huawei_10001")
        if err!=nil { t.Fatal(err) }
        expected:=map[string]any{"snKey":"huawei_10001","zoneKey":"s-"+env,"tsKey":"ts-123"}
        if !reflect.DeepEqual(result,expected) { t.Fatalf("%s: got %+v",env,result) }
        if f.env!=env { t.Fatalf("lookup env: %s",f.env) }
    }
}

func TestGetRejectsUnboundEnvironment(t *testing.T) {
    f:=&fakeReader{app:model.ChannelApp{Config:json.RawMessage(`{"tsKey":"x"}`)}}
    _,err:=serviceWithReader(t,f).Get(context.Background(),"review","app")
    if !errors.Is(err,model.ErrNotFound) { t.Fatalf("got %v",err) }
}

func TestGetRejectsInvalidOrMissingConfig(t *testing.T) {
    for _,appConfig:=range []string{"null", "[]", "invalid"} {
        f:=&fakeReader{
            app:model.ChannelApp{Code:"a1",GameConfig:json.RawMessage("{}"),Config:json.RawMessage(appConfig)},
            group:&model.BoundGroup{ServerGroupCode:"g1",RegionConfig:json.RawMessage("{}"),Config:json.RawMessage("{}")},
        }
        _,err:=serviceWithReader(t,f).Get(context.Background(),"prod","a1")
        if !errors.Is(err,model.ErrInvalidConfig) { t.Fatalf("config %q: %v",appConfig,err) }
    }
}

func TestGetOmitsUnsetOptionalMappingFields(t *testing.T) {
    for _, config := range []string{
        "{}",
        `{"tsKey":null}`,
    } {
        f := &fakeReader{
            app: model.ChannelApp{Code: "app1001", GameConfig: json.RawMessage("{}"), Config: json.RawMessage(config)},
            group: &model.BoundGroup{
                ServerGroupCode: "group1001",
                RegionConfig: json.RawMessage("{}"),
                Config: json.RawMessage("{}"),
            },
        }
        got, err := serviceWithReader(t, f).Get(context.Background(), "prod", "app1001")
        if err != nil { t.Fatalf("config=%s: %v", config, err) }
        want := map[string]any{"snKey":"app1001","zoneKey":"group1001"}
        if !reflect.DeepEqual(got, want) {
            t.Fatalf("config=%s: got %+v, want %+v", config, got, want)
        }
    }
}

func TestGetRejectsInvalidInputBeforeDatabase(t *testing.T) {
    for _,tc:=range []struct{env,appID string}{
        {"production","app"},{"staging","app"},{"prod",""},{"prod","   "},
    } {
        f:=&fakeReader{}
        _,err:=serviceWithReader(t,f).Get(context.Background(),tc.env,tc.appID)
        if !errors.Is(err,model.ErrInvalidRequest) || f.calls!=0 {
            t.Errorf("env=%q appID=%q err=%v calls=%d",tc.env,tc.appID,err,f.calls)
        }
    }
}

func TestGetPropagatesLookupError(t *testing.T) {
    f:=&fakeReader{err:model.ErrAmbiguous}
    _,err:=serviceWithReader(t,f).Get(context.Background(),"prod","app")
    if !errors.Is(err,model.ErrAmbiguous) { t.Fatal(err) }
}

func TestGetRegionConfigMappingWithoutCodeChanges(t *testing.T) {
    resolver, err := mapping.Load([]byte(`{
        "GET /api/core/{env}/{appid}": {
            "snKey": "channel_app.code",
            "regionKey": "region.config.regionKey",
            "loginUrl": "region.config.login.url",
            "unconfigured": "region.config.login.optional"
        }
    }`))
    if err != nil { t.Fatal(err) }

    f := &fakeReader{
        app: model.ChannelApp{Code: "app1001", GameConfig: json.RawMessage("{}"), Config: json.RawMessage("{}")},
        group: &model.BoundGroup{
            RegionCode: "region1001",
            RegionConfig: json.RawMessage(`{"regionKey":"r1001","login":{"url":"https://example.com"}}`),
            ServerGroupCode: "server1001",
            Config: json.RawMessage("{}"),
        },
    }
    got, err := NewConfigService(f, resolver).Get(context.Background(), "prod", "app1001")
    if err != nil { t.Fatal(err) }
    want := map[string]any{
        "snKey": "app1001",
        "regionKey": "r1001",
        "loginUrl": "https://example.com",
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %+v, want %+v", got, want)
    }
}

func TestGetGameConfigNestedMapping(t *testing.T) {
    resolver, err := mapping.Load([]byte(`{
        "GET /api/core/{env}/{appid}": {
            "gameConfig": "game.config",
            "gameTitle": "game.config.title",
            "regionConfig": "region.config",
            "missing": "game.config.doesNotExist"
        }
    }`))
    if err != nil { t.Fatal(err) }

    f := &fakeReader{
        app: model.ChannelApp{
            Code: "app1001",
            GameCode: "game1001",
            GameConfig: json.RawMessage(`{"title":"Game","online":true}`),
            Config: json.RawMessage("{}"),
        },
        group: &model.BoundGroup{
            RegionConfig: json.RawMessage(`{"regionId":"r1"}`),
            Config: json.RawMessage("{}"),
        },
    }
    got, err := NewConfigService(f,resolver).Get(context.Background(),"prod","app1001")
    if err != nil { t.Fatal(err) }
    want := map[string]any{
        "gameConfig": map[string]any{"title":"Game","online":true},
        "gameTitle": "Game",
        "regionConfig": map[string]any{"regionId":"r1"},
    }
    if !reflect.DeepEqual(got,want) { t.Fatalf("got %+v; want %+v",got,want) }
}

func TestGetRejectsMalformedGameConfig(t *testing.T) {
    f := &fakeReader{
        app: model.ChannelApp{
            Code: "app1001",
            GameConfig: json.RawMessage("invalid"),
            Config: json.RawMessage("{}"),
        },
        group: &model.BoundGroup{
            RegionConfig: json.RawMessage("{}"),
            Config: json.RawMessage("{}"),
        },
    }
    _,err := serviceWithReader(t,f).Get(context.Background(),"prod","app1001")
    if !errors.Is(err,model.ErrInvalidConfig) { t.Fatalf("expected invalid game JSON: %v",err) }
}
