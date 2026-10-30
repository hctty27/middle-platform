package store

import (
    "testing"
    "time"
    "github.com/hctty27/middle-platform/config-center-access/internal/remote"
)

// TestValidateNodeEnvironment 验证副本节点只允许大区设置合法的环境类型。
func TestValidateNodeEnvironment(t *testing.T) {
    now := time.Now().UTC().Format("2006-01-02T15:04:05.999999")
    env := "review"
    node := remote.Node{ID: 1, NodeType: "region", EnvType: &env, Code: "r", Name: "Region",
        Config: "{}", CreatedAt: now, UpdatedAt: now}
    if err := validateNode(node); err != nil { t.Fatal(err) }
    env = "bad"
    if err := validateNode(node); err == nil { t.Fatal("accepted bad environment") }
    env = "review"
    node.NodeType = "game"
    if err := validateNode(node); err == nil { t.Fatal("accepted environment on game") }
    node.EnvType = nil
    if err := validateNode(node); err != nil { t.Fatal(err) }
}
