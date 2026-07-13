// Package config loads application configuration from YAML and environment variables.
// Env vars use the prefix DADIARY_ and override file values (12-factor friendly).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"github.com/dadiary/backend/pkg/retry"
)

// Config is the root application configuration.
type Config struct {
	Env        string           `mapstructure:"env"`
	HTTP       HTTPConfig       `mapstructure:"http"`
	Database   DatabaseConfig   `mapstructure:"database"`
	JWT        JWTConfig        `mapstructure:"jwt"`
	Upload     UploadConfig     `mapstructure:"upload"`
	Storage    StorageConfig    `mapstructure:"storage"`
	OpenAI     OpenAIConfig     `mapstructure:"openai"`
	Anthropic  AnthropicConfig  `mapstructure:"anthropic"`
	Moderation ModerationConfig `mapstructure:"moderation"`
	Turnstile  TurnstileConfig  `mapstructure:"turnstile"`
	AI         AIConfig         `mapstructure:"ai"`
	// AdminEmails lists accounts allowed to call /admin/* endpoints.
	// Comma-separated via DADIARY_ADMIN_EMAILS.
	AdminEmails []string `mapstructure:"admin_emails"`
}

// AIConfig groups cross-cutting settings for outbound AI provider calls.
type AIConfig struct {
	// Retry controls exponential-backoff retries for transient AI failures
	// (network errors, HTTP 429 rate limits, HTTP 5xx). See pkg/retry.
	Retry retry.Config `mapstructure:"retry"`
}

// TurnstileConfig is Cloudflare Turnstile — used to reduce spam on POST /auth/register.
// When SecretKey is non-empty, registrations must include a valid turnstile_token.
type TurnstileConfig struct {
	SecretKey string `mapstructure:"secret_key"` // Widget secret key (server-only); DADIARY_TURNSTILE_SECRET_KEY
}

// UploadConfig controls local file storage for skin check-in photos.
type UploadConfig struct {
	Dir   string `mapstructure:"dir"`    // absolute or relative path for saved uploads (local driver)
	MaxMB int    `mapstructure:"max_mb"` // max size per file
}

// StorageConfig selects where uploaded photos are persisted.
//
// Driver is "local" (default; files under Upload.Dir) or "r2" (Cloudflare R2).
// Regardless of driver, public image URLs stay "/uploads/<key>"; the API proxies
// R2 bytes so the frontend and stored DB paths never change.
type StorageConfig struct {
	Driver string   `mapstructure:"driver"` // local | r2
	R2     R2Config `mapstructure:"r2"`
}

