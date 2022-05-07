package sql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var m *metrics

type metrics struct {
	maxOpenDesc           *prometheus.Desc
	openDesc              *prometheus.Desc
	inUseDesc             *prometheus.Desc
	idleDesc              *prometheus.Desc
	waitedForDesc         *prometheus.Desc
	blockedSecondsDesc    *prometheus.Desc
	closedMaxIdleDesc     *prometheus.Desc
	closedMaxLifetimeDesc *prometheus.Desc
	duration              *prometheus.HistogramVec
}

func init() {
	m = &metrics{
		maxOpenDesc: prometheus.NewDesc(
			"sql_stats_connections_max_open",
			"Maximum number of open connections to the database.",
			nil,
			nil,
		),
		openDesc: prometheus.NewDesc(
			"sql_stats_connections_open",
			"The number of established connections both in use and idle.",
			nil,
			nil,
		),
		inUseDesc: prometheus.NewDesc(
			"sql_stats_connections_in_use",
			"The number of connections currently in use.",
			nil,
			nil,
		),
		idleDesc: prometheus.NewDesc(
			"sql_stats_connections_idle",
			"The number of idle connections.",
			nil,
			nil,
		),
		waitedForDesc: prometheus.NewDesc(
			"sql_stats_connections_waited_for",
			"The total number of connections waited for.",
			nil,
			nil,
		),
		blockedSecondsDesc: prometheus.NewDesc(
			"sql_stats_connections_blocked_seconds",
			"The total time blocked waiting for a new connection.",
			nil,
			nil,
		),
		closedMaxIdleDesc: prometheus.NewDesc(
			"sql_stats_connections_closed_max_idle",
			"The total number of connections closed due to SetMaxIdleConns.",
			nil,
			nil,
		),
		closedMaxLifetimeDesc: prometheus.NewDesc(
			"sql_stats_connections_closed_max_lifetime",
			"The total number of connections closed due to SetConnMaxLifetime.",
			nil,
			nil,
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "sql_command_duration_seconds",
				Help:    "Histogram of latencies for SQL requests.",
				Buckets: []float64{.01, .05, .1, .2, .4, 1, 3, 8, 20, 60, 120},
			},
			[]string{"database", "host", "user", "command", "query"},
		),
	}
	prometheus.MustRegister(m.duration)
}

// Config represents the basic configurables for the sql connection.
type Config struct {
	Database string  `json:"database"`
	URL      string  `json:"url" base64:"true"`
	Username string  `json:"username" base64:"true"`
	Password string  `json:"password" base64:"true"`
	Options  Options `json:"options"`
}

// Options represents the configurable sql database connection pool options.
type Options struct {
	MaxOpenConnections int           `json:"max-open-connections"`
	MaxIdleConnections int           `json:"max-idle-connections"`
	MaxLifetime        time.Duration `json:"max-lifetime-ms"`
}

// Sql represents the sql connection struct.
type Sql struct {
	name     string
	host     string
	username string

	URL      *url.URL
	Options  Options
	Database *sql.DB // Export for mocking
	Logger   zerolog.Logger
}

// Connect creates a new sql connection configurables.
func (ds *Sql) Connect(conf Config) error {
	ds.Logger = log.With().Str("connection", conf.Database).Logger()
	uri, err := url.Parse(conf.URL)
	if err != nil {
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}

	if conf.Database == "" {
		databaseName := uri.Query().Get("database")
		if databaseName != "" {
			ds.Logger = log.With().Str("connection", databaseName).Logger()
		}
	} else {
		q := uri.Query()
		q.Set("database", conf.Database)
		uri.RawQuery = q.Encode()
	}

	if conf.Username != "" && conf.Password != "" {
		uri.User = url.UserPassword(conf.Username, conf.Password)
	}

	ds.URL = uri
	ds.Options = Options{
		MaxOpenConnections: conf.Options.MaxOpenConnections,
		MaxIdleConnections: conf.Options.MaxIdleConnections,
		MaxLifetime:        conf.Options.MaxLifetime * time.Millisecond,
	}

	sqlDriver := ds.URL.Scheme

	database, err := sql.Open(sqlDriver, ds.URL.String())
	if err != nil {
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}

	database.SetMaxOpenConns(ds.Options.MaxOpenConnections)
	database.SetMaxIdleConns(ds.Options.MaxIdleConnections)
	database.SetConnMaxLifetime(ds.Options.MaxLifetime)
	ds.Database = database
	ds.name, ds.host, ds.username = ds.URL.Query().Get("database"), ds.URL.Hostname(), ds.URL.User.Username()
	prometheus.WrapRegistererWith(prometheus.Labels{
		"database": ds.name,
		"host":     ds.host,
		"user":     ds.username,
	}, prometheus.DefaultRegisterer).MustRegister(ds)
	return nil
}

