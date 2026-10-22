// Command loadtest sends a light load at a running API and reports requests
// per second and latency percentiles for registration, login, the payment
// webhook and a cheap public read. Standard library only (plus the project's
// own fake gateway to sign webhooks).
//
//	loadtest [-base http://localhost:8080] [-scenario all] [-c 8] [-n 400] [-users 8] [-spoof-ip=true]
//
// The API under test must not be production: it creates accounts and webhook
// events. Run it against a development or throwaway database with the "fake"
// payment provider and, because of the per-IP rate limits, with
// app.trusted_proxies set to the address this tool connects from.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kangsolder001/saka/internal/loadtest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func run() error {
	base := flag.String("base", "http://localhost:8080", "base URL of the running API")
	scenario := flag.String("scenario", loadtest.ScenarioAll, "plans, register, login, webhook or all")
	concurrency := flag.Int("c", 8, "parallel clients")
	requests := flag.Int("n", 400, "requests per scenario")
	users := flag.Int("users", 0, "accounts the login scenario creates and logs in as (default: -c)")
	spoof := flag.Bool("spoof-ip", true, "send a distinct X-Forwarded-For per request (needs app.trusted_proxies)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("target %s, %d clients, %d requests per scenario\n", *base, *concurrency, *requests)
	reports, err := loadtest.Run(ctx, loadtest.Config{
		BaseURL: *base, Scenario: *scenario, Concurrency: *concurrency, Requests: *requests, Users: *users, SpoofIP: *spoof,
	})
	for _, r := range reports {
		fmt.Println(r)
	}
	return err
}
