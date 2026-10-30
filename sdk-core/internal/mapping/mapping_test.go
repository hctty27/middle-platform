package mapping

import (
    "encoding/json"
    "reflect"
    "testing"
)

func TestDefaultMappingProducesExpectedResponse(t *testing.T) {
    resolver,err:=LoadDefault()
    if err!=nil { t.Fatal(err) }
    data,err:=resolver.Resolve(RouteCoreConfig,map[string]any{
        "channel_app":map[string]any{
            "code":"app1001",
            "config":map[string]any{"tsKey":"abc123"},
        },
        "server_group":map[string]any{"code":"s1001"},
    })
    if err!=nil { t.Fatal(err) }
    expected:=map[string]any{"snKey":"app1001","zoneKey":"s1001","tsKey":"abc123"}
    if !reflect.DeepEqual(data,expected) { t.Fatalf("got %+v, want %+v",data,expected) }
}

func TestRouteSpecificMappingAndNestedJSON(t *testing.T) {
    resolver,err:=Load([]byte(`{
        "GET /api/core/{env}/{appid}": {
            "app": "channel_app.code"
        },
        "POST /api/core/metadata/{appid}": {
            "name": "channel_app.name",
            "key": "channel_app.config.login.sdk.key",
            "regions": "server_group.config.regions",
            "enabled": "channel_app.config.enabled",
            "count": "channel_app.config.count",
            "environment": "env"
        }
    }`))
    if err!=nil { t.Fatal(err) }
    raw:=[]byte(`{"login":{"sdk":{"key":"example"}},"enabled":true,"count":9007199254740993}`)
    config,err:=DecodeObject(raw)
    if err!=nil { t.Fatal(err) }
    source:=map[string]any{
        "env":"test",
        "channel_app":map[string]any{"code":"a1","name":"Demo","config":config},
        "server_group":map[string]any{"config":map[string]any{"regions":[]any{"r1","r2"}}},
    }
    core,err:=resolver.Resolve(RouteCoreConfig,source)
    if err!=nil { t.Fatal(err) }
    if !reflect.DeepEqual(core,map[string]any{"app":"a1"}) { t.Fatal(core) }
    meta,err:=resolver.Resolve("POST /api/core/metadata/{appid}",source)
    if err!=nil { t.Fatal(err) }
    if len(meta)!=6 || meta["name"]!="Demo" || meta["environment"]!="test" || meta["enabled"]!=true {
        t.Fatalf("incorrect meta: %+v",meta)
    }
    if meta["count"]!=json.Number("9007199254740993") { t.Fatalf("precision lost: %#v",meta["count"]) }
    if !reflect.DeepEqual(meta["regions"],[]any{"r1","r2"}) { t.Fatal(meta["regions"]) }
}

func TestRejectsInvalidMapping(t *testing.T) {
    cases:=[]string{
        "{}", "[]", `{"GET /a":{}}`,
        `{"GET /a":{"a":"sql.select"}}`,
        `{"GET /a":{"a":"channel_app.config"}}`,
        `{"GET /a":{"a":"channel_app.config..key"}}`,
        `{"GET /a":{"a":"channel_app.password"}}`,
        `{"GET /a":{"a":""}}`,
        `{"bad":{"a":"env"}}`,
        `{"GET /a":{"a":"env"}} trailing`,
    }
    for _,input:=range cases {
        if _,err:=Load([]byte(input));err==nil { t.Errorf("accepted invalid mapping %q",input) }
    }
}

func TestMissingFieldsAreOmittedInsteadOfFailing(t *testing.T) {
    resolver, err := LoadDefault()
    if err != nil { t.Fatal(err) }
    if err := resolver.Require("GET /no-such-route"); err == nil {
        t.Fatal("missing route accepted")
    }

    for _, tc := range []struct {
        name string
        source map[string]any
        want map[string]any
    }{
        {
            name: "missing config key",
            source: map[string]any{
                "channel_app": map[string]any{
                    "code": "app1001",
                    "config": map[string]any{},
                },
                "server_group": map[string]any{"code": "group1"},
            },
            want: map[string]any{"snKey": "app1001", "zoneKey": "group1"},
        },
        {
            name: "null config value",
            source: map[string]any{
                "channel_app": map[string]any{
                    "code": "app1001",
                    "config": map[string]any{"tsKey": nil},
                },
                "server_group": map[string]any{"code": "group1"},
            },
            want: map[string]any{"snKey": "app1001", "zoneKey": "group1"},
        },
        {
            name: "missing parent object",
            source: map[string]any{
                "channel_app": map[string]any{"code": "app1001"},
                "server_group": map[string]any{"code": "group1"},
            },
            want: map[string]any{"snKey": "app1001", "zoneKey": "group1"},
        },
        {
            name: "all fields absent",
            source: map[string]any{},
            want: map[string]any{},
        },
    } {
        t.Run(tc.name, func(t *testing.T) {
            got, err := resolver.Resolve(RouteCoreConfig, tc.source)
            if err != nil { t.Fatal(err) }
            if !reflect.DeepEqual(got, tc.want) {
                t.Fatalf("got %+v, want %+v", got, tc.want)
            }
        })
    }
}

func TestResolverPreservesConfiguredZeroValues(t *testing.T) {
    resolver, err := Load([]byte(`{
        "GET /core": {
            "falseValue": "channel_app.config.falseValue",
            "zeroValue": "channel_app.config.zeroValue",
            "emptyString": "channel_app.config.emptyString",
            "nullValue": "channel_app.config.nullValue",
            "missingNested": "channel_app.config.login.token"
        }
    }`))
    if err != nil { t.Fatal(err) }
    got, err := resolver.Resolve("GET /core", map[string]any{
        "channel_app": map[string]any{"config": map[string]any{
            "falseValue": false,
            "zeroValue": json.Number("0"),
            "emptyString": "",
            "nullValue": nil,
            "login": "not an object",
        }},
    })
    if err != nil { t.Fatal(err) }
    want := map[string]any{
        "falseValue": false,
        "zeroValue": json.Number("0"),
        "emptyString": "",
    }
    if !reflect.DeepEqual(got, want) { t.Fatalf("got %+v, want %+v", got, want) }
}

func TestDecodeObject(t *testing.T) {
    for _,raw:=range []string{"null","[]","1","bad",`{"x":1} {"x":2}`} {
        if _,err:=DecodeObject([]byte(raw));err==nil { t.Fatalf("accepted %q",raw) }
    }
    if _,err:=DecodeObject([]byte(`{"x":null}`));err!=nil { t.Fatal(err) }
}
