package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type fakeFetcher struct {
	result model.ConfigData
	err    error
	env    string
	appID  string
}

func (f *fakeFetcher) Get(_ context.Context, env, appID string) (model.ConfigData, error) {
	f.env, f.appID = env, appID
	return f.result, f.err
}

type fakePinger struct{ err error }

func (p fakePinger) PingContext(context.Context) error { return p.err }

func TestConfigRouteReturnsRawJSONAndBoundGroups(t *testing.T) {
	f := &fakeFetcher{result: model.ConfigData{
		AppID: "app", Env: "prod",
		Config: json.RawMessage(`{"loginEnabled":true}`),
		Regions: []model.Region{
			{Code: "region_1", ServerGroups: []model.ServerGroup{{Code: "group_1001"}}},
		},
	}}
	rec := httptest.NewRecorder()
	Router(f, fakePinger{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/core/prod/app", nil))
	if rec.Code != http.StatusOK || f.env != "prod" || f.appID != "app" {
		t.Fatalf("status=%d env=%s appID=%s", rec.Code, f.env, f.appID)
	}
	if !strings.Contains(rec.Body.String(), `"config":{"loginEnabled":true}`) ||
		!strings.Contains(rec.Body.String(), `"code":"group_1001"`) {
		t.Fatal(rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected Cache-Control: %s", rec.Header().Get("Cache-Control"))
	}
}

func TestConfigErrors(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{model.ErrInvalidRequest, 400},
		{model.ErrNotFound, 404},
		{model.ErrAmbiguous, 409},
		{model.ErrInvalidConfig, 500},
		{errors.New("database unavailable"), 503},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		Router(&fakeFetcher{err: tc.err}, fakePinger{}).ServeHTTP(rec,
			httptest.NewRequest(http.MethodGet, "/api/core/review/app", nil))
		if rec.Code != tc.want {
			t.Errorf("err=%v got=%d want=%d", tc.err, rec.Code, tc.want)
		}
	}
}

func TestHealthProbes(t *testing.T) {
	router := Router(&fakeFetcher{}, fakePinger{err: errors.New("offline")})
	for _, tc := range []struct {
		url  string
		want int
	}{
		{"/health/live", 200},
		{"/health/ready", 503},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if rec.Code != tc.want {
			t.Errorf("%s: got=%d want=%d", tc.url, rec.Code, tc.want)
		}
	}
}
