package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type staticTokenSource struct {
	token string
}

func (s staticTokenSource) Token(context.Context) (string, error)   { return s.token, nil }
func (s staticTokenSource) Reload(context.Context) (string, error)  { return s.token, nil }
func (s staticTokenSource) Refresh(context.Context) (string, error) { return s.token, nil }

func TestDoGetRetriesTransientNetworkError(t *testing.T) {
	oldClient := usageHTTPClient
	defer func() { usageHTTPClient = oldClient }()

	attempts := 0
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, syscall.ECONNRESET
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})}

	body, status, _, err := doGet(context.Background(), "token", func(token string) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "https://example.test/usage", nil)
	})
	if err != nil {
		t.Fatalf("doGet: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDoGetDoesNotRetryUnauthorized(t *testing.T) {
	oldClient := usageHTTPClient
	defer func() { usageHTTPClient = oldClient }()

	attempts := 0
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{"error":"expired"}`)),
			Request:    req,
		}, nil
	})}

	body, status, _, err := doGet(context.Background(), "token", func(token string) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "https://example.test/usage", nil)
	})
	if err != nil {
		t.Fatalf("doGet: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
	}
	if string(body) != `{"error":"expired"}` {
		t.Fatalf("body = %q", body)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestDoGetDoesNotRetryTooManyRequests(t *testing.T) {
	oldClient := usageHTTPClient
	defer func() { usageHTTPClient = oldClient }()

	attempts := 0
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"error":"slow down"}`)),
			Request:    req,
		}, nil
	})}

	body, status, _, err := doGet(context.Background(), "token", func(token string) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "https://example.test/usage", nil)
	})
	if err != nil {
		t.Fatalf("doGet: %v", err)
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", status, http.StatusTooManyRequests)
	}
	if string(body) != `{"error":"slow down"}` {
		t.Fatalf("body = %q", body)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestFetchWithAuthPreservesRetryAfter(t *testing.T) {
	oldClient := usageHTTPClient
	defer func() { usageHTTPClient = oldClient }()

	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"42"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"slow down"}`)),
			Request:    req,
		}, nil
	})}

	start := time.Now()
	_, err := fetchWithAuth(context.Background(), staticTokenSource{token: "token"}, func(token string) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "https://example.test/usage", nil)
	})
	var httpErr *UsageHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %T %v, want UsageHTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", httpErr.StatusCode, http.StatusTooManyRequests)
	}
	if httpErr.RetryAfter.Before(start.Add(42*time.Second)) || httpErr.RetryAfter.After(time.Now().Add(43*time.Second)) {
		t.Fatalf("retry_after = %s, want about 42s from now", httpErr.RetryAfter)
	}
}

func TestUsageHTTPClientDisablesHTTP2(t *testing.T) {
	client := newUsageHTTPClient()
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if tr.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 = true, want false")
	}
	if tr.TLSNextProto == nil {
		t.Fatal("TLSNextProto = nil, want empty map to disable HTTP/2")
	}
	if len(tr.TLSNextProto) != 0 {
		t.Fatalf("TLSNextProto len = %d, want 0", len(tr.TLSNextProto))
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig = nil")
	}
	if len(tr.TLSClientConfig.NextProtos) != 1 || tr.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatalf("NextProtos = %#v, want [http/1.1]", tr.TLSClientConfig.NextProtos)
	}
}
