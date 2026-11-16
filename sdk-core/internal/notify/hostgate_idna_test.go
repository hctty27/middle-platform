package notify

import "testing"

func TestHostGateIDNAEquivalence(t *testing.T) {
	for _, pair := range [][2]string{
		{"http://bücher.example/a", "http://xn--bcher-kva.example/b"},
		{"http://ＢÜCHER.example。/a", "http://xn--bcher-kva.example:80/b"},
		{"https://例え.テスト/a", "https://xn--r8jz45g.xn--zckzah:443/b"},
	} {
		t.Run(pair[0], func(t *testing.T) {
			gate := NewHostGate(1)
			release, ok := gate.TryAcquire(pair[0])
			if !ok {
				t.Fatal("first target rejected")
			}
			defer release()
			other, ok := gate.TryAcquire(pair[1])
			if ok {
				other()
				t.Fatal("equivalent IDNA host bypassed limit")
			}
		})
	}
}
