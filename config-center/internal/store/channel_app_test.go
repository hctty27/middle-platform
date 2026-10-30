package store

import (
	"encoding/json"
	"reflect"
 "strings"
	"testing"
)

func TestMergeChannelConfig(t *testing.T) {
	base:=`{"appId":9223372036854775807,"auth":{"url":"prod","timeout":30},"channel":"huawei"}`
	patch:=`{"auth":{"url":"review"},"channel":null,"callback":"review"}`
	got,err:=MergeChannelConfig(base,patch)
	if err!=nil { t.Fatal(err) }
	want:=`{"appId":9223372036854775807,"auth":{"url":"review","timeout":30},"callback":"review"}`
	var a,b any
	dec:=json.NewDecoder(strings.NewReader(got)); dec.UseNumber(); if err:=dec.Decode(&a);err!=nil { t.Fatal(err) }
	dec=json.NewDecoder(strings.NewReader(want)); dec.UseNumber(); if err:=dec.Decode(&b);err!=nil { t.Fatal(err) }
	if !reflect.DeepEqual(a,b) { t.Fatalf("got %s, want %s",got,want) }
}

func TestMergeChannelConfigRejectsNonObjects(t *testing.T) {
	for _,tc:=range [][2]string{{"[]","{}"},{"{}","[]"},{"null","{}"},{"{}","null"},{"bad","{}"}} {
		if _,err:=MergeChannelConfig(tc[0],tc[1]);err==nil { t.Fatalf("accepted %v",tc) }
	}
}
