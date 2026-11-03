package auth

import (
    "bytes"
    "encoding/json"
    "testing"
)

func TestParseGameSecretLiteralUTF8(t *testing.T){
    for _,tc:=range []string{"short","  secret  ","自定义密钥!","AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}{
        raw,_:=json.Marshal(map[string]string{"gameSecret":tc})
        actual,err:=ParseGameSecret(raw)
        if err!=nil||!bytes.Equal(actual,[]byte(tc)){t.Fatalf("parse %q: %v, %q",tc,err,actual)}
    }
    for _,raw:=range []string{`{}`,`{"gameSecret":""}`,`{"gameSecret":"  "}`,`{"gameSecret":null}`,`{"gameSecret":42}`} {
        if _,err:=ParseGameSecret([]byte(raw));err==nil{t.Errorf("accepted %s",raw)}
    }
}
