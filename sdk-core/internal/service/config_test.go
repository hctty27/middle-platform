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
	app    model.ChannelApp
	groups []model.BoundGroup
	err    error
	calls  int
}

func (f *fakeReader) Lookup(_ context.Context, _, _ string) (model.ChannelApp, []model.BoundGroup, error) {
	f.calls++
	return f.app, f.groups, f.err
}

func TestGetFiltersAndGroupsBoundServers(t *testing.T) {
	f := &fakeReader{
		app: model.ChannelApp{Config: json.RawMessage(`{"payEnabled":true}`)},
		groups: []model.BoundGroup{
			{RegionID: 10, RegionCode: "region_1", ServerGroupCode: "group_1001"},
			{RegionID: 10, RegionCode: "region_1", ServerGroupCode: "group_1002"},
			{RegionID: 20, RegionCode: "region_2", ServerGroupCode: "group_2001"},
		},
	}
	got, err := NewConfigService(f).Get(context.Background(), "prod", "huawei_10001")
	if err != nil {
		t.Fatal(err)
	}
	if got.AppID != "huawei_10001" || got.Env != "prod" || string(got.Config) != `{"payEnabled":true}` {
		t.Fatalf("unexpected data: %+v", got)
	}
	want := []model.Region{
		{Code: "region_1", ServerGroups: []model.ServerGroup{{Code: "group_1001"}, {Code: "group_1002"}}},
		{Code: "region_2", ServerGroups: []model.ServerGroup{{Code: "group_2001"}}},
	}
	if !reflect.DeepEqual(got.Regions, want) {
		t.Fatalf("regions = %+v; want %+v", got.Regions, want)
	}
}

func TestGetEmptyBindingsReturnsEmptyArray(t *testing.T) {
	f := &fakeReader{app: model.ChannelApp{Config: json.RawMessage("{}")}}
	got, err := NewConfigService(f).Get(context.Background(), "review", "app")
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"appid":"app","env":"review","config":{},"regions":[]}` {
		t.Fatalf("unexpected JSON: %s", out)
	}
}

func TestGetRejectsInvalidInputBeforeDatabase(t *testing.T) {
	for _, tc := range []struct{ env, appID string }{
		{"production", "app"}, {"staging", "app"}, {"prod", ""}, {"prod", "   "},
	} {
		f := &fakeReader{}
		_, err := NewConfigService(f).Get(context.Background(), tc.env, tc.appID)
		if !errors.Is(err, model.ErrInvalidRequest) || f.calls != 0 {
			t.Errorf("env=%q appID=%q err=%v calls=%d", tc.env, tc.appID, err, f.calls)
		}
	}
}

func TestGetPropagatesLookupError(t *testing.T) {
	f := &fakeReader{err: model.ErrAmbiguous}
	_, err := NewConfigService(f).Get(context.Background(), "prod", "app")
	if !errors.Is(err, model.ErrAmbiguous) {
		t.Fatal(err)
	}
}
