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
                Config:json.RawMessage(`{"tsKey":"ts-123","enabled":true}`),
            },
            group:&model.BoundGroup{
                RegionCode:"r1",ServerGroupCode:"s-"+env,Config:json.RawMessage("{}"),
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
    for _,appConfig:=range []string{"{}", "null", "[]", "invalid"} {
        f:=&fakeReader{
            app:model.ChannelApp{Code:"a1",Config:json.RawMessage(appConfig)},
            group:&model.BoundGroup{ServerGroupCode:"g1",Config:json.RawMessage("{}")},
        }
        _,err:=serviceWithReader(t,f).Get(context.Background(),"prod","a1")
        if !errors.Is(err,model.ErrInvalidConfig) { t.Fatalf("config %q: %v",appConfig,err) }
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
