// Package service 处理 SDK 配置读取与大区分组。
package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// Reader 由数据仓库提供，便于业务逻辑独立测试。
type Reader interface {
	Lookup(context.Context, string, string) (model.ChannelApp, []model.BoundGroup, error)
}

type ConfigService struct {
	reader Reader
}

func NewConfigService(reader Reader) *ConfigService {
	return &ConfigService{reader: reader}
}

// Get 严格区分 test/review/prod，且仅组装已绑定区服组。
func (s *ConfigService) Get(ctx context.Context, env, appID string) (model.ConfigData, error) {
	if env != "test" && env != "review" && env != "prod" {
		return model.ConfigData{}, model.ErrInvalidRequest
	}
	if strings.TrimSpace(appID) == "" || utf8.RuneCountInString(appID) > 128 {
		return model.ConfigData{}, model.ErrInvalidRequest
	}

	app, groups, err := s.reader.Lookup(ctx, appID, env)
	if err != nil {
		return model.ConfigData{}, err
	}
	out := model.ConfigData{
		AppID: appID,
		Env: env,
		Config: app.Config,
		Regions: make([]model.Region, 0),
	}

	byRegion := make(map[int64]int)
	for _, group := range groups {
		index, ok := byRegion[group.RegionID]
		if !ok {
			index = len(out.Regions)
			byRegion[group.RegionID] = index
			out.Regions = append(out.Regions, model.Region{
				Code: group.RegionCode,
				ServerGroups: make([]model.ServerGroup, 0),
			})
		}
		out.Regions[index].ServerGroups = append(out.Regions[index].ServerGroups,
			model.ServerGroup{Code: group.ServerGroupCode})
	}
	return out, nil
}
