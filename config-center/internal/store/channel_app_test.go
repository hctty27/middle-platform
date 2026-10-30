package store

import "testing"

func TestValidChannelAppConfig(t *testing.T) {
    for _,tc:=range []struct{raw string;valid bool}{
        {`{}`,true},{`{"a":1,"nested":{"code":1234567890123456789}}`,true},
        {`[]`,false},{`null`,false},{`{"a":`,false},
    }{
        if got:=validObject(tc.raw);got!=tc.valid {
            t.Fatalf("validObject(%q) = %v, want %v",tc.raw,got,tc.valid)
        }
    }
}
