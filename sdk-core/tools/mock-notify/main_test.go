package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockNotifySuccess(t *testing.T) {
	handler := newHandler(modeSuccess, log.New(io.Discard, "", 0))
	request := httptest.NewRequest(http.MethodPost, "/pay/notify",
		strings.NewReader("{\"order_no\":\"P100\",\"amount\":600}"))
	request.Header.Set("X-SDK-Nonce", "0123456789abcdef")
	request.Header.Set("X-SDK-AppID", "h0200")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "0123456789abcdef" {
		t.Fatalf("unexpected response: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestMockNotifyFailMode(t *testing.T) {
	handler := newHandler(modeFail, log.New(io.Discard, "", 0))
	request := httptest.NewRequest(http.MethodPost, "/pay/notify", strings.NewReader("{}"))
	request.Header.Set("X-SDK-Nonce", "0123456789abcdef")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500, got %d", response.Code)
	}
}

func TestMockNotifyRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		nonce  string
		status int
	}{
		{"invalid json", http.MethodPost, "/pay/notify", "{", "0123456789abcdef", http.StatusBadRequest},
		{"empty nonce", http.MethodPost, "/pay/notify", "{}", "", http.StatusBadRequest},
		{"bad nonce length", http.MethodPost, "/pay/notify", "{}", "0123", http.StatusBadRequest},
		{"uppercase nonce", http.MethodPost, "/pay/notify", "{}", "0123456789ABCDEF", http.StatusBadRequest},
		{"wrong method", http.MethodGet, "/pay/notify", "{}", "0123456789abcdef", http.StatusMethodNotAllowed},
		{"wrong path", http.MethodPost, "/wrong/path", "{}", "0123456789abcdef", http.StatusNotFound},
		{"oversized body", http.MethodPost, "/pay/notify", string(bytes.Repeat([]byte("x"), 64<<10+1)), "0123456789abcdef", http.StatusBadRequest},
	}
	handler := newHandler(modeSuccess, log.New(io.Discard, "", 0))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("X-SDK-Nonce", tc.nonce)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("unexpected status: got %d want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestMockNotifyHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler(modeSuccess, log.New(io.Discard, "", 0)).ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"status\":\"UP\"}" {
		t.Fatalf("unexpected health result: status=%d body=%q", rec.Code, rec.Body.String())
	}
}
