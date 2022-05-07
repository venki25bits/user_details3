package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/machinebox/graphql"

	"github.com/rs/zerolog/log"

	"github.com/prometheus/client_golang/prometheus"
)

type Response struct {
	Status           string // e.g. "200 OK"
	StatusCode       int    // e.g. 200
	Proto            string // e.g. "HTTP/1.0"
	ProtoMajor       int    // e.g. 1
	ProtoMinor       int    // e.g. 0
	Header           http.Header
	Body             []byte
	ContentLength    int64
	TransferEncoding []string
	Uncompressed     bool
	Trailer          http.Header
	Duration         time.Duration
	Request          *http.Request
}

type Client struct {
	client     *http.Client
	url        *url.URL
	headers    http.Header
	maxRetry   int
	retryDelay time.Duration
}

var m *metrics

type metrics struct {
	inFlight *prometheus.GaugeVec
	counter  *prometheus.CounterVec
	status   *prometheus.CounterVec
	error    *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func (m *metrics) register() {
	prometheus.MustRegister(m.inFlight, m.counter, m.status, m.error, m.duration)
}

func init() {
	m = &metrics{
		inFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "http_outbound_requests_in_flight",
				Help: "In Flight Outbound HTTP requests.",
			},
			[]string{"method", "host"},
		),
		counter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_outbound_requests_total",
				Help: "Counter of successful Outbound HTTP requests.",
			},
			[]string{"method", "host"},
		),
		status: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_outbound_requests_status_total",
				Help: "Counter of successful Outbound HTTP requests by status code.",
			},
			[]string{"method", "host", "code"},
		),
		error: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_outbound_requests_error_total",
				Help: "Counter of Outbound HTTP requests errors.",
			},
			[]string{"method", "host"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_outbound_request_duration_seconds",
				Help:    "Histogram of latencies for Outbound HTTP requests.",
				Buckets: []float64{.01, .05, .1, .2, .4, 1, 3, 8, 20, 60, 120},
			},
			[]string{"method", "host"},
		),
	}
	m.register()
}

// Config represents the configuration of the api package and all of its structs, methods and functions. These values are
// being read in currently from the app.json secret file.
//
// NOTE: in the future, we are wanting to look into utilizing config maps and secrets to identify between non-sensitive
// and sensitive information.
type Config struct {
	URL string `json:"url"`

	// Timeout specifies a time limit for requests made by this
	// Client. The timeout includes connection time, any
	// redirects, and reading the response body. The timer remains
	// running after Get, Head, Post, or Do return and will
	// interrupt reading of the Response.Body.
	// A Timeout of zero means no timeout.
	Timeout time.Duration `json:"timeout-ms"`

	// IdleConnTimeout is the maximum amount of time an idle
	// (keep-alive) connection will remain idle before closing
	// itself.
	// Zero means no limit.
	IdleConnTimeout time.Duration `json:"idle-connection-timeout-ms"`

	// InsecureSkipVerify controls whether a client verifies the
	// server's certificate chain and host name.
	// If InsecureSkipVerify is true, TLS accepts any certificate
	// presented by the server and any host name in that certificate.
	InsecureSkipVerify bool `json:"insecure-skip-verify"`

	// MaxConnsPerHost optionally limits the total number of
	// connections per host, including connections in the dialing,
	// active, and idle states. On limit violation, dials will block.
	// Zero means no limit.
	MaxConnsPerHost int `json:"max-connection-per-host"`

	// MaxIdleConns controls the maximum number of idle (keep-alive)
	// connections across all hosts. Zero means no limit.
	MaxIdleConns int `json:"max-idle-connections"`

	// MaxIdleConnsPerHost, if non-zero, controls the maximum idle
	// (keep-alive) connections to keep per-host. If zero,
	// DefaultMaxIdleConnsPerHost is used.
	MaxIdleConnsPerHost int `json:"max-idle-connections-per-host"`

	Headers    map[string]string `json:"default-headers"`
	MaxRetry   int               `json:"max-retry"`
	RetryDelay time.Duration     `json:"retry-delay-ms"`
	RootCAs    []string          `json:"root-cas"`
}

