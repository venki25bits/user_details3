package controller

import (
	"user-details/pkg/config"
	"user-details/pkg/db"
	"net/http"
	"net/url"

	common "vendor.lib/tng/tng-lib/http"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// Controller houses application's dependencies.
type Controller struct {
	datasource *db.Datasource
	clients    map[string]*common.Client
}

// New Create a new Controller
func New(cfg config.Config) (*Controller, error) {

	clients := make(map[string]*common.Client)

	for k, v := range cfg.Clients {
		client, err := common.New(v)
		if err != nil {
			return &Controller{}, errors.Wrap(err, "Unable to make clients")
		}
		clients[k] = client
	}

	return &Controller{
		datasource: db.Initialize(cfg),
		clients:    clients,
	}, nil
}

// Ready K8s ready check. Verifies connection to all dependencies
func (c *Controller) Ready() error {

	if c.clients["login-service"] != nil {
		uri := &url.URL{Path: "/pkg"}
		resp, err := c.clients["login-service"].Get(uri, http.Header{})
		if err != nil {
			log.Error().Stack().Caller().Err(err).Send()
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New("chp-api dependency is not available")
		}
	}

	err := c.datasource.Mongo.Ping()
	if err != nil {
		e := log.Error().Stack().Caller().Err(err)
		e.Msgf("could not connect to Mongo database,configure in datasource.json")
		return err
	}

	err = c.datasource.Mssql.Ping()
	if err != nil {
		e := log.Error().Stack().Caller().Err(err)
		e.Msgf("could not connect to database, configure in datasource.json")
		return err
	}

	return nil
}

func (c *Controller) FindUserDetails(userId string, ctx context.Context) ([]model.User, error){
	var users []model.User

	user, err := c.datasource.Mongo.FindUser(userId, ctx)
	
	return user
}
