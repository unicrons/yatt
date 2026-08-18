package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

// ErrInvalidKey is wrapped into the error Score returns when AbuseIPDB
// rejects the configured key (401/403). Every subsequent lookup would fail
// the same way, so callers treat it as a hard error rather than a per-IP
// best-effort failure.
var ErrInvalidKey = errors.New("AbuseIPDB rejected the configured key")

// DefaultRate bounds how many requests Client sends to AbuseIPDB per second
// when NewClient is given a qps of zero or less. It is deliberately
// conservative — safe for a free-tier quota — since a caller who knows they
// are on a paid plan is the one who opts into a higher rate.
const DefaultRate float64 = 1

// requestTimeout bounds a single AbuseIPDB lookup, so one slow response
// cannot stall the whole render step.
const requestTimeout = 10 * time.Second

// Client makes live AbuseIPDB Confidence of Abuse lookups. Unlike the rest of
// this package, calling it sends real HTTP requests and spends API credits —
// callers only construct one when the user opted in.
type Client struct {
	key   string
	http  *http.Client
	limit *rate.Limiter
	cache map[string]int
}

// NewClient returns a Client authenticating with key, sending at most qps
// requests per second (DefaultRate if qps is zero or negative). The caller is
// responsible for validating key is non-empty; NewClient does not.
func NewClient(key string, qps float64) *Client {
	if qps <= 0 {
		qps = DefaultRate
	}
	return &Client{
		key:  key,
		http: &http.Client{},
		// A burst of one keeps the spacing even, mirroring
		// internal/resolver's RateLimited: a larger burst would let a run of
		// unique addresses spike right when AbuseIPDB is most likely to start
		// throttling it.
		limit: rate.NewLimiter(rate.Limit(qps), 1),
		cache: make(map[string]int),
	}
}

// CheckURL is the AbuseIPDB "check" endpoint Score queries. It is a var
// rather than a const purely so tests — in this package and others — can
// point it at a fake server instead of the real API; production code never
// changes it.
var CheckURL = "https://api.abuseipdb.com/api/v2/check"

// checkResponse is the subset of AbuseIPDB's check response this package
// reads.
type checkResponse struct {
	Data struct {
		AbuseConfidenceScore int `json:"abuseConfidenceScore"`
	} `json:"data"`
}

// Score returns ip's AbuseIPDB Confidence of Abuse percentage, caching the
// result so a scan with many candidates resolving to the same address only
// pays for the lookup once.
//
// A 401/403 response is returned as a hard error — it means the configured
// key is invalid, which every subsequent lookup would also fail on. Any other
// failure (timeout, 429, 5xx, a malformed body) is also returned as an error,
// but callers are expected to treat that case as best-effort: that one IP
// just gets no score.
func (c *Client) Score(ctx context.Context, ip string) (int, error) {
	if net.ParseIP(ip) == nil {
		return 0, fmt.Errorf("enrich: invalid IP address %q", ip)
	}
	if score, ok := c.cache[ip]; ok {
		return score, nil
	}

	if err := c.limit.Wait(ctx); err != nil {
		return 0, fmt.Errorf("enrich: rate limiter: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	query := url.Values{}
	query.Set("ipAddress", ip)
	query.Set("maxAgeInDays", "90")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CheckURL+"?"+query.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("enrich: building AbuseIPDB request for %s: %w", ip, err)
	}
	req.Header.Set("Key", c.key)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("enrich: querying AbuseIPDB for %s: %w", ip, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return 0, fmt.Errorf("enrich: %w (status %d): check YATT_ABUSEIPDB_KEY", ErrInvalidKey, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("enrich: AbuseIPDB returned status %d for %s", resp.StatusCode, ip)
	}

	var body checkResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("enrich: decoding AbuseIPDB response for %s: %w", ip, err)
	}

	c.cache[ip] = body.Data.AbuseConfidenceScore
	return body.Data.AbuseConfidenceScore, nil
}
