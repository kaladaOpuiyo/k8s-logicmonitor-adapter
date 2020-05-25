package config

import (
	"github.com/kelseyhightower/envconfig"
)

// Config represents the application's configuration file.
type Config struct {
	*Secrets
}

// Secrets represents the application's sensitive configuration file.
type Secrets struct {
	Account string `envconfig:"ACCOUNT"`
	ID      string `envconfig:"ACCESS_ID"`
	Key     string `envconfig:"ACCESS_KEY"`
}

// GetConfig returns the application configuration specified by the config file.
func GetConfig() (*Config, error) {

	c := &Config{}

	if err := envconfig.Process("k8s-logicmonitor-adapter", c); err != nil {
		return nil, err
	}

	return c, nil
}
