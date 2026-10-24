package app

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	authhandler "github.com/lasagna67/lasagna.git/internal/auth/handler"
	companyhandler "github.com/lasagna67/lasagna.git/internal/company/handler"
	"github.com/lasagna67/lasagna.git/internal/config"
	credithandler "github.com/lasagna67/lasagna.git/internal/credit/handler"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	userhandler "github.com/lasagna67/lasagna.git/internal/user/handler"
	"github.com/lasagna67/lasagna.git/pkg/logger"
	"github.com/lasagna67/lasagna.git/pkg/server"
)

type Delivery struct {
	server *server.Server
}

func NewDelivery(cfg config.Config, services *Service, logger *logger.Logger) *Delivery {
	srv := server.NewServer(cfg, logger.Named("http"))

	e := srv.Echo()
	e.GET("/health", healthCheck)
	api := e.Group("/api/v0")

	auth := authhandler.New(services.Auth, logger.Named("auth"))
	auth.RegisterRoutes(api)

	users := userhandler.New(services.User, logger.Named("user"))
	users.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleAdmin))

	companies := companyhandler.New(services.Company, logger.Named("company"))
	companies.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))

	applications := credithandler.New(services.Credit, logger.Named("credit"))
	applications.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))

	products := creditproducts.NewHandler(services.CreditProducts, logger.Named("credit-products"))
	products.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))

	return &Delivery{
		server: srv,
	}
}

// healthCheck — простой liveness-эндпоинт для контейнерного healthcheck.
func healthCheck(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Delivery) Start() error {
	return d.server.Start()
}

func (d *Delivery) Stop(ctx context.Context) error {
	return d.server.Stop(ctx)
}