// R2Config holds Cloudflare R2 (S3-compatible) credentials and target bucket.
type R2Config struct {
	AccountID       string `mapstructure:"account_id"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	Bucket          string `mapstructure:"bucket"`
	// Endpoint optionally overrides the derived account endpoint
	// (https://<account_id>.r2.cloudflarestorage.com).
	Endpoint string `mapstructure:"endpoint"`
}

// OpenAIConfig holds API access for moderation, **vision** (skin photos), and optional legacy/fallback text calls.
type OpenAIConfig struct {
	APIKey string `mapstructure:"api_key"`
	// Model is used for: (1) legacy single-pass multimodal when Anthropic is OFF, (2) optional text calls (e.g. starter fallback).
	Model string `mapstructure:"model"`
	// VisionModel is the chat/vision model for the observation-only photo pass (e.g. gpt-4o, gpt-4o-mini).
	// When empty, Model is used, then default gpt-4o.
	VisionModel string `mapstructure:"vision_model"`
}

// AnthropicConfig is the **primary** text coach (daily feedback, routine suggest, starter routine).
// Vision stays on OpenAI. On Claude error/timeout, TextCoachCompletion falls back to OpenAI text model.
type AnthropicConfig struct {
	APIKey string `mapstructure:"api_key"`
	// Model — recommended Sonnet IDs (override via DADIARY_ANTHROPIC_MODEL):
	//   claude-sonnet-4-6 (default), claude-sonnet-4-20250514, claude-3-5-sonnet-20241022 (may 404 on some keys)
	Model string `mapstructure:"model"`
	// FastModel is the faster/cheaper model for the text-coach pass, set via
	// DADIARY_ANTHROPIC_FAST_MODEL. As of the latency push it is the OPERATIONAL
	// DEFAULT for coaching: the deployed .env ships "claude-haiku-4-5" (Claude
	// Haiku 4.5 — the accessible fast Haiku; classic claude-3-5-haiku-* 404s on our
	// current key). When set it REPLACES Model for coaching calls ONLY (vision stays
	// on OpenAI) — a deliberate speed/cost lever: Haiku is ~2x faster + much cheaper
	// than Sonnet, at the cost of some nuance in the warm, hyper-specific VN tone.
	// To revert the coach to Sonnet quality, delete/unset the env var: the resolver
	// (AnthropicCoachModel) then falls back to the Sonnet Model. Flip it on/off per
	// environment without any code change so the same build can be A/B'd live.
	FastModel string `mapstructure:"fast_model"`
}

// ModerationConfig toggles safety checks before persisting check-ins.
type ModerationConfig struct {
	// Skip disables OpenAI moderation calls (local dev only; do not use in production).
	Skip bool `mapstructure:"skip"`
}

// HTTPConfig holds HTTP server settings.
type HTTPConfig struct {
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	URL string `mapstructure:"url"`
}

// JWTConfig holds signing and TTL settings for access/refresh tokens.
type JWTConfig struct {
	Secret     string        `mapstructure:"secret"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

// Load reads config from optional .env (repo root), config.yaml, and DADIARY_* env vars.
func Load(relativeEnvPath string) (*Config, error) {
	// Optional; ignore missing files. Try CWD and repo root (when `go run` from backend/).
	_ = godotenv.Load(relativeEnvPath)
	_ = godotenv.Load("../.env")

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./backend")

	v.SetEnvPrefix("DADIARY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	// Explicit binds for common 12-factor names (clearer than nested env mapping).
	_ = v.BindEnv("database.url", "DADIARY_DATABASE_URL")
	_ = v.BindEnv("jwt.secret", "DADIARY_JWT_SECRET")
	_ = v.BindEnv("http.port", "DADIARY_HTTP_PORT")
	_ = v.BindEnv("http.read_timeout", "DADIARY_HTTP_READ_TIMEOUT")
	_ = v.BindEnv("http.write_timeout", "DADIARY_HTTP_WRITE_TIMEOUT")
	_ = v.BindEnv("openai.api_key", "DADIARY_OPENAI_API_KEY")
	_ = v.BindEnv("openai.model", "DADIARY_OPENAI_MODEL")
	_ = v.BindEnv("openai.vision_model", "DADIARY_OPENAI_VISION_MODEL")
	_ = v.BindEnv("anthropic.api_key", "DADIARY_ANTHROPIC_API_KEY")
	_ = v.BindEnv("anthropic.model", "DADIARY_ANTHROPIC_MODEL")
	// Explicitly bind the coach fast-model toggle so it is picked up reliably even
	// when it is absent from the config file (AutomaticEnv alone is unreliable for
	// nested keys with no default). This env var is the operational default for the
	// coach model (Haiku) — see AnthropicCoachModel / AnthropicConfig.FastModel.
	_ = v.BindEnv("anthropic.fast_model", "DADIARY_ANTHROPIC_FAST_MODEL")
	_ = v.BindEnv("moderation.skip", "DADIARY_MODERATION_SKIP")
	_ = v.BindEnv("ai.retry.max_retries", "DADIARY_AI_RETRY_MAX_RETRIES")
	_ = v.BindEnv("ai.retry.initial_delay", "DADIARY_AI_RETRY_INITIAL_DELAY")
	_ = v.BindEnv("ai.retry.max_delay", "DADIARY_AI_RETRY_MAX_DELAY")
	_ = v.BindEnv("ai.retry.backoff_multiplier", "DADIARY_AI_RETRY_BACKOFF_MULTIPLIER")
	_ = v.BindEnv("turnstile.secret_key", "DADIARY_TURNSTILE_SECRET_KEY")
	_ = v.BindEnv("upload.dir", "DADIARY_UPLOAD_DIR")
	_ = v.BindEnv("upload.max_mb", "DADIARY_UPLOAD_MAX_MB")
	_ = v.BindEnv("storage.driver", "DADIARY_STORAGE_DRIVER")
	_ = v.BindEnv("storage.r2.account_id", "DADIARY_R2_ACCOUNT_ID")
	_ = v.BindEnv("storage.r2.access_key_id", "DADIARY_R2_ACCESS_KEY_ID")
	_ = v.BindEnv("storage.r2.secret_access_key", "DADIARY_R2_SECRET_ACCESS_KEY")
	_ = v.BindEnv("storage.r2.bucket", "DADIARY_R2_BUCKET")
	_ = v.BindEnv("storage.r2.endpoint", "DADIARY_R2_ENDPOINT")
	_ = v.BindEnv("admin_emails", "DADIARY_ADMIN_EMAILS")

	if err := v.ReadInConfig(); err != nil {
		// Allow env-only mode if no yaml on disk
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.HTTP.Port == 0 {
		cfg.HTTP.Port = 8080
	}
	// Railway, Render, Fly, etc. inject PORT; prefer it over config file defaults.
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			cfg.HTTP.Port = parsed
		}
	}
	if cfg.HTTP.ReadTimeout == 0 {
		// Multipart photo uploads may take longer than default browser idle timeouts.
		cfg.HTTP.ReadTimeout = 2 * time.Minute
	}
	if cfg.HTTP.WriteTimeout == 0 {
		// Skin-check AI (vision + Claude) can take minutes — response is held until coach JSON is ready.
		cfg.HTTP.WriteTimeout = 12 * time.Minute
	}
	if cfg.JWT.AccessTTL == 0 {
		cfg.JWT.AccessTTL = 24 * time.Hour
	}
	if cfg.JWT.RefreshTTL == 0 {
		cfg.JWT.RefreshTTL = 7 * 24 * time.Hour
	}
	if strings.TrimSpace(cfg.Upload.Dir) == "" {
		cfg.Upload.Dir = "./data/uploads"
	}
	if cfg.Upload.MaxMB == 0 {
		cfg.Upload.MaxMB = 10
	}
	if strings.TrimSpace(cfg.Storage.Driver) == "" {
		cfg.Storage.Driver = "local"
	}
	if strings.TrimSpace(cfg.OpenAI.Model) == "" {
		cfg.OpenAI.Model = "gpt-4o"
	}
	if strings.TrimSpace(cfg.Anthropic.Model) == "" {
		// Default: Claude Sonnet 4.6 (current API). Override via DADIARY_ANTHROPIC_MODEL.
		cfg.Anthropic.Model = "claude-sonnet-4-6"
	}
	// AI retry defaults (see pkg/retry). Zero-values mean "unset" → apply the
	// recommended policy so retries are on by default without extra config.
	if cfg.AI.Retry.MaxRetries <= 0 {
		cfg.AI.Retry.MaxRetries = 3
	}
	if cfg.AI.Retry.InitialDelay <= 0 {
		cfg.AI.Retry.InitialDelay = 500 * time.Millisecond
	}
	if cfg.AI.Retry.MaxDelay <= 0 {
		cfg.AI.Retry.MaxDelay = 5 * time.Second
	}
	if cfg.AI.Retry.BackoffMultiplier <= 1 {
		cfg.AI.Retry.BackoffMultiplier = 2
	}

	// Admin emails: support comma-separated env for simple Beta admin gating.
	if raw := strings.TrimSpace(os.Getenv("DADIARY_ADMIN_EMAILS")); raw != "" && len(cfg.AdminEmails) == 0 {
		for _, part := range strings.Split(raw, ",") {
			if e := strings.TrimSpace(strings.ToLower(part)); e != "" {
				cfg.AdminEmails = append(cfg.AdminEmails, e)
			}
		}
	}
	for i, e := range cfg.AdminEmails {
		cfg.AdminEmails[i] = strings.TrimSpace(strings.ToLower(e))
	}

	// Validate the *final* merged retry settings (YAML + env + defaults). This
	// fails startup fast on incoherent combinations that defaults can't fix —
	// most notably an explicit max_delay that is smaller than initial_delay.
	if err := validateRetryConfig(cfg.AI.Retry); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// validateRetryConfig verifies the ai.retry block is internally consistent.
//
// Rules:
//   - max_retries        > 0            (at least one attempt budget)
//   - initial_delay      > 0            (a real base backoff)
//   - max_delay          >= initial_delay (the cap can't be below the floor)
//   - backoff_multiplier >= 1           (delay must not shrink between retries)
//
// All violations are collected and returned together via errors.Join so an
// operator sees every problem at once instead of fixing them one restart at a
// time. It works identically whether values came from config.yaml or the
// DADIARY_AI_RETRY_* environment variables, since both feed the same struct.
func validateRetryConfig(cfg retry.Config) error {
	var errs []error
	if cfg.MaxRetries <= 0 {
		errs = append(errs, fmt.Errorf("ai.retry.max_retries must be greater than 0 (got %d)", cfg.MaxRetries))
	}
	if cfg.InitialDelay <= 0 {
		errs = append(errs, fmt.Errorf("ai.retry.initial_delay must be greater than 0 (got %s)", cfg.InitialDelay))
	}
	if cfg.MaxDelay < cfg.InitialDelay {
		errs = append(errs, fmt.Errorf("ai.retry.max_delay must be greater than or equal to initial_delay (max_delay=%s, initial_delay=%s)", cfg.MaxDelay, cfg.InitialDelay))
	}
	if cfg.BackoffMultiplier < 1 {
		errs = append(errs, fmt.Errorf("ai.retry.backoff_multiplier must be greater than or equal to 1 (got %g)", cfg.BackoffMultiplier))
	}
	// errors.Join returns nil when errs is empty.
	return errors.Join(errs...)
}

// AnthropicModel returns the configured Claude model with package default applied.
func (c *Config) AnthropicModel() string {
	if c == nil {
		return "claude-sonnet-4-6"
	}
	if m := strings.TrimSpace(c.Anthropic.Model); m != "" {
		return m
	}
	return "claude-sonnet-4-6"
}

// AnthropicCoachModel returns the model to use for the Claude text-coach pass.
// It prefers Anthropic.FastModel — which is the operational default today
// (DADIARY_ANTHROPIC_FAST_MODEL=claude-3-5-haiku-latest, our latency lever) — and
// falls back to the standard AnthropicModel (Sonnet) only when FastModel is unset,
// which is how an operator reverts the coach to Sonnet quality. Every coach call
// and its logging resolves the model through this so logs report the model that
// actually ran (Haiku vs Sonnet vs override).
func (c *Config) AnthropicCoachModel() string {
	if c != nil {
		if m := strings.TrimSpace(c.Anthropic.FastModel); m != "" {
			return m
		}
	}
	return c.AnthropicModel()
}

// OpenAITextModel is the GPT model for text-coaching fallback (default gpt-4o).
func (c *Config) OpenAITextModel() string {
	if c == nil {
		return "gpt-4o"
	}
	if m := strings.TrimSpace(c.OpenAI.Model); m != "" {
		return m
	}
	return "gpt-4o"
}

// OpenAIVisionModel is the GPT model for photo vision + moderation (default gpt-4o).
func (c *Config) OpenAIVisionModel() string {
	if c == nil {
		return "gpt-4o"
	}
	if m := strings.TrimSpace(c.OpenAI.VisionModel); m != "" {
		return m
	}
	return c.OpenAITextModel()
}

// HasAnthropicKey reports whether Claude text coaching can be attempted.
func (c *Config) HasAnthropicKey() bool {
	return c != nil && strings.TrimSpace(c.Anthropic.APIKey) != ""
}

// HasOpenAIKey reports whether OpenAI vision / text fallback is available.
func (c *Config) HasOpenAIKey() bool {
	return c != nil && strings.TrimSpace(c.OpenAI.APIKey) != ""
}

// IsAdminEmail reports whether email is in the configured admin allow-list.
func (c *Config) IsAdminEmail(email string) bool {
	if c == nil || len(c.AdminEmails) == 0 {
		return false
	}
	norm := strings.TrimSpace(strings.ToLower(email))
	for _, e := range c.AdminEmails {
		if e == norm {
			return true
		}
	}
	return false
}
