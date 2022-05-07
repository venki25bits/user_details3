package router

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	_ "net/http/pprof" // debug

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// swagger:model ErrorResponse
type ErrorResponse struct {
	Err    string `json:"error,omitempty"`
	Errors Errors `json:"errors,omitempty"`
}

// Errors holds multiple errors
type Errors []error

// MarshalJSON ...
func (e Errors) MarshalJSON() ([]byte, error) {
	errs := make([]string, 0)
	for _, err := range e {
		errs = append(errs, err.Error())
	}
	return json.Marshal(&errs)
}

// Contains checks if any errors passed into the method matches any errors within Errors at any error level
func (e Errors) Contains(err ...error) bool {
	for i := range e {
		for j := range err {
			if e[i] == err[j] || errors.Cause(e[i]) == err[j] {
				return true
			}
		}
	}
	return false
}

// BuildInfo ...
type BuildInfo struct {
	Start     time.Time `json:"-"`
	Uptime    string    `json:"uptime,omitempty"`
	Version   string    `json:"version,omitempty"`
	BuildDate string    `json:"build_date,omitempty"`
	BuildHost string    `json:"build_host,omitempty"`
	GitURL    string    `json:"git_url,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Debug     bool      `json:"debug"`
}

type metrics struct {
	InFlight prometheus.Gauge
	Counter  *prometheus.CounterVec
	Duration *prometheus.HistogramVec
}

var m *metrics

func init() {
	m = &metrics{
		InFlight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "In Flight HTTP requests.",
			},
		),
		Counter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Counter of HTTP requests.",
			},
			[]string{"handler", "code", "method"},
		),
		Duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Histogram of latencies for HTTP requests.",
				Buckets: []float64{.01, .05, .1, .2, .4, 1, 3, 8, 20, 60, 120},
			},
			[]string{"handler", "code", "method"},
		),
	}
	m.register()
}

func (m *metrics) handler(path string, handler http.Handler) (string, http.Handler) {
	return path,
		promhttp.InstrumentHandlerCounter(m.Counter.MustCurryWith(prometheus.Labels{"handler": path}),
			promhttp.InstrumentHandlerInFlight(m.InFlight,
				promhttp.InstrumentHandlerDuration(m.Duration.MustCurryWith(prometheus.Labels{"handler": path}),
					handler,
				),
			),
		)
}

func (m *metrics) handlerFunc(path string, f http.HandlerFunc) (string, http.HandlerFunc) {
	return path,
		promhttp.InstrumentHandlerCounter(m.Counter.MustCurryWith(prometheus.Labels{"handler": path}),
			promhttp.InstrumentHandlerInFlight(m.InFlight,
				promhttp.InstrumentHandlerDuration(m.Duration.MustCurryWith(prometheus.Labels{"handler": path}),
					f,
				),
			),
		)
}

func (m *metrics) register() {
	prometheus.MustRegister(m.InFlight, m.Counter, m.Duration)
}

func recovery(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			defer r.Body.Close()
			ctx := r.Context()

			if r := recover(); r != nil {
				var err error
				switch t := r.(type) {
				case string:
					err = errors.New(t)
				case error:
					err = errors.WithStack(t)
				default:
					err = errors.New("unknown error")
				}
				log.Error().Stack().Caller().Err(err).Msg("an unexpected error occurred")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else if err := ctx.Err(); err != nil {
				log.Warn().Err(err).Msg("request canceled")
				// Use nginx's non-standard response code for metrics
				// 499: Client Closed Request
				http.Error(w, err.Error(), 499)
				return
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// Router ...
type Router struct {
	Info    *BuildInfo
	mux     *mux.Router
	metrics *metrics
}

// GetRoute ...
func (r *Router) GetRoute(name string) *mux.Route {
	return r.mux.GetRoute(name)
}

// Handle implements http.Handler.
func (r *Router) Handle(path string, handler http.Handler) *mux.Route {
	handler = recovery(handler)
	return r.mux.Handle(path, handler)
}

// HandleFunc implements http.Handler.
func (r *Router) HandleFunc(path string, f http.HandlerFunc) *mux.Route {
	return r.Handle(path, f)
}

// HandleWithMetrics implements http.Handler wrapping handler with m.
func (r *Router) HandleWithMetrics(path string, handler http.Handler) *mux.Route {
	handler = recovery(handler)
	return r.mux.Handle(r.metrics.handler(path, handler))
}

// HandleFuncWithMetrics implements http.Handler wrapping handler func with m.
func (r *Router) HandleFuncWithMetrics(path string, f http.HandlerFunc) *mux.Route {
	return r.HandleWithMetrics(path, f)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// PathPrefix ...
func (r *Router) PathPrefix(prefix string) *mux.Route {
	return r.mux.PathPrefix(prefix)
}

// NewRouter returns a new Router.
func NewRouter(buildinfo *BuildInfo) *Router {
	router := &Router{
		Info:    buildinfo,
		mux:     mux.NewRouter(),
		metrics: m,
	}

	// create basic endpoints used for dev ops and prod support
	router.Handle("/info", info(router.Info)).Methods(http.MethodGet, http.MethodHead).Name("INFO")
	router.Handle("/health", health()).Methods(http.MethodGet, http.MethodHead).Name("HEALTH")
	router.Handle("/metrics", promhttp.Handler()).Name("METRICS")

	// create swagger endpoint in all environments except master
	if router.Info.Branch != "master" {
		fs := http.FileServer(http.Dir("./swagger/"))
		router.PathPrefix("/swagger/").Handler(http.StripPrefix("/swagger/", fs))
		http.Handle("/swagger/", router)
	}

	if buildinfo.Debug {
		log.Warn().Msg("pprof enabled")
		router.mux.PathPrefix("/debug/pprof").Handler(http.DefaultServeMux).Name("PPROF")
		go func() {
			log.Error().Err(http.ListenAndServe("localhost:6060", nil)).Send()
		}()
	}

	return router
}

func health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		Respond(w, http.StatusOK, []byte(http.StatusText(http.StatusOK)))
	}
}

func info(buildinfo *BuildInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		buildinfo.Uptime = time.Now().Sub(buildinfo.Start).String()
		RespondWithJSON(w, http.StatusOK, buildinfo)
	}
}

// RespondWithError ...
func RespondWithError(w http.ResponseWriter, code int, err error) {
	RespondWithJSON(w, code, ErrorResponse{Err: err.Error()})
}

// RespondWithErrors ...
func RespondWithErrors(w http.ResponseWriter, code int, errs Errors) {
	RespondWithJSON(w, code, ErrorResponse{Errors: errs})
}

// RespondWithJSON ...
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	body, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	Respond(w, code, body)
}

// Respond ...
func Respond(w http.ResponseWriter, code int, body []byte) {
	w.WriteHeader(code)
	w.Write(body)
}

// ReadyChecker ...
type ReadyChecker interface {
	Ready() bool
}

// Ready ...
func Ready(dependencies ...ReadyChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ready := []byte("OK")
		numDeps := len(dependencies)
		if numDeps == 0 {
			Respond(w, http.StatusOK, ready)
			return
		}
		wg := sync.WaitGroup{}
		checks := make(chan bool, numDeps)
		for _, dep := range dependencies {
			wg.Add(1)
			if dep != nil {
				go func(d ReadyChecker) {
					checks <- d.Ready()
					wg.Done()
				}(dep)
			}
		}
		wg.Wait()
		close(checks)
		for ok := range checks {
			if !ok {
				Respond(w, http.StatusServiceUnavailable, nil)
				return
			}
		}
		Respond(w, http.StatusOK, ready)
		return
	}
}
