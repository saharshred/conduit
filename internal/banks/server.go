// Package banks implements mock "bank" HTTP APIs, each with a different
// failure personality. This is the actual shape of Plaid's core problem:
// every real bank integration has some combination of flaky auth, rate
// limits, and pagination that doesn't quite behave — the normalizer has to
// be built assuming all three, not the happy path.
package banks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config controls which quirks a given mock bank exhibits. Real bank
// integrations rarely have exactly one problem — but SPREAD's three mock
// banks are each tuned to make one quirk dominant so it's obvious in logs
// and tests which failure mode is being exercised.
type Config struct {
	Name string

	// TokenTTLRequests: a bearer token issued by POST /auth stops working
	// after this many requests to /transactions (0 = tokens never expire).
	TokenTTLRequests int

	// RateLimitPerSecond: max requests accepted per second across all
	// callers (0 = unlimited). Exceeding it returns 429.
	RateLimitPerSecond int

	// PaginationQuirk: if true, every 4th page re-serves the last item of
	// the previous page instead of advancing cleanly — simulating the
	// at-least-once, occasionally-overlapping pagination real bank APIs
	// are notorious for. The normalizer must dedupe on native ID to
	// survive this cleanly.
	PaginationQuirk bool

	PageSize int
}

// NativeTxn is intentionally a raw map: each mock bank uses its own field
// names, which is the point — the normalizer has to map three different
// native shapes onto schema.Transaction.
type NativeTxn = map[string]any

type Server struct {
	cfg          Config
	transactions []NativeTxn

	mu           sync.Mutex
	tokens       map[string]int // token -> requests remaining
	requestTimes []time.Time    // sliding window for rate limiting
}

func NewServer(cfg Config, transactions []NativeTxn) *Server {
	if cfg.PageSize == 0 {
		cfg.PageSize = 25
	}
	return &Server{
		cfg:          cfg,
		transactions: transactions,
		tokens:       map[string]int{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", s.handleAuth)
	mux.HandleFunc("/transactions", s.handleTransactions)
	return mux
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := fmt.Sprintf("tok_%s_%d", s.cfg.Name, time.Now().UnixNano())
	ttl := s.cfg.TokenTTLRequests
	if ttl == 0 {
		ttl = 1 << 30 // effectively unlimited
	}
	s.tokens[token] = ttl

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if !s.allowRequest() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	token := bearerToken(r)
	if !s.consumeToken(token) {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	offset := s.decodeCursor(r.URL.Query().Get("cursor"))
	page, nextOffset := s.page(offset)

	var next string
	if nextOffset < len(s.transactions) {
		next = s.encodeCursor(nextOffset)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"transactions": page,
		"next_cursor":  next,
	})
}

func (s *Server) page(offset int) (page []NativeTxn, nextOffset int) {
	end := offset + s.cfg.PageSize
	if end > len(s.transactions) {
		end = len(s.transactions)
	}
	if offset > len(s.transactions) {
		offset = len(s.transactions)
	}
	page = append(page, s.transactions[offset:end]...)
	nextOffset = end

	// The pagination quirk: every 4th page starts one record *before*
	// where it should, re-serving the previous page's last record. A
	// client that doesn't dedupe by native ID will double-count it.
	s.mu.Lock()
	pageNum := offset / s.cfg.PageSize
	quirk := s.cfg.PaginationQuirk && pageNum > 0 && pageNum%4 == 0 && offset > 0
	s.mu.Unlock()
	if quirk {
		page = append([]NativeTxn{s.transactions[offset-1]}, page...)
	}
	return page, nextOffset
}

func (s *Server) allowRequest() bool {
	if s.cfg.RateLimitPerSecond == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Second)
	kept := s.requestTimes[:0]
	for _, t := range s.requestTimes {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.requestTimes = kept

	if len(s.requestTimes) >= s.cfg.RateLimitPerSecond {
		return false
	}
	s.requestTimes = append(s.requestTimes, now)
	return true
}

func (s *Server) consumeToken(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	remaining, ok := s.tokens[token]
	if !ok || remaining <= 0 {
		return false
	}
	s.tokens[token] = remaining - 1
	return true
}

func (s *Server) encodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func (s *Server) decodeCursor(raw string) int {
	if raw == "" {
		return 0
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return 0
	}
	return n
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	return strings.TrimPrefix(h, "Bearer ")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
