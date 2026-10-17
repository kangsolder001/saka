package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/config"
)

const validDBURL = "postgres://saka:saka@localhost:5433/saka?sslmode=disable"

func TestLoadFrom_Defaults(t *testing.T) {
	cfg, err := config.LoadFrom(map[string]string{"DATABASE_URL": validDBURL})

	require.NoError(t, err)
	assert.Equal(t, config.EnvDevelopment, cfg.AppEnv)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, 15*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, int32(10), cfg.DBMaxConns)
	assert.Empty(t, cfg.CORSOrigins)
	assert.False(t, cfg.IsProduction())
}

func TestLoadFrom_Overrides(t *testing.T) {
	cfg, err := config.LoadFrom(map[string]string{
		"APP_ENV":              "production",
		"HTTP_ADDR":            ":9000",
		"SHUTDOWN_TIMEOUT":     "30s",
		"LOG_LEVEL":            "debug",
		"CORS_ALLOWED_ORIGINS": "https://a.example,https://b.example",
		"DATABASE_URL":         validDBURL,
		"DB_MAX_CONNS":         "25",
		"DB_MIN_CONNS":         "2",
	})

	require.NoError(t, err)
	assert.True(t, cfg.IsProduction())
	assert.Equal(t, ":9000", cfg.HTTPAddr)
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, []string{"https://a.example", "https://b.example"}, cfg.CORSOrigins)
	assert.Equal(t, int32(25), cfg.DBMaxConns)
	assert.Equal(t, int32(2), cfg.DBMinConns)
}

func TestLoadFrom_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		environ map[string]string
		wantMsg string
	}{
		{"missing database url", map[string]string{}, "DATABASE_URL is required"},
		{"non postgres url", map[string]string{"DATABASE_URL": "mysql://x"}, "must be a postgres:// URL"},
		{"unknown env", map[string]string{"DATABASE_URL": validDBURL, "APP_ENV": "staging"}, "APP_ENV must be one of"},
		{"unknown log level", map[string]string{"DATABASE_URL": validDBURL, "LOG_LEVEL": "trace"}, "LOG_LEVEL must be one of"},
		{"min conns above max", map[string]string{"DATABASE_URL": validDBURL, "DB_MAX_CONNS": "2", "DB_MIN_CONNS": "5"}, "DB_MIN_CONNS"},
		{"wildcard cors in production", map[string]string{"DATABASE_URL": validDBURL, "APP_ENV": "production", "CORS_ALLOWED_ORIGINS": "*"}, "must not contain *"},
		{"bad duration", map[string]string{"DATABASE_URL": validDBURL, "SHUTDOWN_TIMEOUT": "soon"}, "invalid duration"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadFrom(tt.environ)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidate_ReportsAllProblems(t *testing.T) {
	_, err := config.LoadFrom(map[string]string{"APP_ENV": "staging", "LOG_LEVEL": "trace"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "APP_ENV")
	assert.Contains(t, err.Error(), "LOG_LEVEL")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}
