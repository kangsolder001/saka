// Package loadtest is a small, dependency-free load generator for a running
// API (cmd/loadtest). It measures the endpoints whose cost matters most:
// registration and login (Argon2id), the payment webhook (signature check and
// a database write) and a cheap public read as the baseline of the HTTP stack.
//
// It is a measuring stick, not a benchmark suite: numbers depend on the
// hardware, on the config of the API under test and on what else the machine
// runs, so compare runs on the same machine.
package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kangsolder001/saka/internal/billing/payment"
	"github.com/kangsolder001/saka/internal/billing/payment/fake"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/safego"
)

// Scenario names.
const (
	ScenarioPlans    = "plans"
	ScenarioRegister = "register"
	ScenarioLogin    = "login"
	ScenarioWebhook  = "webhook"
	// ScenarioAll runs every scenario one after the other.
	ScenarioAll = "all"
)

// Scenarios lists the scenarios ScenarioAll runs, in order.
var Scenarios = []string{ScenarioPlans, ScenarioRegister, ScenarioLogin, ScenarioWebhook}

const password = "load-test-password-1"

// Config is one load test run.
type Config struct {
	// BaseURL is the API root without a trailing slash, e.g. http://localhost:8080.
	BaseURL  string
	Scenario string
	// Concurrency is the number of parallel clients.
	Concurrency int
	// Requests is how many requests each scenario sends.
	Requests int
	// Users is how many accounts the login scenario registers first and then
	// logs in as.
	Users int
	// SpoofIP gives every request its own client address in X-Forwarded-For.
	// The API rate limits per IP (spec §5.4), so one address would only measure
	// the limiter; it needs app.trusted_proxies to contain the address this tool
	// connects from (127.0.0.1 locally), otherwise the header is ignored.
	SpoofIP bool
	// HTTPClient replaces the default client (tests).
	HTTPClient *http.Client
	Clock      clock.Clock
}

// Report is the outcome of one scenario.
type Report struct {
	Scenario string
	Requests int
	// Failed counts transport errors and statuses other than the one the
	// scenario expects when the API is healthy and not rate limiting.
	Failed   int
	Statuses map[int]int
	Elapsed  time.Duration
	RPS      float64
	Mean     time.Duration
	P50      time.Duration
	P95      time.Duration
	P99      time.Duration
	Max      time.Duration
}

