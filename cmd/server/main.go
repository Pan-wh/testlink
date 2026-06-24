package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"testlink/internal/api"
	"testlink/internal/auth"
	"testlink/internal/geoip"
	"testlink/internal/probe"
	"testlink/internal/ratelimit"
	"testlink/internal/session"
	"testlink/internal/store"
	"testlink/internal/target"
)

func connectClickHouseWithRetry(cfg *Config, timeout time.Duration) (*store.ClickHouse, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ch, err := store.New(cfg.ClickHouse.Host, cfg.ClickHouse.Port, cfg.ClickHouse.Database, cfg.ClickHouse.Username, cfg.ClickHouse.Password)
		if err == nil {
			return ch, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		log.Printf("clickhouse not ready, retrying: %v", err)
		time.Sleep(2 * time.Second)
	}
}

func warnWeakAuth(cfg *Config) {
	if cfg.Auth.AdminToken == "" || cfg.Auth.JWTSecret == "" {
		log.Println("WARN auth.admin_token or auth.jwt_secret is empty; set strong values before production use")
		return
	}
	if cfg.Auth.AdminToken == "testlink123!" || cfg.Auth.JWTSecret == "testlink123!" {
		log.Println("WARN default auth token/secret is in use; change TESTLINK_ADMIN_TOKEN and TESTLINK_JWT_SECRET for production")
	}
}

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// GeoIP
	geo, err := geoip.New(cfg.GeoIP.IP2RegionV4, cfg.GeoIP.IP2RegionV6, cfg.GeoIP.MaxmindCountry, cfg.GeoIP.MaxmindASN)
	if err != nil {
		log.Fatalf("geoip: %v", err)
	}
	defer geo.Close()
	log.Println("geoip loaded")

	warnWeakAuth(cfg)

	// ClickHouse
	ch, err := connectClickHouseWithRetry(cfg, 30*time.Second)
	if err != nil {
		log.Fatalf("clickhouse: %v", err)
	}
	defer ch.Conn().Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ch.Init(ctx); err != nil {
		log.Fatalf("clickhouse init: %v", err)
	}
	log.Println("clickhouse ready")

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("redis unavailable (rate limiting disabled): %v", err)
		// Continue without Redis — rate limiting will be skipped
	} else {
		log.Println("redis ready")
	}

	// Services
	authSvc := auth.New(cfg.Auth.JWTSecret, cfg.Auth.AdminToken)
	rlSvc := ratelimit.New(rdb)
	sessSvc := session.New(ch, geo)
	targetSvc := target.New(ch)
	probeSvc := probe.New(ch, sessSvc)
	handler := api.NewHandler(ch, sessSvc, targetSvc, probeSvc)
	mw := api.NewMiddleware(authSvc, rlSvc, cfg.RateLimit)

	// Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	// Trust X-Forwarded-For headers for client IP.
	proxies := cfg.Server.TrustedProxyCIDRs
	if len(proxies) == 0 && cfg.Server.TrustedProxies > 0 {
		proxies = make([]string, cfg.Server.TrustedProxies)
		for i := range proxies {
			proxies[i] = "0.0.0.0/0"
		}
	}
	if len(proxies) > 0 {
		if err := r.SetTrustedProxies(proxies); err != nil {
			log.Fatalf("set trusted proxies: %v", err)
		}
	}

	// Player routes (public)
	r.GET("/api/session", mw.RateLimitSession(), handler.CreateSession)
	r.POST("/api/report", mw.RateLimitReport(), handler.SubmitReport)

	// Admin routes
	admin := r.Group("/admin")
	admin.POST("/login", func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		jwt, err := authSvc.Login(req.Token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"access_token": jwt})
	})
	admin.Use(mw.AdminAuth())
	{
		admin.GET("/targets", handler.ListTargets)
		admin.POST("/targets", handler.CreateTarget)
		admin.PUT("/targets/:id", handler.UpdateTarget)
		admin.DELETE("/targets/:id", handler.DeleteTarget)
		admin.GET("/sessions", handler.ListSessions)
		admin.GET("/sessions/:id", handler.GetSession)
		admin.PATCH("/sessions/:id", handler.UpdateSessionNote)
	}

	// Static files (player page + admin)
	r.StaticFile("/", "web/player/index.html")
	r.StaticFile("/player", "web/player/index.html")
	r.StaticFile("/admin-page", "web/admin/index.html")

	// Start
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Printf("testlink server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped")
}
