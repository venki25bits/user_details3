package http

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/machinebox/graphql"
)

// GraphQL client
type GraphQL struct {
	client  *graphql.Client
	url     *url.URL
	headers http.Header
}

// Run GraphQL query
func (c *GraphQL) Run(ctx context.Context, req *graphql.Request, resp interface{}) error {
	start := time.Now()
	m.inFlight.WithLabelValues("graphql", c.url.Host).Inc()
	defer m.inFlight.WithLabelValues(strings.ToLower(http.MethodPost), c.url.Host).Dec()
	defer m.duration.WithLabelValues(strings.ToLower(http.MethodPost), c.url.Host).Observe(time.Now().Sub(start).Seconds())

	headers := req.Header
	for k, vs := range c.headers {
		for _, v := range vs {
			headers.Add(k, v)
		}
	}
	req.Header = headers
	err := c.client.Run(ctx, req, resp)
	if err != nil {
		m.error.WithLabelValues(strings.ToLower(http.MethodPost), c.url.Host).Inc()
	}
	return err
}
