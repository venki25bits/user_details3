package main

import (
	"user-details/pkg/server"
	"time"

	router "vendor.lib/tng/tng-lib/router/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

var (
	start = time.Now()
	version,
	buildDate,
	buildHost,
	gitURL,
	branch string
)

func main() {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	err := server.Run(&router.BuildInfo{
		Start:     start,
		Version:   version,
		BuildDate: buildDate,
		BuildHost: buildHost,
		GitURL:    gitURL,
		Branch:    branch,
	})
	if err != nil {
		log.Fatal().Stack().Caller().Err(err).Send()
	}
}
