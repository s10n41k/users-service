package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// -------------------- METRICS --------------------

var (
	requests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "users_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	duration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "users_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"method", "path"},
	)

	inFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "users_http_requests_in_flight",
			Help: "Current number of in-flight requests",
		},
	)
)

// -------------------- RESPONSE WRITER --------------------

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// -------------------- PATH NORMALIZATION --------------------

func normalizePath(path string) string {
	parts := strings.Split(path, "/")

	for i, p := range parts {
		if _, err := strconv.Atoi(p); err == nil {
			parts[i] = ":id"
		}
	}

	return strings.Join(parts, "/")
}

// -------------------- MIDDLEWARE --------------------

func Middleware(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		inFlight.Inc()
		defer inFlight.Dec()

		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     200,
		}

		// выполняем основной handler
		h(rw, r)

		path := normalizePath(r.URL.Path)

		requests.WithLabelValues(
			r.Method,
			path,
			strconv.Itoa(rw.statusCode),
		).Inc()

		duration.WithLabelValues(
			r.Method,
			path,
		).Observe(time.Since(start).Seconds())
	}
}