// New creates a new instance of a http client.
// In order to create a new client, we need to instantiate and configure three structs from the http package:
//	- transport{}
//	- client{}
//	- headers{}
//
// These configs values are coming from the Config struct being passed in as a parameter. Once all three are configured,
// we add each struct to our client implemented struct and return it.
func New(conf Config) (*Client, error) {
	uri, err := url.Parse(conf.URL)
	if err != nil {
		return nil, err
	}

	// Transport specifies the mechanism by which individual HTTP requests are made
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: conf.InsecureSkipVerify,
		},
		MaxConnsPerHost:     conf.MaxConnsPerHost,
		MaxIdleConns:        conf.MaxIdleConns,
		MaxIdleConnsPerHost: conf.MaxIdleConnsPerHost,
		IdleConnTimeout:     conf.IdleConnTimeout * time.Millisecond,
	}

	if len(conf.RootCAs) > 0 {
		cp, err := getCertPool(conf.RootCAs)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig.RootCAs = cp
	}

	// Http client
	client := &http.Client{
		Transport: transport,
		Timeout:   conf.Timeout * time.Millisecond,
	}

	// A Header represents the key-value pairs in an HTTP header. We are creating all string key pairs headers that are
	// being read in from the config file.
	headers := http.Header{}
	for k, v := range conf.Headers {
		headers.Set(k, v)
	}

	return &Client{client: client, url: uri, headers: headers, maxRetry: conf.MaxRetry, retryDelay: conf.RetryDelay}, nil
}

// GraphQL Create a GraphQL client from current client
func (c *Client) GraphQL(rel *url.URL) *GraphQL {
	uri := c.url.ResolveReference(rel)
	client := graphql.NewClient(uri.String(), graphql.WithHTTPClient(c.client))
	return &GraphQL{client: client, url: uri, headers: c.headers}
}

// Headers Get default headers
func (c *Client) Headers() http.Header {
	return c.headers
}

func (c *Client) SetTransport(t http.RoundTripper) {
	c.client.Transport = t
}

// Do ...
func (c *Client) Do(method string, rel *url.URL, headers http.Header, body io.Reader) (*Response, error) {
	uri := c.url.ResolveReference(rel)
	request, err := http.NewRequest(method, uri.String(), body)
	if err != nil {
		return nil, err
	}

	if headers == nil {
		headers = http.Header{}
	}

	for k, vs := range c.headers {
		for _, v := range vs {
			headers.Add(k, v)
		}
	}
	request.Header = headers
	return c.do(request)
}

// DoWithContext ...
func (c *Client) DoWithContext(ctx context.Context, method string, rel *url.URL, headers http.Header, body io.Reader) (*Response, error) {
	uri := c.url.ResolveReference(rel)
	request, err := http.NewRequest(method, uri.String(), body)
	if err != nil {
		return nil, err
	}
	request = request.WithContext(ctx)

	if headers == nil {
		headers = http.Header{}
	}
	for k, vs := range c.headers {
		for _, v := range vs {
			headers.Add(k, v)
		}
	}
	request.Header = headers
	return c.do(request)
}

// Get helper method for making a GET request
func (c *Client) Get(rel *url.URL, headers http.Header) (*Response, error) {
	return c.Do(http.MethodGet, rel, headers, nil)
}

// GetWithContext helper method for making a GET request
func (c *Client) GetWithContext(ctx context.Context, rel *url.URL, headers http.Header) (*Response, error) {
	return c.DoWithContext(ctx, http.MethodGet, rel, headers, nil)
}

// Put helper method for making a PUT request
func (c *Client) Put(rel *url.URL, headers http.Header, body io.Reader) (*Response, error) {
	return c.Do(http.MethodPut, rel, headers, body)
}

// PutWithContext helper method for making a PUT request
func (c *Client) PutWithContext(ctx context.Context, rel *url.URL, headers http.Header, body io.Reader) (*Response, error) {
	return c.DoWithContext(ctx, http.MethodPut, rel, headers, body)
}

// Post helper method for making a POST request
func (c *Client) Post(rel *url.URL, headers http.Header, body io.Reader) (*Response, error) {
	return c.Do(http.MethodPost, rel, headers, body)
}

// PostWithContext helper method for making a POST request
func (c *Client) PostWithContext(ctx context.Context, rel *url.URL, headers http.Header, body io.Reader) (*Response, error) {
	return c.DoWithContext(ctx, http.MethodPost, rel, headers, body)
}

// Delete helper method for making a DELETE request
func (c *Client) Delete(rel *url.URL, headers http.Header) (*Response, error) {
	return c.Do(http.MethodDelete, rel, headers, nil)
}