// Ping is used to check if the remote server is available.
// see: github.com/denisenkom/go-mssqldb@v0.0.0-20190515213511-eb9f6a1743f3/mssql.go:898
func (ds *Sql) Ping() error {
	if ds.Database == nil {
		err := errors.New("could not connect to database")
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}

	// creates a new context to add to the client's connection. We do not need to return a CancelFunc().
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := ds.PingContext(ctx)
	if err != nil {
		ds.Logger.Error().Stack().Caller().Err(err).Send()
		return err
	}
	return nil
}

// Stats helper method to return sql DB Stats
// See sql.DBStats
func (ds *Sql) Stats() sql.DBStats {
	return ds.Database.Stats()
}

// PingContext wraps and exposes sql.PingContext with metrics
func (ds *Sql) PingContext(ctx context.Context) error {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "ping", "").Observe(time.Since(start).Seconds())
	}()
	return ds.Database.PingContext(ctx)
}

// ExecContext wraps and exposes sql.ExecContext with metrics
func (ds *Sql) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "exec", query).Observe(time.Since(start).Seconds())
	}()
	return ds.Database.ExecContext(ctx, query, args...)
}

// Exec wraps and exposes sql.Exec with metrics
func (ds *Sql) Exec(query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "exec", query).Observe(time.Since(start).Seconds())
	}()
	return ds.Database.Exec(query, args...)
}

// QueryContext wraps and exposes sql.QueryContext with metrics
func (ds *Sql) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "query", query).Observe(time.Since(start).Seconds())
	}()
	return ds.Database.QueryContext(ctx, query, args...)
}

// Query wraps and exposes sql.Query with metrics
func (ds *Sql) Query(query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "query", query).Observe(time.Since(start).Seconds())
	}()
	return ds.Database.Query(query, args...)
}

// QueryRowContext wraps and exposes sql.QueryRowContext with metrics
func (ds *Sql) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "query_row", query).Observe(time.Since(start).Seconds())
	}()
	return ds.Database.QueryRowContext(ctx, query, args...)
}

// QueryRow wraps and exposes sql.QueryRow with metrics
func (ds *Sql) QueryRow(query string, args ...interface{}) *sql.Row {
	start := time.Now()
	defer func() {
		m.duration.WithLabelValues(ds.name, ds.host, ds.username, "query_row", query).Observe(time.Since(start).Seconds())
	}()
	return ds.Database.QueryRow(query, args...)
}

// Close wraps and exposes sql.Close
func (ds *Sql) Close() error {
	return ds.Database.Close()
}

// PrepareContext wraps and exposes sql.PrepareContext
func (ds *Sql) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return ds.Database.PrepareContext(ctx, query)
}

// Prepare wraps and exposes sql.Prepare
func (ds *Sql) Prepare(query string) (*sql.Stmt, error) {
	return ds.Database.Prepare(query)
}

// BeginTx wraps and exposes sql.BeginTx
func (ds *Sql) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return ds.Database.BeginTx(ctx, opts)
}

// Begin wraps and exposes sql.Begin
func (ds *Sql) Begin() (*sql.Tx, error) {
	return ds.Database.Begin()
}

// Driver wraps and exposes sql.Driver
func (ds *Sql) Driver() driver.Driver {
	return ds.Database.Driver()
}

// Conn wraps and exposes sql.Conn
func (ds *Sql) Conn(ctx context.Context) (*sql.Conn, error) {
	return ds.Database.Conn(ctx)
}

// Describe implements the prometheus.Collector interface.
func (ds *Sql) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.maxOpenDesc
	ch <- m.openDesc
	ch <- m.inUseDesc
	ch <- m.idleDesc
	ch <- m.waitedForDesc
	ch <- m.blockedSecondsDesc
	ch <- m.closedMaxIdleDesc
	ch <- m.closedMaxLifetimeDesc
}

// Collect implements the prometheus.Collector interface.
func (ds *Sql) Collect(ch chan<- prometheus.Metric) {
	stats := ds.Stats()

	ch <- prometheus.MustNewConstMetric(
		m.maxOpenDesc,
		prometheus.GaugeValue,
		float64(stats.MaxOpenConnections),
	)
	ch <- prometheus.MustNewConstMetric(
		m.openDesc,
		prometheus.GaugeValue,
		float64(stats.OpenConnections),
	)
	ch <- prometheus.MustNewConstMetric(
		m.inUseDesc,
		prometheus.GaugeValue,
		float64(stats.InUse),
	)
	ch <- prometheus.MustNewConstMetric(
		m.idleDesc,
		prometheus.GaugeValue,
		float64(stats.Idle),
	)
	ch <- prometheus.MustNewConstMetric(
		m.waitedForDesc,
		prometheus.CounterValue,
		float64(stats.WaitCount),
	)
	ch <- prometheus.MustNewConstMetric(
		m.blockedSecondsDesc,
		prometheus.CounterValue,
		stats.WaitDuration.Seconds(),
	)
	ch <- prometheus.MustNewConstMetric(
		m.closedMaxIdleDesc,
		prometheus.CounterValue,
		float64(stats.MaxIdleClosed),
	)
	ch <- prometheus.MustNewConstMetric(
		m.closedMaxLifetimeDesc,
		prometheus.CounterValue,
		float64(stats.MaxLifetimeClosed),
	)
}
