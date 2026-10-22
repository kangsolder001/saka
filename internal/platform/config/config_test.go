package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/config"
)

const validDBURL = "postgres://saka:saka@localhost:5433/saka?sslmode=disable"

// minimal is the smallest valid file: only the required database url.
const validSecret = "0123456789abcdef0123456789abcdef-test"

const minimal = `{"database": {"url": "` + validDBURL + `"}, "auth": {"jwt_secret": "` + validSecret + `"}}`

func TestParse_MinimalFileGetsDefaults(t *testing.T) {
	cfg, err := config.Parse([]byte(minimal))

	require.NoError(t, err)
	assert.Equal(t, "Saka", cfg.App.Name)
	assert.Equal(t, config.EnvDevelopment, cfg.App.Env)
	assert.Equal(t, ":8080", cfg.App.HTTPAddr)
	assert.Equal(t, 15*time.Second, cfg.App.ShutdownTimeout.Std())
	assert.Equal(t, "info", cfg.App.LogLevel)
	assert.Empty(t, cfg.App.CORSAllowedOrigins)
	assert.Equal(t, "http://localhost:3000", cfg.App.FrontendURL)
	assert.Equal(t, 15*time.Minute, cfg.Auth.AccessTokenTTL.Std())
	assert.Equal(t, 30*24*time.Hour, cfg.Auth.RefreshTokenTTL.Std())
	assert.Equal(t, int32(10), cfg.Database.MaxConns)
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
	assert.Equal(t, "saka", cfg.Redis.KeyPrefix)
	assert.Equal(t, 5*time.Minute, cfg.Redis.UserCacheTTL.Std())
	assert.Equal(t, "log", cfg.Mail.Driver)
	assert.Equal(t, "localhost", cfg.Mail.SMTP.Host)
	assert.Equal(t, 1025, cfg.Mail.SMTP.Port)
	assert.Equal(t, "fake", cfg.Billing.Provider)
	assert.Equal(t, 7, cfg.Billing.InvoiceDueDays)
	assert.Equal(t, 3, cfg.Billing.RenewalLeadDays)
	assert.Equal(t, 3, cfg.Billing.GraceDays)
	assert.Equal(t, time.Hour, cfg.Billing.HourlyJobsInterval.Std())
	assert.Equal(t, 24*time.Hour, cfg.Billing.DailyJobsInterval.Std())
	assert.Equal(t, time.Hour, cfg.Billing.ReconcileMinAge.Std())
	assert.Equal(t, "https://app.sandbox.midtrans.com", cfg.Billing.Midtrans.SnapBaseURL)
	assert.Equal(t, "https://api.sandbox.midtrans.com", cfg.Billing.Midtrans.APIBaseURL)
	assert.Empty(t, cfg.Billing.Midtrans.ServerKey)
	assert.Equal(t, 4, cfg.Worker.Concurrency)
	assert.Equal(t, 5*time.Second, cfg.Worker.PollInterval.Std())
	assert.Equal(t, time.Minute, cfg.Worker.JobTimeout.Std())
	assert.Equal(t, 5*time.Minute, cfg.Worker.Lease.Std())
	assert.False(t, cfg.IsProduction())
}

