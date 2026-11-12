package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config junta toda la config del server. Si falta algo critico
// prefiero reventar al arranque y no en runtime.
type Config struct {
	Port             string        `envconfig:"PORT" default:"8080"`
	DatabaseURL      string        `envconfig:"DATABASE_URL" required:"true"`
	LogLevel         string        `envconfig:"LOG_LEVEL" default:"info"`
	JWTSecret        string        `envconfig:"JWT_SECRET" default:"dev-secret-change-me"`
	ReadTimeout      time.Duration `envconfig:"READ_TIMEOUT" default:"15s"`
	WriteTimeout     time.Duration `envconfig:"WRITE_TIMEOUT" default:"15s"`
	Env              string        `envconfig:"ENV" default:"development"`
	MPWebhookSecret  string        `envconfig:"MP_WEBHOOK_SECRET" default:""`
}

// Load lee las variables del environment y arma un Config.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

// IsProd devuelve true si estamos corriendo en produccion.
// Lo uso para prender/apagar logs verbosos y modo debug de gqlgen.
func (c *Config) IsProd() bool {
	return c.Env == "production"
}
