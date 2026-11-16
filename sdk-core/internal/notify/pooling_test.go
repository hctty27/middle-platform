package notify

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

func deliveryOrder(target string) model.Order {
	return model.Order{AppID: "app", GameOrderID: "G1", PlatformOrderNo: "P1", Amount: 1, PaidAt: time.Now(), GameNotifyURL: target}
}

func TestPoolDefaultsAndOptions(t *testing.T) {
	c := NewClient()
	defer c.CloseIdleConnections()
	tr := c.http.Transport.(*http.Transport)
	if tr.DisableKeepAlives || tr.MaxIdleConns != 32 || tr.MaxIdleConnsPerHost != 4 || tr.MaxConnsPerHost != 4 || tr.IdleConnTimeout != 30*time.Second {
		t.Fatalf("incorrect defaults: %+v", tr)
	}
	if tr.Proxy != nil || c.http.Timeout != 5*time.Second || tr.TLSHandshakeTimeout != 3*time.Second || tr.ResponseHeaderTimeout != 4*time.Second {
		t.Fatal("safety/timeouts changed")
	}
	c = NewClientWithOptions(Options{DisableKeepAlives: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 3, IdleConnTimeout: time.Second})
	defer c.CloseIdleConnections()
	tr = c.http.Transport.(*http.Transport)
	if !tr.DisableKeepAlives || tr.MaxIdleConns != 8 || tr.MaxIdleConnsPerHost != 2 || tr.MaxConnsPerHost != 3 || tr.IdleConnTimeout != time.Second {
		t.Fatal("options ignored")
	}
}

func TestSuccessfulACKReusesConnection(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "reuse", true: "disabled"}[disabled], func(t *testing.T) {
			var connections atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				io.WriteString(w, r.Header.Get("x-nonce"))
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			server.Start()
			defer server.Close()
			c := NewClientWithOptions(Options{DisableKeepAlives: disabled})
			defer c.CloseIdleConnections()
			for i := 0; i < 3; i++ {
				if err := c.Send(context.Background(), deliveryOrder(server.URL), []byte("secret")); err != nil {
					t.Fatal(err)
				}
			}
			want := int32(1)
			if disabled {
				want = 3
			}
			if got := connections.Load(); got != want {
				t.Fatalf("connections=%d want=%d", got, want)
			}
			c.CloseIdleConnections()
			if err := c.Send(context.Background(), deliveryOrder(server.URL), []byte("secret")); err != nil {
				t.Fatal(err)
			}
			if connections.Load() != want+1 {
				t.Fatal("CloseIdleConnections did not close pool")
			}
		})
	}
}

func TestACKMustBeExactAndHTTP200(t *testing.T) {
	for _, mode := range []string{"empty", "wrong", "whitespace", "extra", "201", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				io.Copy(io.Discard, r.Body)
				nonce := r.Header.Get("x-nonce")
				switch mode {
				case "empty":
				case "wrong":
					io.WriteString(w, "wrong")
				case "whitespace":
					io.WriteString(w, nonce+"\n")
				case "extra":
					io.WriteString(w, nonce+"x")
				case "201":
					w.WriteHeader(201)
					io.WriteString(w, nonce)
				case "redirect":
					w.Header().Set("Location", "/destination")
					w.WriteHeader(307)
				case "oversize":
					io.WriteString(w, nonce+strings.Repeat("x", 65536))
				}
			}))
			defer server.Close()
			c := NewClient()
			defer c.CloseIdleConnections()
			if err := c.Send(context.Background(), deliveryOrder(server.URL), []byte("secret")); err == nil {
				t.Fatal("invalid ACK accepted")
			}
			if requests.Load() != 1 {
				t.Fatal("redirect followed")
			}
		})
	}
}

func TestOversizeACKDoesNotDrainOrReuse(t *testing.T) {
	var connections atomic.Int32
	unblock := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/bad" {
			w.Header().Set("Content-Length", "1000000")
			io.WriteString(w, strings.Repeat("x", 128))
			w.(http.Flusher).Flush()
			<-unblock
			return
		}
		io.WriteString(w, r.Header.Get("x-nonce"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	defer close(unblock)
	c := NewClient()
	defer c.CloseIdleConnections()
	done := make(chan error, 1)
	go func() { done <- c.Send(context.Background(), deliveryOrder(server.URL+"/bad"), []byte("secret")) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("oversize accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("response was drained or waited for EOF")
	}
	if err := c.Send(context.Background(), deliveryOrder(server.URL), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if connections.Load() != 2 {
		t.Fatal("oversized response connection reused")
	}
}

func TestSafeDialChecksAllDNSBeforeDial(t *testing.T) {
	for _, ips := range [][]string{{"127.0.0.1", "169.254.169.254"}, {"100.100.100.200", "127.0.0.1"}, {"::ffff:169.254.169.254"}} {
		called := false
		dial := safeDialContext(func(context.Context, string) ([]net.IPAddr, error) {
			var result []net.IPAddr
			for _, ip := range ips {
				result = append(result, net.IPAddr{IP: net.ParseIP(ip)})
			}
			return result, nil
		}, func(context.Context, string, string) (net.Conn, error) {
			called = true
			return nil, errors.New("unexpected dial")
		})
		_, err := dial(context.Background(), "tcp", "example.test:80")
		if !errors.Is(err, ErrTarget) || called {
			t.Fatalf("mixed/prohibited DNS dialed: %v, %t", err, called)
		}
	}
	var addresses []string
	dial := safeDialContext(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}, func(_ context.Context, _, address string) (net.Conn, error) {
		addresses = append(addresses, address)
		return nil, errors.New("refused")
	})
	dial(context.Background(), "tcp", "safe.test:8080")
	if strings.Join(addresses, ",") != "10.0.0.1:8080,127.0.0.1:8080" {
		t.Fatalf("did not dial checked IPs directly: %v", addresses)
	}
}
