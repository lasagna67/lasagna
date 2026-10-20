package server

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/lasagna67/lasagna.git/internal/config"
	"github.com/lasagna67/lasagna.git/pkg/logger"
)

type Server struct {
	echo   *echo.Echo
	http   *http.Server
	logger *logger.Logger
}

func NewServer(cfg config.Config, logger *logger.Logger) *Server {
	e := echo.New()
	//registerMiddleware(e, cfg, logger)

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
