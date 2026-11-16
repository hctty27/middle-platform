package notify

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestHostGateCanonicalizationAndIsolation(t *testing.T) {
	for _, pair := range [][2]string{
		{"http://GAME.example/one", "http://game.example.:80/two"},
		{"https://game.example/one", "https://GAME.example:443/two"},
		{"http://[0:0:0:0:0:0:0:1]/one", "http://[::1]:80/two"},
		{"http://game.example:080/a", "http://game.example/b"},
	} {
		gate := NewHostGate(1)
		release, ok := gate.TryAcquire(pair[0])
		if !ok {
			t.Fatal("first acquisition failed")
		}
		if _, ok := gate.TryAcquire(pair[1]); ok {
			t.Fatalf("same host not limited: %v", pair)
		}
		other, ok := gate.TryAcquire("http://other.example/x")
		if !ok {
			t.Fatal("different host blocked")
		}
		other()
		release()
		release()
		again, ok := gate.TryAcquire(pair[1])
		if !ok {
			t.Fatal("release did not restore quota")
		}
		again()
		if len(gate.active) != 0 {
			t.Fatal("inactive host retained")
		}
	}
	gate := NewHostGate(1)
	a, _ := gate.TryAcquire("http://same.example/")
	defer a()
	b, ok := gate.TryAcquire("https://same.example/")
	if !ok {
		t.Fatal("different effective ports must be isolated")
	}
	b()
}

func TestHostGateNoHistoryAndDisabled(t *testing.T) {
	gate := NewHostGate(1)
	for i := 0; i < 2000; i++ {
		release, ok := gate.TryAcquire(fmt.Sprintf("http://h%d.example/path", i))
		if !ok {
			t.Fatal("acquire failed")
		}
		release()
	}
	if len(gate.active) != 0 {
		t.Fatal("historical map growth")
	}
	disabled := NewHostGate(0)
	for i := 0; i < 10; i++ {
		release, ok := disabled.TryAcquire("http://same.example/")
		if !ok {
			t.Fatal("disabled gate rejected")
		}
		release()
	}
	if len(disabled.active) != 0 {
		t.Fatal("disabled gate allocates history")
	}
	release, ok := gate.TryAcquire("invalid URL")
	if !ok {
		t.Fatal("invalid target should reach notifier validation")
	}
	release()
}

func TestHostGateConcurrentLimit(t *testing.T) {
	gate := NewHostGate(3)
	var inside, max atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				release, ok := gate.TryAcquire("http://game.example/path")
				if !ok {
					continue
				}
				n := inside.Add(1)
				for old := max.Load(); n > old && !max.CompareAndSwap(old, n); old = max.Load() {
				}
				inside.Add(-1)
				release()
			}
		}()
	}
	wg.Wait()
	if max.Load() > 3 {
		t.Fatalf("limit exceeded: %d", max.Load())
	}
	if len(gate.active) != 0 {
		t.Fatal("active entry leaked")
	}
}
