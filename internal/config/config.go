// Package config reads runtime settings from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env             string        // "development" | "production"
	Addr            string        // listen address, e.g. ":8080"
	DatabaseURL     string        // postgres://user:pass@host:5432/db?sslmode=disable
	JWTSecret       string        // HMAC secret for access tokens
	AccessTokenTTL  time.Duration // default 1h
	RefreshTokenTTL time.Duration // default 30 days
	CORSOrigins     []string      // allowed browser origins; empty = allow all
	UploadDir       string        // where uploaded files are stored
	PublicURL       string        // base URL used to build links to uploads, e.g. https://api.example.com
	APIPrefix       string        // default /api/v1
	Timezone        string        // database session time zone, default Asia/Jakarta
	BodyLimitMB     int
}

func Load() Config {
	c := Config{
		Env:             env("APP_ENV", "development"),
		Addr:            env("APP_ADDR", ":8080"),
		DatabaseURL:     env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/lestari?sslmode=disable"),
		JWTSecret:       env("JWT_SECRET", "change-me-in-production"),
		AccessTokenTTL:  dur("ACCESS_TOKEN_TTL", time.Hour),
		RefreshTokenTTL: dur("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		UploadDir:       env("UPLOAD_DIR", "./storage/uploads"),
		PublicURL:       strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/"),
		APIPrefix:       env("API_PREFIX", "/api/v1"),
		Timezone:        env("APP_TIMEZONE", "Asia/Jakarta"),
		BodyLimitMB:     atoi(env("BODY_LIMIT_MB", "20"), 20),
	}
	if o := strings.TrimSpace(os.Getenv("CORS_ORIGINS")); o != "" {
		for _, s := range strings.Split(o, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.CORSOrigins = append(c.CORSOrigins, s)
			}
		}
	}
	return c
}

func (c Config) IsProduction() bool { return c.Env == "production" }

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func dur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