// String formats the report as one readable block.
func (r Report) String() string {
	codes := make([]int, 0, len(r.Statuses))
	for code := range r.Statuses {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	var statuses []string
	for _, code := range codes {
		statuses = append(statuses, fmt.Sprintf("%d x%d", code, r.Statuses[code]))
	}
	if len(statuses) == 0 {
		statuses = []string{"none"}
	}
	return fmt.Sprintf("%-9s requests=%d failed=%d elapsed=%s rps=%.1f latency mean=%s p50=%s p95=%s p99=%s max=%s statuses: %s",
		r.Scenario, r.Requests, r.Failed, r.Elapsed.Round(time.Millisecond), r.RPS,
		r.Mean.Round(time.Microsecond), r.P50.Round(time.Microsecond), r.P95.Round(time.Microsecond),
		r.P99.Round(time.Microsecond), r.Max.Round(time.Microsecond), strings.Join(statuses, ", "))
}

// Run executes the configured scenario (or all of them) and returns one report each.
func Run(ctx context.Context, cfg Config) ([]Report, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	names := []string{cfg.Scenario}
	if cfg.Scenario == ScenarioAll {
		names = Scenarios
	}
	var reports []Report
	for _, name := range names {
		report, err := runScenario(ctx, cfg, name)
		if err != nil {
			return reports, fmt.Errorf("scenario %s: %w", name, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (c *Config) normalize() error {
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.BaseURL == "" {
		return errors.New("base URL is required")
	}
	switch c.Scenario {
	case ScenarioAll, ScenarioPlans, ScenarioRegister, ScenarioLogin, ScenarioWebhook:
	default:
		return fmt.Errorf("unknown scenario %q (want %s, or %s)", c.Scenario, strings.Join(Scenarios, ", "), ScenarioAll)
	}
	if c.Concurrency < 1 || c.Requests < 1 {
		return errors.New("concurrency and requests must be at least 1")
	}
	if c.Users < 1 {
		c.Users = c.Concurrency
	}
	if c.HTTPClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConnsPerHost = c.Concurrency
		c.HTTPClient = &http.Client{Transport: transport, Timeout: 60 * time.Second}
	}
	if c.Clock == nil {
		c.Clock = clock.Real{}
	}
	return nil
}

// request is what a scenario asks the driver to send for the n-th request.
type request struct {
	method string
	path   string
	body   []byte
	header http.Header
	want   int
}

type scenario struct {
	// prepare runs once before the measurement (for example to create the accounts to log in as).
	prepare func(ctx context.Context, d *driver) error
	build   func(d *driver, n int) request
}

type driver struct {
	cfg    Config
	runID  string
	runner *safego.Runner
	users  []string
}

func runScenario(ctx context.Context, cfg Config, name string) (Report, error) {
	d := &driver{
		cfg:    cfg,
		runID:  fmt.Sprintf("%x", cfg.Clock.Now().UnixNano()&0xFFFFFFFF),
		runner: safego.NewRunner(logger.Nop()),
	}
	sc := scenarios[name]
	if sc.prepare != nil {
		if err := sc.prepare(ctx, d); err != nil {
			return Report{}, fmt.Errorf("prepare: %w", err)
		}
	}
	return d.measure(ctx, name, sc.build)
}

// measure sends cfg.Requests requests with cfg.Concurrency clients and times each.
func (d *driver) measure(ctx context.Context, name string, build func(*driver, int) request) (Report, error) {
	type sample struct {
		status int
		took   time.Duration
		failed bool
	}
	var next atomic.Int64
	perWorker := make([][]sample, d.cfg.Concurrency)
	group := d.runner.NewGroup(ctx, d.cfg.Concurrency)
	started := d.cfg.Clock.Now()
	for w := range d.cfg.Concurrency {
		group.Go(fmt.Sprintf("loadtest-%s-%d", name, w), func(ctx context.Context) error {
			for ctx.Err() == nil {
				n := int(next.Add(1)) - 1
				if n >= d.cfg.Requests {
					return nil
				}
				req := build(d, n)
				t0 := d.cfg.Clock.Now()
				status, err := d.send(ctx, req)
				perWorker[w] = append(perWorker[w], sample{
					status: status, took: d.cfg.Clock.Now().Sub(t0), failed: err != nil || status != req.want,
				})
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Report{}, err
	}
	elapsed := d.cfg.Clock.Now().Sub(started)
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}

	report := Report{Scenario: name, Statuses: map[int]int{}, Elapsed: elapsed}
	var all []time.Duration
	var total time.Duration
	for _, samples := range perWorker {
		for _, s := range samples {
			report.Requests++
			if s.failed {
				report.Failed++
			}
			if s.status != 0 {
				report.Statuses[s.status]++
			}
			all = append(all, s.took)
			total += s.took
		}
	}
	if report.Requests == 0 {
		return report, errors.New("no request was sent")
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	report.RPS = float64(report.Requests) / elapsed.Seconds()
	report.Mean = total / time.Duration(len(all))
	report.P50, report.P95, report.P99, report.Max = percentile(all, 50), percentile(all, 95), percentile(all, 99), all[len(all)-1]
	return report, nil
}

// percentile is the nearest-rank percentile of sorted durations.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

// send performs one request and returns its status. A non-nil error is a
// transport failure; the response body is always drained so connections are reused.
func (d *driver) send(ctx context.Context, r request) (int, error) {
	req, err := http.NewRequestWithContext(ctx, r.method, d.cfg.BaseURL+r.path, bytes.NewReader(r.body))
	if err != nil {
		return 0, err
	}
	for k, vs := range r.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if len(r.body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// clientIP returns a distinct private address for the n-th request of a run.
func (d *driver) clientIP(n int) string {
	if !d.cfg.SpoofIP {
		return ""
	}
	salt := 0
	for _, c := range d.runID {
		salt = salt*31 + int(c)
	}
	v := (n + salt) & 0xFFFFFF
	return fmt.Sprintf("10.%d.%d.%d", v>>16&0xFF, v>>8&0xFF, v&0xFF)
}

func (d *driver) header(n int) http.Header {
	h := http.Header{}
	if ip := d.clientIP(n); ip != "" {
		h.Set("X-Forwarded-For", ip)
	}
	return h
}

var scenarios = map[string]scenario{
	ScenarioPlans: {
		build: func(d *driver, n int) request {
			return request{method: http.MethodGet, path: "/api/v1/plans", header: d.header(n), want: http.StatusOK}
		},
	},
	ScenarioRegister: {
		build: func(d *driver, n int) request {
			return request{method: http.MethodPost, path: "/api/v1/auth/register", header: d.header(n), want: http.StatusCreated,
				body: registerBody(fmt.Sprintf("load-%s-r%d@example.com", d.runID, n))}
		},
	},
	ScenarioLogin: {
		prepare: func(ctx context.Context, d *driver) error {
			d.users = make([]string, d.cfg.Users)
			for i := range d.users {
				d.users[i] = fmt.Sprintf("load-%s-u%d@example.com", d.runID, i)
				status, err := d.send(ctx, request{
					method: http.MethodPost, path: "/api/v1/auth/register", header: d.header(1<<20 + i), body: registerBody(d.users[i]),
				})
				if err != nil {
					return err
				}
				if status != http.StatusCreated {
					return fmt.Errorf("registering %s answered %d, want 201 (is app.trusted_proxies set and the API on a fresh run?)", d.users[i], status)
				}
			}
			return nil
		},
		build: func(d *driver, n int) request {
			body, _ := json.Marshal(map[string]string{
				"email": d.users[n%len(d.users)], "password": password, "token_delivery": "body",
			})
			return request{method: http.MethodPost, path: "/api/v1/auth/login", header: d.header(n), body: body, want: http.StatusOK}
		},
	},
	ScenarioWebhook: {
		build: func(d *driver, n int) request {
			// A genuine, signed notification for an invoice nobody issued: it passes
			// the signature check, is stored and is answered 200 (spec §8.7).
			body, signed := fake.Webhook(payment.WebhookEvent{
				EventID: fmt.Sprintf("load-%s-%d", d.runID, n), InvoiceNumber: fmt.Sprintf("INV-LOAD-%s-%d", d.runID, n),
				ProviderRef: "load", State: payment.StatePaid, Amount: 99000, Currency: "IDR",
			})
			h := d.header(n)
			for k, vs := range signed {
				h[k] = vs
			}
			return request{method: http.MethodPost, path: "/api/v1/webhooks/fake", header: h, body: body, want: http.StatusOK}
		},
	},
}

func registerBody(email string) []byte {
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": password, "name": "Load Test", "organization_name": "Load Test Org",
	})
	return body
}