func TestParse_OverridesAndKeepsUntouchedDefaults(t *testing.T) {
	cfg, err := config.Parse([]byte(`{
		"app": {"env": "production", "http_addr": ":9000", "shutdown_timeout": "30s", "frontend_url": "https://app.example.com",
		        "cors_allowed_origins": ["https://a.example", "https://b.example"],
		        "trusted_proxies": ["10.0.0.0/8"]},
		"database": {"url": "` + validDBURL + `", "max_conns": 25, "min_conns": 2},
		"auth": {"jwt_secret": "` + validSecret + `", "access_token_ttl": "10m", "refresh_token_ttl": "240h"},
		"mail": {"driver": "smtp", "from": "Saka <noreply@example.com>",
		         "smtp": {"host": "smtp.example.com", "port": 587, "username": "u", "password": "p"}},
		"redis": {"addr": "redis.internal:6380", "password": "pw", "db": 3, "key_prefix": "acme-prod", "user_cache_ttl": "90s"},
		"worker": {"concurrency": 8, "lease": "10m"},
		"billing": {"provider": "midtrans", "grace_days": 5, "midtrans": {"server_key": "Mid-server-abc", "snap_base_url": "https://app.midtrans.com", "api_base_url": "https://api.midtrans.com"}}
	}`))

	require.NoError(t, err)
	assert.True(t, cfg.IsProduction())
	assert.Equal(t, ":9000", cfg.App.HTTPAddr)
	assert.Equal(t, 30*time.Second, cfg.App.ShutdownTimeout.Std())
	assert.Equal(t, []string{"https://a.example", "https://b.example"}, cfg.App.CORSAllowedOrigins)
	assert.Equal(t, []string{"10.0.0.0/8"}, cfg.App.TrustedProxies)
	assert.Equal(t, int32(25), cfg.Database.MaxConns)
	assert.Equal(t, int32(2), cfg.Database.MinConns)
	assert.Equal(t, "https://app.example.com", cfg.App.FrontendURL)
	assert.Equal(t, 10*time.Minute, cfg.Auth.AccessTokenTTL.Std())
	assert.Equal(t, 240*time.Hour, cfg.Auth.RefreshTokenTTL.Std())
	assert.Equal(t, "smtp.example.com", cfg.Mail.SMTP.Host)
	assert.Equal(t, 587, cfg.Mail.SMTP.Port)
	assert.Equal(t, "redis.internal:6380", cfg.Redis.Addr)
	assert.Equal(t, 3, cfg.Redis.DB)
	assert.Equal(t, "acme-prod", cfg.Redis.KeyPrefix)
	assert.Equal(t, 90*time.Second, cfg.Redis.UserCacheTTL.Std())
	assert.Equal(t, 8, cfg.Worker.Concurrency)
	assert.Equal(t, 10*time.Minute, cfg.Worker.Lease.Std())
	assert.Equal(t, "midtrans", cfg.Billing.Provider)
	assert.Equal(t, 5, cfg.Billing.GraceDays)
	assert.Equal(t, 3, cfg.Billing.RenewalLeadDays, "keys not in the section keep their default")
	assert.Equal(t, "Mid-server-abc", cfg.Billing.Midtrans.ServerKey)
	assert.Equal(t, 15*time.Second, cfg.Billing.Midtrans.RequestTimeout.Std())
	assert.Equal(t, "Saka", cfg.App.Name, "keys not in the file keep their default")
	assert.Equal(t, time.Minute, cfg.Worker.JobTimeout.Std(), "partial sections keep sibling defaults")
}

