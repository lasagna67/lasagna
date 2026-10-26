package server

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	echo   *echo.Echo
	http   *http.Server
	logger *logger.Logger
}

func NewServer(cfg config.Config, logger *logger.Logger) *Server {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			method := c.Request().Method
			route := c.Path()
			inFlight := requestMetrics.inFlight.WithLabelValues(method, route)
			inFlight.Inc()
			defer inFlight.Dec()

			requestID := c.Request().Header.Get(echo.HeaderXRequestID)
			if requestID == "" {
				requestID = uuid.NewString()
			}
			c.Response().Header().Set(echo.HeaderXRequestID, requestID)

			startedAt := time.Now()
			err := next(c)
			if err != nil {
				c.Echo().HTTPErrorHandler(c, err)
			}
			_, status := echo.ResolveResponseStatus(c.Response(), err)
			duration := time.Since(startedAt)
			observeRequest(method, route, status, duration)

			logger.Info(
				"http request completed",
				"method", method,
				"route", route,
				"status", status,
				"duration_ms", float64(duration)/float64(time.Millisecond),
				"request_id", requestID,
			)

			return err
		}
	})
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	srv := &http.Server{
		Addr:    cfg.HTTP.Host + ":" + cfg.HTTP.Port,
		Handler: e,

		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &Server{
		echo:   e,
		http:   srv,
		logger: logger,
	}
}

func (s *Server) Start() error {
	return s.http.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) Echo() *echo.Echo {
	return s.echo
}
