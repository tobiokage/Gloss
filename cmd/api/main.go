package main

import (
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gloss/internal/audit"
	"gloss/internal/auth"
	"gloss/internal/billing"
	"gloss/internal/bootstrap"
	"gloss/internal/catalogue"
	"gloss/internal/payments"
	"gloss/internal/payments/hdfc"
	platformconfig "gloss/internal/platform/config"
	platformdb "gloss/internal/platform/db"
	platformhttp "gloss/internal/platform/http"
	platformlogger "gloss/internal/platform/logger"
	"gloss/internal/shared/enums"
	"gloss/internal/shared/idempotency"
	"gloss/internal/staff"
)

func main() {
	cfg, err := platformconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := platformlogger.New(cfg)

	db, err := platformdb.NewPostgres(cfg)
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		logger.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	authRepo := auth.NewRepo(db)
	authService := auth.NewService(cfg, authRepo)
	authHandler := auth.NewHandler(authService)
	bootstrapRepo := bootstrap.NewRepo(db)
	bootstrapService := bootstrap.NewService(bootstrapRepo, true)
	bootstrapHandler := bootstrap.NewHandler(bootstrapService)
	auditRepo := audit.NewRepo(db)
	auditService := audit.NewService(auditRepo)
	idempotencyStore := idempotency.NewStore()
	hdfcClient := hdfc.NewClient(cfg.HDFC, nil)
	paymentsRepo := payments.NewRepo(db)
	paymentsService := payments.NewService(paymentsRepo, hdfcClient, auditService, logger.With("module", "payments"))
	billingRepo := billing.NewRepo(db)
	billingService := billing.NewService(db, billingRepo, idempotencyStore, auditService, logger.With("module", "billing"), paymentsService)
	billingHandler := billing.NewHandler(billingService)
	catalogueRepo := catalogue.NewRepo(db)
	catalogueService := catalogue.NewService(catalogueRepo)
	catalogueHandler := catalogue.NewHandler(catalogueService)
	staffRepo := staff.NewRepo(db)
	staffService := staff.NewService(staffRepo)
	staffHandler := staff.NewHandler(staffService)
	authMiddleware := auth.Middleware(cfg)
	superAdminOnlyMiddleware := func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			authCtx, err := auth.AuthContextFromContext(r.Context())
			if err != nil {
				platformhttp.WriteError(w, err)
				return
			}

			if err := auth.RequireRole(authCtx, enums.RoleSuperAdmin); err != nil {
				platformhttp.WriteError(w, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
	storeManagerOnlyMiddleware := func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			authCtx, err := auth.AuthContextFromContext(r.Context())
			if err != nil {
				platformhttp.WriteError(w, err)
				return
			}

			if err := auth.RequireRole(authCtx, enums.RoleStoreManager); err != nil {
				platformhttp.WriteError(w, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	router := platformhttp.NewRouter(
		authHandler.Login,
		authMiddleware,
		superAdminOnlyMiddleware,
		storeManagerOnlyMiddleware,
		bootstrapHandler.GetStoreBootstrap,
		billingHandler.CreateBill,
		billingHandler.GetBill,
		billingHandler.CancelBill,
		billingHandler.RetryOnlinePayment,
		billingHandler.CancelPaymentAttempt,
		catalogueHandler.ListCatalogueItems,
		catalogueHandler.CreateCatalogueItem,
		catalogueHandler.UpdateCatalogueItem,
		catalogueHandler.DeactivateCatalogueItem,
		staffHandler.ListStaff,
		staffHandler.CreateStaff,
		staffHandler.DeactivateStaff,
		staffHandler.AssignStaffToStore,
	)

	server := &nethttp.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", server.Addr, "env", cfg.AppEnv)
		serverErrCh <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
