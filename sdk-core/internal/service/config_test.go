package service

import (
    "context"
    "encoding/json"
    "errors"
    "reflect"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type fakeReader struct {
    config model.ConfigResponse
    err error
    calls int
    appID string
    env string
}

func (f *fakeReader) LookupPublicConfig(_ context.Context, appID, env string) (model.ConfigResponse, error) {
    f.calls++
    f.appID, f.env = appID, env
    return f.config, f.err
}

func TestGetUsesFixedFieldsForEveryEnvironment(t *testing.T) {
    for _, env := range []string{"prod", "test", "review"} {
        key := "ts-123"
        reader := &fakeReader{config: model.ConfigResponse{
            SnKey: "app1001", ZoneKey: "group-" + env, TsKey: &key,
        }}
        got, err := NewConfigService(reader).Get(context.Background(), env, "app1001")
        if err != nil { t.Fatal(err) }
        if !reflect.DeepEqual(got, reader.config) {
            t.Fatalf("%s: got %+v, want %+v", env, got, reader.config)
        }
        if reader.calls != 1 || reader.env != env || reader.appID != "app1001" {
            t.Fatalf("%s: unexpected query args: %+v", env, reader)
        }
    }
}

func TestGetDoesNotQueryForInvalidInput(t *testing.T) {
    for _, tc := range []struct{ env, appID string }{
        {"production", "app"}, {"staging", "app"}, {"prod", ""}, {"prod", "   "},
    } {
        reader := &fakeReader{}
        _, err := NewConfigService(reader).Get(context.Background(), tc.env, tc.appID)
        if !errors.Is(err, model.ErrInvalidRequest) || reader.calls != 0 {
            t.Errorf("env=%q appID=%q err=%v calls=%d", tc.env, tc.appID, err, reader.calls)
        }
    }
}

func TestGetPropagatesLookupErrors(t *testing.T) {
    for _, want := range []error{model.ErrNotFound, model.ErrInvalidConfig, errors.New("db unavailable")} {
        reader := &fakeReader{err: want}
        _, err := NewConfigService(reader).Get(context.Background(), "prod", "app1001")
        if !errors.Is(err, want) { t.Fatalf("want %v, got %v", want, err) }
    }
}

func TestConfigResponseOptionalTSKey(t *testing.T) {
    config := model.ConfigResponse{SnKey: "app1001", ZoneKey: "group1001"}
    encoded, err := json.Marshal(config)
    if err != nil { t.Fatal(err) }
    if string(encoded) != `{"snKey":"app1001","zoneKey":"group1001"}` {
        t.Fatalf("unexpected response without tsKey: %s", encoded)
    }
    empty := ""
    config.TsKey = &empty
    encoded, err = json.Marshal(config)
    if err != nil { t.Fatal(err) }
    if string(encoded) != `{"snKey":"app1001","zoneKey":"group1001","tsKey":""}` {
        t.Fatalf("empty tsKey must be retained: %s", encoded)
    }
}
