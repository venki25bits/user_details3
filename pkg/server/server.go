package server

import (
	router "vendor.lib/tng/tng-lib/router/mux"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"api-template/pkg/config"
	"api-template/pkg/controller"
	"api-template/pkg/service"
	"net/http"
)

// Run configures and creates a new http.Server to be used for the application to listen on
func Run(info *router.BuildInfo) error {
	conf, err := config.GetConfig()
	if err != nil {
		return err
	}

	info.Debug = conf.Debug
	level, err := zerolog.ParseLevel(conf.LogLevel)
	if err != nil {
		level = zerolog.ErrorLevel
		log.Warn().Err(err).Msgf("unable to parse log level, logging level is set to %s", level.String())
	}
	zerolog.SetGlobalLevel(level)
	log.Logger = log.With().Str("app", conf.Name).Logger()

	ctrl, err := controller.New(conf)
	if err != nil {
		return errors.Wrap(err, "unable to create controller")
	}

	router := router.NewRouter(info)
	service.AddHandlers(router, ctrl)

	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", conf.Port),
		Handler: cors.Default().Handler(router),
	}

	log.Info().Msgf("Server running %v", srv.Addr)
	return srv.ListenAndServe()
}