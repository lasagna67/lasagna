package app

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"
	authdomain "github.com/lasagna67/lasagna.git/internal/auth/domain"
	authhandler "github.com/lasagna67/lasagna.git/internal/auth/handler"
	collateralhandler "github.com/lasagna67/lasagna.git/internal/collateral/handler"
	companyhandler "github.com/lasagna67/lasagna.git/internal/company/handler"
	"github.com/lasagna67/lasagna.git/internal/config"
	creditproducts "github.com/lasagna67/lasagna.git/internal/credit_products"
	documenthandler "github.com/lasagna67/lasagna.git/internal/document/handler"
	financialscsvhandler "github.com/lasagna67/lasagna.git/internal/financials-csv/handler"
	intakehandler "github.com/lasagna67/lasagna.git/internal/intake/handler"
	offerhandler "github.com/lasagna67/lasagna.git/internal/offer/handler"
	underwritinghandler "github.com/lasagna67/lasagna.git/internal/underwriting/handler"
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

	uploads := financialscsvhandler.New(services.Financials, logger.Named("financials-csv"))
	uploads.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))
	collateral := collateralhandler.New(services.Collateral, logger.Named("collateral"))
	collateral.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleUnderwriter, authdomain.RoleAdmin))
	intake := intakehandler.New(services.Intake, logger.Named("intake"))
	intake.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))

	products := creditproducts.NewHandler(services.CreditProducts, logger.Named("credit-products"))
	products.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))

	underwriting := underwritinghandler.New(services.Underwriting, logger.Named("underwriting"))
	underwriting.RegisterRoutes(api, auth.Authenticate,
		auth.RequireRole(authdomain.RoleClient),
		auth.RequireRole(authdomain.RoleUnderwriter, authdomain.RoleAdmin),
	)

	offers := offerhandler.New(services.Offer, logger.Named("offer"))
	offers.RegisterRoutes(api, auth.Authenticate, auth.RequireRole(authdomain.RoleClient))

	documents := documenthandler.New(services.Document, logger.Named("document"))
	documents.RegisterRoutes(api, auth.Authenticate)

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
