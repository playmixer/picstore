package rest

import (
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger middleware логирования.
func (s *Server) middlewareLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		s.log.Info(
			"Request information",
			zap.String("uri", c.Request.RequestURI),
			zap.Duration("duration", time.Since(start)),
			zap.String("method", c.Request.Method),
			zap.Int("status", c.Writer.Status()),
			zap.Int("size", c.Writer.Size()),
		)
	}
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		_, err := s.checkAuth(ctx)
		if err != nil {
			authURL, err := s.buildAuthURL(ctx)
			if err != nil {
				s.log.Error("failed to build auth URL", zap.Error(err))
				ctx.Header("Cache-Control", "no-cache")
				ctx.Redirect(http.StatusMovedPermanently, s.ssoAuthURL)
				ctx.Abort()
				return
			}
			ctx.Header("Cache-Control", "no-cache")
			ctx.Redirect(http.StatusMovedPermanently, authURL)
			ctx.Abort()
		}
		ctx.Next()
	}
}

// Auth middleware проверка аутентификации пользователя.
func (s *Server) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, err := s.checkAuth(c)
		if err != nil {
			c.Writer.WriteHeader(http.StatusUnauthorized)
			c.Abort()
		}

		c.Next()
	}
}

// TrustedSubnet middleware проверка сети пользователя как доверенную.
func (s *Server) TrustedSubnet() gin.HandlerFunc {
	return func(c *gin.Context) {
		access := true
		network, err := netip.ParsePrefix(s.trustedSubnet)
		if err != nil {
			s.log.Debug("trusted subnet is not valid", zap.Error(err), zap.String("subnet", s.trustedSubnet))
			access = false
		}
		ipStr := c.Request.Header.Get("X-Real-IP")
		ip, err := netip.ParseAddr(ipStr)
		if err != nil {
			s.log.Debug("IP address is not valid", zap.Error(err), zap.String("ip", ipStr))
			access = false
		}
		if ok := network.Contains(ip); !ok {
			access = false
		}

		if !access {
			c.Writer.WriteHeader(http.StatusForbidden)
			c.Abort()
		}
		c.Next()
	}
}

func (s *Server) middlewareImageAccess(c *gin.Context) {
	user := s.getUser(c)
	imgPath := strings.Replace(c.Request.RequestURI, "/image/", "", 1)

	img, err := s.pic.GetImg(c.Request.Context(), imgPath)
	if err != nil {
		c.HTML(http.StatusNotFound, "404.html", gin.H{
			"error": err.Error(),
		})
		c.Abort()
		return
	}
	if !img.IsPublic && img.UserID != user.ID {
		c.HTML(http.StatusForbidden, "error.html", gin.H{
			"error": "Forbidden",
		})
		c.Abort()
	}

	s.log.Debug("image", zap.String("path", c.Request.RequestURI), zap.Any("user", user))
	c.Next()
}

// PublisherMiddleware проверяет, что у пользователя есть роль publisher.
func (s *Server) PublisherMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := s.getUser(c)
		if user.ID == 0 {
			c.Writer.WriteHeader(http.StatusUnauthorized)
			c.Abort()
			return
		}
		if !user.IsPublisher() {
			c.Writer.WriteHeader(http.StatusForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}
