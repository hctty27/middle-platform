package service

import (
    "testing"
    "github.com/hctty27/middle-platform/config-center/internal/model"
)

// TestRegionEnvironmentValidation 覆盖测试服、提审服、正式服枚举以及非法环境分类。
func TestRegionEnvironmentValidation(t *testing.T) {
    status := 1
    for _, env := range []string{"test", "review", "prod"} {
        n := model.CreateNodeRequest{ParentID: 1, NodeType: "region", EnvType: &env, Code: "r", Name: "Region", Status: &status, Config: "{}"}
        if err := ValidateCreate(n); err != nil { t.Fatalf("%s: %v", env, err) }
    }
    invalid := "staging"
    legacy := "production"
    for _, n := range []model.CreateNodeRequest{
        {ParentID: 1, NodeType: "region", Code: "r", Name: "Region", Status: &status, Config: "{}"},
        {ParentID: 1, NodeType: "region", EnvType: &invalid, Code: "r", Name: "Region", Status: &status, Config: "{}"},
        {ParentID: 1, NodeType: "region", EnvType: &legacy, Code: "r", Name: "Region", Status: &status, Config: "{}"},
        {NodeType: "game", EnvType: &invalid, Code: "g", Name: "Game", Status: &status, Config: "{}"},
    } {
        if err := ValidateCreate(n); err == nil { t.Fatalf("accepted invalid environment: %+v", n) }
    }
}

// TestGameMarketTypeValidation 验证枚举合法性、非游戏节点约束及旧客户端兼容。
func TestGameMarketTypeValidation(t *testing.T) {
    status := 1
    for _, market := range []string{"domestic", "overseas"} {
        n := model.CreateNodeRequest{NodeType: "game", MarketType: &market, Code: "g", Name: "Game", Status: &status, Config: "{}"}
        if err := ValidateCreate(n); err != nil { t.Fatalf("%s: %v", market, err) }
    }
    if err := ValidateCreate(model.CreateNodeRequest{NodeType: "game", Code: "g", Name: "Game", Status: &status, Config: "{}"}); err != nil { t.Fatal(err) }
    invalid, env := "global", "test"
    for _, n := range []model.CreateNodeRequest{
        {NodeType: "game", MarketType: &invalid, Code: "g", Name: "Game", Status: &status, Config: "{}"},
        {NodeType: "region", ParentID: 1, MarketType: &invalid, EnvType: &env, Code: "r", Name: "Region", Status: &status, Config: "{}"},
    } {
        if err := ValidateCreate(n); err == nil { t.Fatalf("accepted invalid market: %+v", n) }
    }
}
