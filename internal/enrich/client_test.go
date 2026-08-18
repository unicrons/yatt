package enrich_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/unicrons/yatt/internal/enrich"
)

// abuseIPDBServer returns an httptest.Server standing in for AbuseIPDB's
// check endpoint, along with a counter of how many requests it received.
// handler decides the response for each request.
func abuseIPDBServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// newTestClient builds a Client pointed at srv instead of the real AbuseIPDB
// endpoint, restoring enrich.CheckURL once the test ends.
func newTestClient(t *testing.T, srv *httptest.Server, key string) *enrich.Client {
	t.Helper()
	original := enrich.CheckURL
	enrich.CheckURL = srv.URL
	t.Cleanup(func() { enrich.CheckURL = original })
	return enrich.NewClient(key, enrich.DefaultRate)
}

func TestScoreSendsKeyHeaderAndQueryParams(t *testing.T) {
	var gotKey, gotIP, gotMaxAge string
	srv, _ := abuseIPDBServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Key")
		gotIP = r.URL.Query().Get("ipAddress")
		gotMaxAge = r.URL.Query().Get("maxAgeInDays")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"abuseConfidenceScore":42}}`))
	})

	c := newTestClient(t, srv, "test-key")
	score, err := c.Score(context.Background(), "192.0.2.1")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if score != 42 {
		t.Errorf("score = %d, want 42", score)
	}
	if gotKey != "test-key" {
		t.Errorf("Key header = %q, want %q", gotKey, "test-key")
	}
	if gotIP != "192.0.2.1" {
		t.Errorf("ipAddress param = %q, want %q", gotIP, "192.0.2.1")
	}
	if gotMaxAge != "90" {
		t.Errorf("maxAgeInDays param = %q, want %q", gotMaxAge, "90")
	}
}

func TestScoreRejectsInvalidIP(t *testing.T) {
	srv, calls := abuseIPDBServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c := newTestClient(t, srv, "test-key")

	if _, err := c.Score(context.Background(), "not-an-ip"); err == nil {
		t.Fatal("Score(\"not-an-ip\") succeeded, want an error")
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Errorf("server received %d requests, want 0 for an invalid IP", got)
	}
}

func TestScoreCachesByIP(t *testing.T) {
	srv, calls := abuseIPDBServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"abuseConfidenceScore":7}}`))
	})
	c := newTestClient(t, srv, "test-key")

	for i := 0; i < 3; i++ {
		score, err := c.Score(context.Background(), "192.0.2.1")
		if err != nil {
			t.Fatalf("Score call %d: %v", i, err)
		}
		if score != 7 {
			t.Errorf("call %d: score = %d, want 7", i, score)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("server received %d requests, want exactly 1 for a repeated IP", got)
	}
}

func TestScoreSurfacesInvalidKeyAsHardError(t *testing.T) {
	srv, _ := abuseIPDBServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := newTestClient(t, srv, "bad-key")

	_, err := c.Score(context.Background(), "192.0.2.1")
	if err == nil {
		t.Fatal("Score succeeded, want an error for a 401 response")
	}
	if !errors.Is(err, enrich.ErrInvalidKey) {
		t.Errorf("error = %v, want it to wrap enrich.ErrInvalidKey", err)
	}
}

func TestScoreDegradesOtherFailuresWithoutPanicking(t *testing.T) {
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name: "500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "429",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
			},
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`not json`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := abuseIPDBServer(t, tt.handler)
			c := newTestClient(t, srv, "test-key")

			_, err := c.Score(context.Background(), "192.0.2.1")
			if err == nil {
				t.Fatal("Score succeeded, want an error")
			}
			if errors.Is(err, enrich.ErrInvalidKey) {
				t.Errorf("error = %v, want a best-effort failure, not ErrInvalidKey", err)
			}
		})
	}
}
