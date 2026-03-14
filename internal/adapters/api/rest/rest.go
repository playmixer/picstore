package rest

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"picstore/internal/adapters/models"
	"picstore/internal/adapters/storage/types"
	"picstore/internal/core/picstore"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/playmixer/single-auth/pkg/logger"
	"go.uber.org/zap"
)

const (
	ContentLength   string = "Content-Length"   // заголовок длины конетента
	ContentType     string = "Content-Type"     // заколовок типа контент
	ApplicationJSON string = "application/json" // json контент

	CookieJWT          string = "singleauth_token" // поле хранения токента
	CookieRefreshToken string = "singleauth_refresh"
)

var (
	shutdownDelay = time.Second * 5
)

type PicStore interface {
	UploadImgFile(ctx context.Context, userID uint, f *multipart.FileHeader, isPublic bool, tags string) (*picstore.PicImage, error)
	UploadImgURL(ctx context.Context, userID uint, url string, isPublic bool, tags string) (*picstore.PicImage, error)
	GetImg(ctx context.Context, path string) (*picstore.PicImage, error)

	GetPosts(ctx context.Context) ([]*picstore.PicImage, error)
	GetPostsWithTags(ctx context.Context, tags string) ([]*picstore.PicImage, error)
	GetPostsPage(ctx context.Context, page, pageSize int, tags string) ([]*picstore.PicImage, error)
	GetTagsWithCount(ctx context.Context) (map[string]int, error)
	GetUserPosts(ctx context.Context, userID uint) ([]*picstore.PicImage, error)
	UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error
	DeleteImage(ctx context.Context, userID uint, imageID uint) error
}

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	GetH(ctx context.Context, key string, obj types.ObjInterface) (err error)
	SetH(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error
}

type AuthManager interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUser(ctx context.Context, userID uint) (*models.User, error)
	NewUser(ctx context.Context, user *models.User) (*models.User, error)
	UpdRoles(ctx context.Context, user *models.User) (*models.User, error)
}

type Server struct {
	pic            PicStore
	auth           AuthManager
	log            *logger.Logger
	cache          Cache
	baseURL        string
	trustedSubnet  string
	secretKey      []byte
	cookieDomain   []string
	cookieSecure   bool
	cookieLifeTime int
	s              http.Server
	tlsEnable      bool
	ssoAuthURL     string
	ssoAuthCert    string
	certData       []byte
	imgPath        string
}

type Option func(s *Server)

// New создает Server.
func New(pic PicStore, auth AuthManager, cache Cache, log *logger.Logger, options ...Option) *Server {
	srv := &Server{
		pic:       pic,
		auth:      auth,
		cache:     cache,
		log:       log,
		secretKey: []byte("rest_secret_key"),
		imgPath:   "./tmp",
	}
	srv.s.Addr = "localhost:8080"

	for _, opt := range options {
		opt(srv)
	}

	if srv.ssoAuthCert != "" {
		f, err := os.Open(srv.ssoAuthCert)
		if err != nil {
			log.Error("failed open certificate", zap.Error(err), zap.String("file", srv.ssoAuthCert))
		}

		data, err := io.ReadAll(f)
		if err != nil {
			log.Error("failed read certificate", zap.Error(err), zap.String("file", srv.ssoAuthCert))
		}
		f.Close()

		srv.certData = data
	}

	return srv
}

func SetImgPath(path string) func(*Server) {
	return func(s *Server) {
		s.imgPath = path
	}
}

func BaseURL(url string) func(*Server) {
	return func(s *Server) {
		s.baseURL = url
	}
}

// Addr - Насткройка сервера, задает адрес сервера.
func Addr(addr string) func(s *Server) {
	return func(s *Server) {
		s.s.Addr = addr
	}
}

// SecretKey - задает секретный ключ.
func SecretKey(secret []byte) Option {
	return func(s *Server) {
		s.secretKey = secret
	}
}

// HTTPSEnable - включает https.
func HTTPSEnable(enable bool) Option {
	return func(s *Server) {
		s.tlsEnable = enable
	}
}

func SetCookieDomain(domain []string) Option {
	return func(s *Server) {
		s.cookieDomain = domain
	}
}

func SetCookieSecure(secure bool) Option {
	return func(s *Server) {
		s.cookieSecure = secure
	}
}

func SetCookieLifeTime(ttl int) Option {
	return func(s *Server) {
		s.cookieLifeTime = ttl
	}
}

func SetSSOAuth(url, cert string) Option {
	return func(s *Server) {
		s.ssoAuthURL = url
		s.ssoAuthCert = cert
	}
}

func (s *Server) SetupRouter() *gin.Engine {
	r := gin.New()

	r.Use(
		s.middlewareLogger(),
	)
	r.SetFuncMap(template.FuncMap{
		"datetimeF":  datetimeF,
		"genNumbers": genNumbers,
		"afterI":     afterI,
		"beforeI":    beforeI,
		"split":      strings.Split,
	})
	r.LoadHTMLGlob("templates/**/*")
	r.Static("/css", "./static/css")
	r.Static("/js", "./static/js")

	image := r.Group("/image")
	image.Use(s.middlewareImageAccess)
	{
		image.Static("/", s.imgPath)
	}
	r.GET("/sso/auth", func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-cache")
		ctx.Redirect(http.StatusMovedPermanently, s.ssoAuthURL)
	})
	r.GET("/sso/login", s.handlerSSOLogin)
	r.GET("/sso/logout", func(ctx *gin.Context) {
		for _, domain := range s.cookieDomain {
			ctx.SetCookie(CookieJWT, "", 0, "/", domain, s.cookieSecure, true)
		}
		ctx.Header("Cache-Control", "no-cache")
		ctx.Redirect(http.StatusMovedPermanently, "/")
	})

	r.GET("/", s.handlerMain)
	r.GET("/about", s.handlerAbout)
	r.GET("/posts", s.handlerPosts)
	r.GET("/api/tags", s.handlerTagsAutocomplete)
	r.GET("/view/:y/:m/:d/:h/:filename", s.handlerView)

	auth := r.Group("/")
	auth.Use(s.authMiddleware())
	{
		auth.GET("/i", s.handlerProfile)
		auth.GET("/i/upload", s.handlerUpload)
		auth.POST("/i/upload", s.handlerUploadPost)
		auth.GET("/i/posts", s.handlerUserPosts)
		auth.POST("/view/:y/:m/:d/:h/:filename/update", s.handlerUpdateImage)
		auth.POST("/view/:y/:m/:d/:h/:filename/delete", s.handlerDeleteImage)
	}

	return r
}

func (s *Server) Run() error {
	s.s.Handler = s.SetupRouter().Handler()
	if err := s.s.ListenAndServe(); err != nil {
		return fmt.Errorf("server has failed: %w", err)
	}

	return nil
}

// Stop - остановка сервера.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownDelay)
	defer cancel()
	err := s.s.Shutdown(ctx)
	if err != nil {
		s.log.Error("failed shutdown server", zap.Error(err))
	}
	s.log.Info("Server exiting")
}
