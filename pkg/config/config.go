package config

import (
	"vendor.lib/tng/tng-lib/config"
)

const (
	portEnvVar  = "PORT"
	defaultPort = 3000

	shutdownTimeoutEnvVar  = "SHUTDOWN_TIMEOUT"
	defaultShutdownTimeout = 25
)

// Config application configuration
type Config struct {
	config.Application
	config.Datasource
}

func GetConfig() (Config, error) {

	conf := Config{}
	if err := config.Read("config/app.json", &conf); err != nil {
		return conf, err
	}

	if err := config.Read("config/datasource.json", &conf.Datasource); err != nil {
		return conf, err
	}

	return conf, nil
}
