package access

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestStatusPreservesOpaqueID(t *testing.T) {
	client, err := NewClient("https://access.invalid/api/", "test-token")
	if err != nil {
		t.Fatal(err)
	}

	for id, path := range map[string]string{
		"..":                         "/api/requests/%2E%2E",
		"//other.invalid/requests?#": "/api/requests/%2F%2Fother.invalid%2Frequests%3F%23",
	} {
		client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "access.invalid" || r.URL.EscapedPath() != path {
				t.Fatalf("unexpected request URL %s", r.URL)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"ticket-1","status":"approved"}`)),
			}, nil
		})
		ticket, err := client.Status(context.Background(), id)
		if err != nil || ticket.ID != "ticket-1" {
			t.Fatalf("Status(%q) = %+v, %v", id, ticket, err)
		}
	}
}

func TestCreateDoesNotFollowRedirect(t *testing.T) {
	client, err := NewClient("https://access.invalid", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/requests" {
			t.Fatalf("redirect was followed to %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Error("missing access service authorization")
		}
		if got := r.Header.Get("Idempotency-Key"); got != "test-key" {
			t.Error("missing idempotency key")
		}
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": {"/redirected"}},
			Body:       http.NoBody,
		}, nil
	})
	if _, err := client.Create(context.Background(), "test-key", []byte(`{}`)); err == nil {
		t.Fatal("Create succeeded after a redirect")
	}
}
