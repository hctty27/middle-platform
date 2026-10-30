package service

import (
    "testing"
    "github.com/hctty27/middle-platform/config-center/internal/model"
)

// TestRegionEnvironmentValidation 覆盖测试服、提审服、正式服枚举以及非法环境分类。
func TestRegionEnvironmentValidation(t *testing.T) {
    status := 1
    for _, env := range []string{"test", "review", "production"} {
        n := model.CreateNodeRequest{ParentID: 1, NodeType: "region", EnvType: &env, Code: "r", Name: "Region", Status: &status, Config: "{}"}
        if err := ValidateCreate(n); err != nil { t.Fatalf("%s: %v", env, err) }
    }
    invalid := "staging"
    for _, n := range []model.CreateNodeRequest{
        {ParentID: 1, NodeType: "region", Code: "r", Name: "Region", Status: &status, Config: "{}"},
        {ParentID: 1, NodeType: "region", EnvType: &invalid, Code: "r", Name: "Region", Status: &status, Config: "{}"},
        {NodeType: "game", EnvType: &invalid, Code: "g", Name: "Game", Status: &status, Config: "{}"},
    } {
        if err := ValidateCreate(n); err == nil { t.Fatalf("accepted invalid environment: %+v", n) }
    }
}
