package mgo

import (
	"context"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var m *metrics

type metrics struct {
	openConnections *prometheus.GaugeVec
	inUse           *prometheus.GaugeVec
	duration        *prometheus.HistogramVec
}

func (m *metrics) register() {
	prometheus.MustRegister(m.openConnections, m.inUse, m.duration)
}

func init() {
	m = &metrics{
		openConnections: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mongo_open_connections",
				Help: "Gauge of Open Connections of the Mongodb",
			},
			[]string{"database", "host", "user"},
		),
		inUse: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mongo_in_use",
				Help: "Gauge of Connections that are currently in use of the Mongodb",
			},
			[]string{"database", "host", "user"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mongo_command_duration_seconds",
				Help:    "Histogram of latencies for Mongodb requests.",
				Buckets: []float64{.01, .05, .1, .2, .4, 1, 3, 8, 20, 60, 120},
			},
			[]string{"database", "host", "user", "command"},
		),
	}
	m.register()
}

// Config represents the basic configurations for the mongo.
type Config struct {
	Database string `json:"database"`
	URL      string `json:"url" base64:"true"`
	Username string `json:"username" base64:"true"`
	Password string `json:"password" base64:"true"`
}

// Mongo represents the basic configuration for connecting to mongo.
type Mongo struct {
	name     string
	host     string
	username string

	URL      *url.URL
	Database *mongo.Database
	Logger   zerolog.Logger
}

// Connect creates a new mongo connection based on config configurations.
func (ds *Mongo) Connect(conf Config) error {
	ds.Logger = log.With().Str("connection", conf.Database).Logger()
	uri, err := url.Parse(conf.URL)
	if err != nil {
		ds.Logger.Error().Err(err).Send()
		return err
	}

	ds.URL = uri
	opts := options.Client()
	opts.ApplyURI(ds.URL.String())
	opts.Monitor = &event.CommandMonitor{
		Started: func(i context.Context, startedEvent *event.CommandStartedEvent) {
			e := ds.Logger.Trace()
			err := i.Err()
			if err != nil {
				e = ds.Logger.Error().Err(err)
			}
			e.Str("database", startedEvent.DatabaseName).
				Str("connection_id", startedEvent.ConnectionID).
				Int64("request_id", startedEvent.RequestID).Send()
		},
		Succeeded: func(i context.Context, succeededEvent *event.CommandSucceededEvent) {
			e := ds.Logger.Trace()
			err := i.Err()
			if err != nil {
				e = ds.Logger.Error().Err(err)
			}
			e.Str("command", succeededEvent.CommandName).
				Str("connection_id", succeededEvent.ConnectionID).
				Int64("request_id", succeededEvent.RequestID).
				Dur("resp_time", time.Duration(succeededEvent.DurationNanos)/time.Nanosecond).Send()
			m.duration.WithLabelValues(ds.name, ds.host, ds.username, succeededEvent.CommandName).Observe(time.Duration(succeededEvent.DurationNanos).Seconds())
		},
		Failed: func(i context.Context, failedEvent *event.CommandFailedEvent) {
			e := ds.Logger.Error()
			err := i.Err()
			if err != nil {
				e.Err(err)
			}
			e.Str("command", failedEvent.CommandName).
				Str("connection_id", failedEvent.ConnectionID).
				Int64("request_id", failedEvent.RequestID).
				Dur("resp_time", time.Duration(failedEvent.DurationNanos)/time.Nanosecond).
				Str("failure", failedEvent.Failure).Send()
			m.duration.WithLabelValues(ds.name, ds.host, ds.username, failedEvent.CommandName).Observe(time.Duration(failedEvent.DurationNanos).Seconds())
		},
	}
	opts.PoolMonitor = &event.PoolMonitor{
		Event: func(poolEvent *event.PoolEvent) {
			ds.Logger.Trace().Str("pool", poolEvent.Type).
				Uint64("connection_id", poolEvent.ConnectionID).
				Str("address", poolEvent.Address).
				Str("reason", poolEvent.Reason).Send()

			switch poolEvent.Type {
			case event.ConnectionCreated:
				m.openConnections.WithLabelValues(ds.name, ds.host, ds.username).Inc()
			case event.ConnectionClosed:
				m.openConnections.WithLabelValues(ds.name, ds.host, ds.username).Dec()
			case event.GetSucceeded:
				m.inUse.WithLabelValues(ds.name, ds.host, ds.username).Inc()
			case event.ConnectionReturned:
				m.inUse.WithLabelValues(ds.name, ds.host, ds.username).Dec()
			}
		},
	}

	// creates a new mongo client with configurables defined above.
	client, err := mongo.NewClient(opts)
	if err != nil {
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}

	// creates a new context to add to the client's connection. We do not need to return a CancelFunc().
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Connect(ctx)
	if err != nil {
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}

	ds.Database = client.Database(conf.Database)
	ds.name, ds.host, ds.username = ds.Database.Name(), ds.URL.Hostname(), ds.URL.User.Username()
	return nil
}

// Ping is a no-op used to test whether a server is responding to commands. This command will return immediately even if the server is write-locked
// see: https://docs.mongodb.com/manual/reference/command/ping/
func (ds *Mongo) Ping() error {
	if ds.Database == nil {
		err := errors.New("could not connect to database")
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}

	// creates a new context to add to the client's connection. We do not need to return a CancelFunc().
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := ds.Database.Client().Ping(ctx, nil)
	if err != nil {
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}
	return nil
}
