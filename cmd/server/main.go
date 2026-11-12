package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MatiasHernanSerrato/pulso-api/internal/auth"
	"github.com/MatiasHernanSerrato/pulso-api/internal/config"
	"github.com/MatiasHernanSerrato/pulso-api/internal/db"
	"github.com/MatiasHernanSerrato/pulso-api/internal/health"
	"github.com/MatiasHernanSerrato/pulso-api/internal/webhooks"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("no pude cargar la config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, db.DefaultPoolConfig(cfg.DatabaseURL))
	if err != nil {
		log.Fatalf("no pude conectar a la db: %v", err)
	}
	defer pool.Close()
	log.Println("db conectada")

	signer, err := auth.NewSigner(cfg.JWTSecret)
	if err != nil {
		log.Fatalf("no pude crear el signer de jwt: %v", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", health.Handler())

	// Webhooks externos. No pasan por auth JWT: validan con firma propia.
	// Mercado Pago aca; otros proveedores van a vivir al lado.
	mpHandler := webhooks.NewMercadoPagoHandler(cfg.MPWebhookSecret, webhooks.NoopSink{})
	r.Post("/webhooks/mercadopago", mpHandler.Handler())

	// Rutas /api/* siempre piden token. Mas adelante aca cuelga el handler
	// de graphql detras del middleware de auth.
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.Middleware(signer))
		r.Get("/me", func(w http.ResponseWriter, req *http.Request) {
			id, _ := auth.UserFromContext(req.Context())
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"user_id":"` + id.String() + `"}`))
		})
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("pulso-api [%s] escuchando en :%s", cfg.Env, cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("bajando server...")
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = shutdownCtx
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("listo, chau")
}