// DeleteWithContext helper method for making a DELETE request
func (c *Client) DeleteWithContext(ctx context.Context, rel *url.URL, headers http.Header) (*Response, error) {
	return c.DoWithContext(ctx, http.MethodDelete, rel, headers, nil)
}

// Head helper method for making a HEAD request
func (c *Client) Head(rel *url.URL, headers http.Header) (*Response, error) {
	return c.Do(http.MethodHead, rel, headers, nil)
}

// HeadWithContext helper method for making a HEAD request
func (c *Client) HeadWithContext(ctx context.Context, rel *url.URL, headers http.Header) (*Response, error) {
	return c.DoWithContext(ctx, http.MethodHead, rel, headers, nil)
}

func (c *Client) do(request *http.Request) (*Response, error) {
	defer func() {
		if request.Body != nil {
			request.Body.Close()
		}
	}()
	response, err := c.handle(request)
	if (err != nil || inRange(response.StatusCode, 500, 600)) && c.maxRetry > 0 {
		if err == context.Canceled {
			return response, err
		}

		e := log.Warn().Str("url", request.URL.String())
		if response != nil {
			e.Int("code", response.StatusCode)
		}
		e.Err(err).Msg("Error with request. Retrying...")

		retries := 0
		for retries < c.maxRetry {
			time.Sleep(c.retryDelay)

			retries++
			response, err = c.handle(request)
			if err != nil {
				continue
			}

			if !inRange(response.StatusCode, 500, 600) {
				return response, err
			}
		}

		e = log.Warn().Str("url", request.URL.String())
		if response != nil {
			e.Int("code", response.StatusCode)
		}
		e.Err(err).Int("retries", retries).Msg("Max retries reached")
	}
	return response, err
}

func (c *Client) handle(request *http.Request) (*Response, error) {
	m.inFlight.WithLabelValues(strings.ToLower(request.Method), request.URL.Host).Inc()
	defer m.inFlight.WithLabelValues(strings.ToLower(request.Method), request.URL.Host).Dec()

	start := time.Now()
	req := request.Clone(request.Context())
	if request.Body != nil && request.GetBody != nil {
		if body, err := request.GetBody(); err == nil {
			req.Body = ioutil.NopCloser(body)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		m.error.WithLabelValues(strings.ToLower(request.Method), request.URL.Host).Inc()
		if err, ok := err.(*url.Error); ok {
			return nil, err.Unwrap()
		}
		return nil, err
	}
	defer resp.Body.Close()
	m.status.WithLabelValues(strings.ToLower(request.Method), request.URL.Host, strconv.Itoa(resp.StatusCode)).Inc()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		m.error.WithLabelValues(request.Method, request.URL.Host).Inc()
		return nil, err
	}
	response := Response{
		Status:           resp.Status,
		StatusCode:       resp.StatusCode,
		Proto:            resp.Proto,
		ProtoMajor:       resp.ProtoMajor,
		ProtoMinor:       resp.ProtoMinor,
		Header:           resp.Header,
		Body:             body,
		ContentLength:    resp.ContentLength,
		TransferEncoding: resp.TransferEncoding,
		Uncompressed:     resp.Uncompressed,
		Trailer:          resp.Trailer,
		Duration:         time.Now().Sub(start),
		Request:          request,
	}
	m.duration.WithLabelValues(strings.ToLower(request.Method), request.URL.Host).Observe(response.Duration.Seconds())
	return &response, nil
}

// IsSuccessful checks if server code being passed is a successfully code
func IsSuccessful(code int) bool {
	return inRange(code, 200, 300)
}

// IsServerError checks if server code being passed is a server error code
func IsServerError(code int) bool {
	return inRange(code, 500, 600)
}

// IsClientError checks if server code being passed is a client error code
func IsClientError(code int) bool {
	return inRange(code, 400, 500)
}

func inRange(code, a, b int) bool {
	return a <= code && code < b
}

func getCertPool(paths []string) (*x509.CertPool, error) {
	cp := x509.NewCertPool()
	for _, p := range paths {
		if p != "" {
			caCert, err := ioutil.ReadFile(p)
			if err != nil {
				return cp, err
			}
			if !cp.AppendCertsFromPEM(caCert) {
				return cp, errors.New("unable to add ca bundle")
			}
		}
	}
	return cp, nil
}
