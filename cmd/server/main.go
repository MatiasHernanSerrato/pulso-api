package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/MatiasHernanSerrato/pulso-api/internal/metrics"
	"github.com/MatiasHernanSerrato/pulso-api/internal/webhooks"
)

// Build info (poblado por -ldflags).
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("no pude cargar la config: %v", err)
	}

	switch cmd {
	case "serve":
		runServe(cfg)
	case "worker":
		runWorker(cfg)
	case "migrate":
		runMigrate(cfg, os.Args[2:])
	case "version":
		fmt.Printf("pulso-api %s (commit %s, built %s)\n", version, commit, buildDate)
	default:
		log.Fatalf("subcomando desconocido: %q. Uso: pulso-api [serve|worker|migrate|version]", cmd)
	}
}

func runServe(cfg *config.Config) {
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
	r.Use(metrics.Middleware(func(req *http.Request) string {
		if rc := chi.RouteContext(req.Context()); rc != nil {
			return rc.RoutePattern()
		}
		return req.URL.Path
	}))

	r.Get("/healthz", health.Handler())
	r.Method(http.MethodGet, "/metrics", metrics.Handler())

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
		log.Printf("pulso-api %s [%s] escuchando en :%s", version, cfg.Env, cfg.Port)
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("listo, chau")
}

func runWorker(cfg *config.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.NewPool(ctx, db.DefaultPoolConfig(cfg.DatabaseURL))
	if err != nil {
		log.Fatalf("worker: no pude conectar a la db: %v", err)
	}
	defer pool.Close()
	log.Printf("worker %s arrancando en env=%s", version, cfg.Env)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Loop simple: cada 60s corremos un tick de deteccion de anomalias.
	// Cuando lleguen mas jobs (mp outbox, notifs), paso a un scheduler real.
	tick := time.NewTicker(60 * time.Second)
	defer tick.Stop()

	log.Println("worker listo, esperando ticks")
	for {
		select {
		case <-tick.C:
			// TODO: llamar al service de anomaly aca cuando este listo
			// para correr en worker separado. Por ahora solo heartbeat.
			log.Println("worker tick")
		case <-stop:
			log.Println("worker bajando")
			return
		}
	}
}

func runMigrate(cfg *config.Config, args []string) {
	direction := "up"
	if len(args) > 0 {
		direction = args[0]
	}

	// Permito pasar la source por env para tests, default a lo que va en la
	// imagen de docker (`db/migrations/` copiado al working dir).
	source := os.Getenv("MIGRATIONS_URL")
	if source == "" {
		source = "file://db/migrations"
	}

	switch direction {
	case "up":
		if err := db.MigrateUp(source, cfg.DatabaseURL); err != nil {
			log.Fatalf("migrate up: %v", err)
		}
		log.Println("migrations aplicadas")
	case "down":
		if err := db.MigrateDown(source, cfg.DatabaseURL); err != nil {
			log.Fatalf("migrate down: %v", err)
		}
		log.Println("rollback un step ok")
	case "version":
		v, dirty, err := db.MigrateVersion(source, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("migrate version: %v", err)
		}
		log.Printf("version=%d dirty=%v", v, dirty)
	default:
		log.Fatalf("direction desconocida: %q (usa up|down|version)", direction)
	}
}
