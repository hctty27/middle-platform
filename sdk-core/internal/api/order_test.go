package api

import (
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)
func TestDecodeStrict(t *testing.T){
    cases:=[]string{
        `{"game_order_id":"1","amount":2,"amount":3,"attach":""}`,
        `{"game_order_id":"1","amount":2}`,
        `{"game_order_id":"1","amount":2,"attach":"","extra":1}`,
        `{"game_order_id":"1","amount":"2","attach":""}`,
    }
    for _,raw:=range cases{
        var req model.RegisterOrder
        if err:=decodeOrderJSON([]byte(raw),[]string{"game_order_id","amount","attach"},&req);err==nil{
            t.Errorf("accepted %s",raw)
        }
    }
}
