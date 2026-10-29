package config

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid request")

type LocalDateTime time.Time

func (t LocalDateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).Format("2006-01-02T15:04:05.999999"))
}

type Node struct {
	ID            int64         `json:"id"`
	NodeType      string        `json:"nodeType"`
	GameID        *int64        `json:"gameId"`
	RegionID      *int64        `json:"regionId"`
	ServerGroupID *int64        `json:"serverGroupId"`
	Code          string        `json:"code"`
	Name          string        `json:"name"`
	Status        int           `json:"status"`
	Config        string        `json:"config"`
	Remark        *string       `json:"remark"`
	CreatedAt     LocalDateTime `json:"createdAt"`
	UpdatedAt     LocalDateTime `json:"updatedAt"`
	ParentID      int64         `json:"parentId"`
}

func (n Node) Parent() int64 {
	switch n.NodeType {
	case "region":
		return value(n.GameID)
	case "server_group":
		return value(n.RegionID)
	case "channel_app":
		return value(n.ServerGroupID)
	default:
		return 0
	}
}

func value(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

type NodeSummary struct {
	ID       int64  `json:"id"`
	ParentID int64  `json:"parentId"`
	NodeType string `json:"nodeType"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Status   int    `json:"status"`
	Leaf     bool   `json:"leaf"`
}

func summary(n Node) NodeSummary {
	return NodeSummary{n.ID, n.Parent(), n.NodeType, n.Code, n.Name, n.Status, n.NodeType == "server_group" || n.NodeType == "channel_app"}
}

type NodePage struct {
	Items []NodeSummary `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

type NodeMatch struct {
	Node NodeSummary   `json:"node"`
	Path []NodeSummary `json:"path"`
}

type Revision struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
}

type ChangeEvent struct {
	Revision  int64         `json:"revision"`
	NodeID    int64         `json:"nodeId"`
	Operation string        `json:"operation"`
	CreatedAt LocalDateTime `json:"createdAt"`
}

type Snapshot struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
	Nodes    []Node `json:"nodes"`
}

type Subscriber struct {
	ID                   int64   `json:"id"`
	Code                 string  `json:"code"`
	CallbackURL          string  `json:"callbackUrl"`
	Enabled              bool    `json:"enabled"`
	AcknowledgedRevision int64   `json:"acknowledgedRevision"`
	Failures             int     `json:"failures"`
	LastError            *string `json:"lastError"`
}
