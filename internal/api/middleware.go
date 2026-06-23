package api

import (
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"testlink/internal/auth"
	"testlink/internal/ratelimit"
)

type Middleware struct {
	auth *auth.Service
	rl   *ratelimit.Limiter
	rlCfg ratelimit.Config
}

func NewMiddleware(auth *auth.Service, rl *ratelimit.Limiter, rlCfg ratelimit.Config) *Middleware {
	return &Middleware{auth: auth, rl: rl, rlCfg: rlCfg}
}

// AdminAuth validates JWT from Authorization header or cookie.
func (m *Middleware) AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Support Authorization header
		authHeader := c.GetHeader("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			// Fallback to x-admin-token header (simple mode)
			token = c.GetHeader("x-admin-token")
		}
		if token == "" {
			token, _ = c.Cookie("admin_token")
		}
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		// Try JWT first, then raw token
		_, err := m.auth.Validate(token)
		if err != nil {
			// Try raw admin token
			if !m.auth.ValidateToken(token) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// RateLimitSession limits session creation.
func (m *Middleware) RateLimitSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := clientIP(c)
		if err := m.rl.AllowSession(c.Request.Context(), ip, m.rlCfg); err != nil {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			c.Abort()
			return
		}
		if err := m.rl.AllowGlobal(c.Request.Context(), m.rlCfg); err != nil {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RateLimitReport limits report uploads.
func (m *Middleware) RateLimitReport() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := clientIP(c)
		if err := m.rl.AllowReport(c.Request.Context(), ip, m.rlCfg); err != nil {
			log.Printf("rate limited report from %s", ip)
			// Don't block reports — just log and continue
		}
		c.Next()
	}
}

// clientIP extracts the real client IP, respecting X-Forwarded-For.
func clientIP(c *gin.Context) string {
	if c.ClientIP() != "" {
		return c.ClientIP()
	}
	remoteIP := c.Request.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		return host
	}
	return remoteIP
}
