package db

import (
	"user-details/pkg/config"
	"user-details/pkg/db/mongo"
	"user-details/pkg/db/mssql"
	_ "github.com/denisenkom/go-mssqldb" // needed for sql driver.

	"github.com/rs/zerolog/log"
)

// Datasource represents the sql and mongo connected databases utilized by the application.
type Datasource struct {
	Mongo mongo.Mongo
	Mssql mssql.Mssql
}

// Initialize creates a new Datasource object and populates it with tested connections to sql and mongo databases.
func Initialize(conf config.Config) *Datasource {

	mgo := mongo.Mongo{Collection: conf.Collection}
	err := mgo.Connect(conf.Datasource.Mongo["cm"])
	
	if err == nil {
		if mgo.Ping() != nil {
			log.Warn().Err(err).Msg("Unable to connect to mongo")
		}
	}

	mssql := mssql.Mssql{}
	err = mssql.Connect(conf.SQL)

	if err == nil {
		if mssql.Ping() != nil {
			log.Warn().Err(err).Msg("Unable to connect to sql")
		}
	}

	return &Datasource{Mongo: mgo, Mssql: mssql}
}