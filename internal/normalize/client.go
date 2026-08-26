// Package normalize implements the client side of talking to a single mock
// bank: authenticating, paging through results, retrying on rate limits,
// re-authenticating on token expiry, deduping the pagination quirk's
// overlap, and mapping each bank's native fields onto schema.Transaction.
package normalize

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/saharshred/conduit/internal/banks"
	"github.com/saharshred/conduit/internal/schema"
)

// Mapper translates one bank's native transaction shape into the unified
// schema. Each bank gets its own — see mappers.go — because each one uses
// different field names for the same underlying data.
type Mapper func(native banks.NativeTxn) (schema.Transaction, error)

type Client struct {
	BankName   string
	BaseURL    string
	Mapper     Mapper
	HTTPClient *http.Client

	MaxRetries  int           // per request, for 429s
	BackoffBase time.Duration // exponential backoff base

	token string
}

func New(bankName, baseURL string, mapper Mapper) *Client {
	return &Client{
		BankName:   bankName,
		BaseURL:    baseURL,
		Mapper:     mapper,
		HTTPClient: http.DefaultClient,
		// Generous defaults: a real rate-limited bank needs to be outlasted
		// across multiple 1s sliding windows, not just one quick retry.
		MaxRetries:  20,
		BackoffBase: 50 * time.Millisecond,
	}
}

// FetchAll pages through the entire transaction history, surviving 401s
// (re-auth) and 429s (backoff + retry), and dedupes on native ID before
// mapping — the pagination quirk can hand back the same record twice, and
// that has to be caught here, before it ever reaches the fraud scorer.
func (c *Client) FetchAll(ctx context.Context) ([]schema.Transaction, error) {
	if err := c.authenticate(ctx); err != nil {
		return nil, fmt.Errorf("normalize[%s]: initial auth: %w", c.BankName, err)
	}

	seen := map[string]bool{}
	var out []schema.Transaction
	cursor := ""

	for {
		page, next, err := c.fetchPage(ctx, cursor)
		if err != nil {
			return nil, err
		}

		for _, native := range page {
			txn, err := c.Mapper(native)
			if err != nil {
				return nil, fmt.Errorf("normalize[%s]: mapping native record: %w", c.BankName, err)
			}
			if seen[txn.IdempotencyKey()] {
				continue // pagination overlap quirk — already have this one
			}
			seen[txn.IdempotencyKey()] = true
			out = append(out, txn)
		}

		if next == "" {
			break
		}
		cursor = next
	}

	return out, nil
}

func (c *Client) authenticate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/auth", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	c.token = body.Token
	return nil
}

func (c *Client) fetchPage(ctx context.Context, cursor string) (page []banks.NativeTxn, next string, err error) {
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		url := c.BaseURL + "/transactions"
		if cursor != "" {
			url += "?cursor=" + cursor
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, "", err
		}

		switch resp.StatusCode {
		case http.StatusOK:
			defer resp.Body.Close()
			var body struct {
				Transactions []banks.NativeTxn `json:"transactions"`
				NextCursor   string            `json:"next_cursor"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				return nil, "", err
			}
			return body.Transactions, body.NextCursor, nil

		case http.StatusUnauthorized:
			resp.Body.Close()
			// Token expired mid-run (the flaky-auth quirk) — re-auth once
			// and retry this same page, don't burn a rate-limit retry on it.
			if err := c.authenticate(ctx); err != nil {
				return nil, "", fmt.Errorf("normalize[%s]: re-auth after 401: %w", c.BankName, err)
			}
			continue

		case http.StatusTooManyRequests:
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if attempt == c.MaxRetries {
				return nil, "", fmt.Errorf("normalize[%s]: exceeded retries after repeated 429s", c.BankName)
			}
			sleep(c.backoff(attempt))
			continue

		default:
			resp.Body.Close()
			return nil, "", fmt.Errorf("normalize[%s]: unexpected status %d", c.BankName, resp.StatusCode)
		}
	}
	return nil, "", fmt.Errorf("normalize[%s]: exhausted retries fetching page", c.BankName)
}

const maxBackoff = 500 * time.Millisecond

// backoff is exponential with jitter: base * 2^attempt, +/- 20%, capped so
// a long retry sequence still makes forward progress against a sliding
// rate-limit window instead of ballooning unboundedly.
func (c *Client) backoff(attempt int) time.Duration {
	d := c.BackoffBase << attempt
	if d > maxBackoff || d <= 0 {
		d = maxBackoff
	}
	jitter := time.Duration(rand.Int63n(int64(d) / 5))
	return d + jitter
}

var sleep = time.Sleep