func TestParse_RejectsBadJSON(t *testing.T) {
	tests := map[string]struct {
		input   string
		wantMsg string
	}{
		"empty file":         {``, "parse json"},
		"not json":           {`hello`, "parse json"},
		"unknown top key":    {`{"databse": {"url": "` + validDBURL + `"}}`, "unknown field"},
		"unknown nested key": {`{"database": {"url": "` + validDBURL + `", "pool": 3}}`, "unknown field"},
		"wrong type":         {`{"database": {"url": "` + validDBURL + `", "max_conns": "ten"}}`, "parse json"},
		"duration as number": {`{"database": {"url": "` + validDBURL + `"}, "app": {"shutdown_timeout": 15}}`, "duration must be a string"},
		"bad duration":       {`{"database": {"url": "` + validDBURL + `"}, "app": {"shutdown_timeout": "soon"}}`, "invalid duration"},
		"trailing data":      {minimal + ` {}`, "unexpected data"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(tt.input))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestParse_Validation(t *testing.T) {
	with := func(app, db, mail, worker string, redis ...string) string {
		if db == "" {
			db = `{"url": "` + validDBURL + `"}`
		}
		field := func(name, v string) string {
			if v == "" {
				return ""
			}
			return `"` + name + `": ` + v + `,`
		}
		if len(redis) > 0 {
			return `{` + field("app", app) + field("mail", mail) + field("worker", worker) + field("redis", redis[0]) +
				`"auth": {"jwt_secret": "` + validSecret + `"}, "database": ` + db + `}`
		}
		return `{` + field("app", app) + field("mail", mail) + field("worker", worker) +
			`"auth": {"jwt_secret": "` + validSecret + `"}, "database": ` + db + `}`
	}
	billing := func(section string) string {
		return `{"billing": ` + section + `, "auth": {"jwt_secret": "` + validSecret + `"}, "database": {"url": "` + validDBURL + `"}}`
	}
	billingInProduction := func(section string) string {
		return `{"app": {"env": "production"}, "mail": {"driver": "smtp"}, "billing": ` + section +
			`, "auth": {"jwt_secret": "` + validSecret + `"}, "database": {"url": "` + validDBURL + `"}}`
	}
	tests := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"missing database url", `{}`, "database.url is required"},
		{"non postgres url", with("", `{"url": "mysql://x"}`, "", ""), "database.url must be a postgres:// URL"},
		{"unknown env", with(`{"env": "staging"}`, "", "", ""), "app.env must be one of"},
		{"blank app name", with(`{"name": " "}`, "", "", ""), "app.name must not be empty"},
		{"unknown log level", with(`{"log_level": "trace"}`, "", "", ""), "app.log_level must be one of"},
		{"zero shutdown timeout", with(`{"shutdown_timeout": "0s"}`, "", "", ""), "app.shutdown_timeout must be positive"},
		{"min conns above max", with("", `{"url": "`+validDBURL+`", "max_conns": 2, "min_conns": 5}`, "", ""), "database.min_conns"},
		{"app role with uppercase", with("", `{"url": "`+validDBURL+`", "app_role": "Saka App"}`, "", ""), "database.app_role"},
		{"no app role in production", with(`{"env": "production"}`, `{"url": "`+validDBURL+`", "app_role": ""}`, `{"driver": "smtp"}`, ""), "database.app_role must be set in production"},
		{"zero max conns", with("", `{"url": "`+validDBURL+`", "max_conns": 0}`, "", ""), "database.max_conns"},
		{"wildcard cors in development", with(`{"cors_allowed_origins": ["*"]}`, "", "", ""), "must not contain *"},
		{"wildcard cors in production", with(`{"env": "production", "cors_allowed_origins": ["*"]}`, "", `{"driver": "smtp"}`, ""), "must not contain *"},
		{"missing jwt secret", `{"database": {"url": "` + validDBURL + `"}}`, "auth.jwt_secret is required"},
		{"short jwt secret", `{"database": {"url": "` + validDBURL + `"}, "auth": {"jwt_secret": "short"}}`, "at least 32 characters"},
		{"placeholder jwt secret in production", `{"app": {"env": "production"}, "mail": {"driver": "smtp"}, "database": {"url": "` + validDBURL + `"}, "auth": {"jwt_secret": "change-me-to-a-random-string-of-at-least-32-chars"}}`, "example placeholder"},
		{"access ttl not below refresh ttl", `{"database": {"url": "` + validDBURL + `"}, "auth": {"jwt_secret": "` + validSecret + `", "access_token_ttl": "48h", "refresh_token_ttl": "24h"}}`, "access_token_ttl must be shorter"},
		{"zero token ttl", `{"database": {"url": "` + validDBURL + `"}, "auth": {"jwt_secret": "` + validSecret + `", "access_token_ttl": "0s"}}`, "must be positive"},
		{"bad frontend url", with(`{"frontend_url": "not a url"}`, "", "", ""), "app.frontend_url"},
		{"redis addr without port", with("", "", "", "", `{"addr": "localhost"}`), "redis.addr"},
		{"redis addr empty", with("", "", "", "", `{"addr": ""}`), "redis.addr"},
		{"redis db out of range", with("", "", "", "", `{"db": 16}`), "redis.db"},
		{"redis prefix with colon", with("", "", "", "", `{"key_prefix": "a:b"}`), "redis.key_prefix"},
		{"redis prefix uppercase", with("", "", "", "", `{"key_prefix": "Saka"}`), "redis.key_prefix"},
		{"redis prefix empty", with("", "", "", "", `{"key_prefix": ""}`), "redis.key_prefix"},
		{"redis cache ttl zero", with("", "", "", "", `{"user_cache_ttl": "0s"}`), "redis.user_cache_ttl"},
		{"redis cache ttl too long", with("", "", "", "", `{"user_cache_ttl": "48h"}`), "redis.user_cache_ttl"},
		{"unknown mail driver", with("", "", `{"driver": "pigeon"}`, ""), "mail.driver must be one of"},
		{"log mailer in production", with(`{"env": "production"}`, "", `{"driver": "log"}`, ""), "not allowed in production"},
		{"resend without api key", with("", "", `{"driver": "resend"}`, ""), "mail.resend.api_key is required"},
		{"resend bad base url", with("", "", `{"driver": "resend", "resend": {"api_key": "re_abc", "base_url": "api.resend.com"}}`, ""), "mail.resend.base_url"},
		{"resend zero timeout", with("", "", `{"driver": "resend", "resend": {"api_key": "re_abc", "timeout": "0s"}}`, ""), "mail.resend.timeout"},
		{"resend bad from", with("", "", `{"driver": "resend", "from": "nope", "resend": {"api_key": "re_abc"}}`, ""), "mail.from"},
		{"resend placeholder key in production", with(`{"env": "production"}`, "", `{"driver": "resend", "resend": {"api_key": "re_change-me"}}`, ""), "example placeholder"},
		{"resend over http in production", with(`{"env": "production"}`, "", `{"driver": "resend", "resend": {"api_key": "re_abc", "base_url": "http://api.resend.com"}}`, ""), "must use https in production"},
		{"smtp without host", with("", "", `{"driver": "smtp", "smtp": {"host": " "}}`, ""), "mail.smtp.host is required"},
		{"smtp bad port", with("", "", `{"driver": "smtp", "smtp": {"port": 70000}}`, ""), "mail.smtp.port"},
		{"smtp bad from", with("", "", `{"driver": "smtp", "from": "nope"}`, ""), "mail.from"},
		{"zero worker concurrency", with("", "", "", `{"concurrency": 0}`), "worker.concurrency"},
		{"lease not above timeout", with("", "", "", `{"lease": "30s", "job_timeout": "1m"}`), "worker.lease must be longer"},
		{"unknown billing provider", billing(`{"provider": "stripe"}`), "billing.provider must be one of"},
		{"midtrans without a server key", billing(`{"provider": "midtrans"}`), "billing.midtrans.server_key is required"},
		{"fake provider in production", billingInProduction(`{"provider": "fake"}`), `billing.provider "fake" is not allowed in production`},
		{"sandbox host in production", billingInProduction(`{"provider": "midtrans", "midtrans": {"server_key": "Mid-server-abc"}}`), "points at the Midtrans sandbox"},
		{"sandbox key in production", billingInProduction(`{"provider": "midtrans", "midtrans": {"server_key": "SB-Mid-server-abc", "snap_base_url": "https://app.midtrans.com", "api_base_url": "https://api.midtrans.com"}}`), "sandbox key"},
		{"placeholder key in production", billingInProduction(`{"provider": "midtrans", "midtrans": {"server_key": "Mid-server-change-me", "snap_base_url": "https://app.midtrans.com", "api_base_url": "https://api.midtrans.com"}}`), "example placeholder"},
		{"invoice due days zero", billing(`{"invoice_due_days": 0}`), "billing.invoice_due_days"},
		{"renewal lead too long", billing(`{"renewal_lead_days": 31}`), "billing.renewal_lead_days"},
		{"negative grace", billing(`{"grace_days": -1}`), "billing.grace_days"},
		{"zero job interval", billing(`{"hourly_jobs_interval": "0s"}`), "billing.hourly_jobs_interval"},
		{"negative reconcile age", billing(`{"reconcile_min_age": "-1m"}`), "billing.reconcile_min_age"},
		{"bad midtrans url", billing(`{"midtrans": {"snap_base_url": "app.midtrans.com"}}`), "billing.midtrans.snap_base_url"},
		{"zero midtrans timeout", billing(`{"midtrans": {"request_timeout": "0s"}}`), "billing.midtrans.request_timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Parse([]byte(tt.input))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestParse_ReportsAllProblemsAtOnce(t *testing.T) {
	_, err := config.Parse([]byte(`{"app": {"env": "staging", "log_level": "trace"}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "app.env")
	assert.Contains(t, err.Error(), "app.log_level")
	assert.Contains(t, err.Error(), "database.url")
}

func TestParse_NeverEchoesJWTSecret(t *testing.T) {
	_, err := config.Parse([]byte(`{"database": {"url": "mysql://x"}, "auth": {"jwt_secret": "topsecretvalue"}}`))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecretvalue")
}

func TestParse_NeverEchoesDatabaseCredentials(t *testing.T) {
	_, err := config.Parse([]byte(`{"database": {"url": "mysql://admin:s3cretpass@host/db"}}`))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cretpass")
}

func TestDefault_IsValidOnceRequiredSecretsAreSet(t *testing.T) {
	cfg := config.Default()
	assert.Error(t, cfg.Validate(), "database.url and auth.jwt_secret have no default")

	cfg.Database.URL = validDBURL
	cfg.Auth.JWTSecret = validSecret
	assert.NoError(t, cfg.Validate())
}

func TestLoad(t *testing.T) {
	t.Run("reads a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(minimal), 0o600))

		cfg, err := config.Load(path)

		require.NoError(t, err)
		assert.Equal(t, validDBURL, cfg.Database.URL)
	})

	t.Run("missing file explains how to create it", func(t *testing.T) {
		_, err := config.Load(filepath.Join(t.TempDir(), "nope.json"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "config.example.json")
	})

	t.Run("errors mention the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.json")
		require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))

		_, err := config.Load(path)

		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "bad.json"))
	})
}

// The example file is what people copy; it must always be a valid config.
func TestExampleFileIsValid(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "..", "config.example.json"))

	require.NoError(t, err)
	assert.Equal(t, "smtp", cfg.Mail.Driver)
	assert.Equal(t, "fake", cfg.Billing.Provider, "the example runs without a payment account")
}

func TestParse_NeverEchoesTheMidtransServerKey(t *testing.T) {
	_, err := config.Parse([]byte(`{"app": {"env": "production"}, "mail": {"driver": "smtp"}, "database": {"url": "` + validDBURL + `"},
		"auth": {"jwt_secret": "` + validSecret + `"}, "billing": {"provider": "midtrans", "grace_days": 99, "midtrans": {"server_key": "SB-Mid-server-topsecret"}}}`))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestParse_AProductionConfigWithRealMidtransSettingsIsValid(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"app": {"env": "production"}, "mail": {"driver": "smtp"}, "database": {"url": "` + validDBURL + `"},
		"auth": {"jwt_secret": "` + validSecret + `"}, "billing": {"provider": "midtrans", "midtrans": {"server_key": "Mid-server-abc",
		"snap_base_url": "https://app.midtrans.com", "api_base_url": "https://api.midtrans.com"}}}`))

	require.NoError(t, err)
	assert.Equal(t, "midtrans", cfg.Billing.Provider)
}

func TestParse_TheApplicationRoleDefaultsToSakaApp(t *testing.T) {
	cfg, err := config.Parse([]byte(minimal))

	require.NoError(t, err)
	assert.Equal(t, "saka_app", cfg.Database.AppRole)
}

func TestParse_TheApplicationRoleMayBeDisabledOutsideProduction(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"database": {"url": "` + validDBURL + `", "app_role": ""}, "auth": {"jwt_secret": "` + validSecret + `"}}`))

	require.NoError(t, err)
	assert.Empty(t, cfg.Database.AppRole)
}

func TestParse_TheResendDriver(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"app": {"env": "production"}, "database": {"url": "` + validDBURL + `"},
		"auth": {"jwt_secret": "` + validSecret + `"},
		"mail": {"driver": "resend", "from": "Saka <noreply@example.com>", "resend": {"api_key": "re_realKey123"}},
		"billing": {"provider": "midtrans", "midtrans": {"server_key": "Mid-server-abc",
			"snap_base_url": "https://app.midtrans.com", "api_base_url": "https://api.midtrans.com"}}}`))

	require.NoError(t, err, "resend is accepted in production")
	assert.Equal(t, "resend", cfg.Mail.Driver)
	assert.Equal(t, "re_realKey123", cfg.Mail.Resend.APIKey)
	assert.Equal(t, "https://api.resend.com", cfg.Mail.Resend.BaseURL, "the base URL defaults to the real API")
	assert.Equal(t, 15*time.Second, cfg.Mail.Resend.Timeout.Std())
}

func TestParse_NeverEchoesTheResendAPIKey(t *testing.T) {
	_, err := config.Parse([]byte(`{"app": {"env": "production"}, "database": {"url": "` + validDBURL + `"}, "auth": {"jwt_secret": "` + validSecret + `"},
		"mail": {"driver": "resend", "from": "nope", "resend": {"api_key": "re_topsecretkey", "base_url": "http://x.example", "timeout": "0s"}}}`))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret")
}
