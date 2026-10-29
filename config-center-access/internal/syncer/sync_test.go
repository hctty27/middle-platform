package syncer

import (
	"testing"
	"github.com/hctty27/middle-platform/config-center-access/internal/remote"
)

func TestContiguous(t *testing.T) {
	cases := []struct{
		name string
		after int64
		events []remote.Event
		want bool
	}{
		{"valid",7,[]remote.Event{{Revision:8},{Revision:9}},true},
		{"empty",7,nil,false},
		{"gap",7,[]remote.Event{{Revision:9}},false},
		{"duplicate",7,[]remote.Event{{Revision:8},{Revision:8}},false},
	}
	for _, tc := range cases {
		t.Run(tc.name,func(t *testing.T) {
			if actual := contiguous(tc.after,tc.events); actual != tc.want { t.Fatalf("got %v",actual) }
		})
	}
}
