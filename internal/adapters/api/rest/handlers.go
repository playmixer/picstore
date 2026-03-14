package rest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"picstore/internal/adapters/apperror"
	"picstore/internal/adapters/models"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/playmixer/single-auth/pkg/authtools"
	"go.uber.org/zap"
)

var (
	pageSize int = 10
)

func (s *Server) handlerSSOLogin(ctx *gin.Context) {
	bParams, err := base64.RawStdEncoding.DecodeString(ctx.Query("paramsURI"))
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": err.Error(),
		})
		return
	}
	if s.ssoAuthCert != "" {
		bParams, err = authtools.DecryptDataRSA(bParams, s.certData)
		if err != nil {
			ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"error": err.Error(),
			})
			return
		}
	}

	mapParams := map[string]string{}
	err = json.Unmarshal(bParams, &mapParams)
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": err.Error(),
		})
		return
	}

	user, err := s.auth.GetUserByEmail(ctx.Request.Context(), mapParams["email"])
	if err != nil && !errors.Is(err, apperror.ErrNotFoundData) {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": err.Error(),
		})
		return
	}
	if err != nil && errors.Is(err, apperror.ErrNotFoundData) {
		user = &models.User{}
		user.Email = mapParams["email"]
		user.Username = mapParams["username"]
		user.RegSource = "ssomix"
		if user, err = s.auth.NewUser(ctx.Request.Context(), user); err != nil {
			ctx.HTML(http.StatusBadRequest, "error.html", gin.H{
				"error": err.Error(),
			})
			return
		}
	}

	if r, ok := mapParams["roles"]; ok {
		user.Roles = []models.Role{}
		roles := strings.Split(r, ",")
		for _, line := range roles {
			if roleName, ok := models.MapRole[line]; ok {
				user.Roles = append(user.Roles, models.Role{
					Name: roleName,
				})
			}
		}
	}
	user, err = s.auth.UpdRoles(ctx.Request.Context(), user)
	if err != nil {
		s.log.Error("failed upd user roles", zap.Error(err))
	}

	token, err := authtools.CreateJWT([]byte(s.secretKey), map[string]string{
		"userID": strconv.Itoa(int(user.ID)),
	})
	if err != nil {
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": err.Error(),
		})
		return
	}

	for _, domain := range s.cookieDomain {
		ctx.SetCookie(CookieJWT, token, s.cookieLifeTime, "/", domain, s.cookieSecure, true)
	}
	ctx.Header("Cache-Control", "no-cache")
	ctx.Redirect(http.StatusMovedPermanently, "/")
}

func (s *Server) handlerMain(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"user": s.getUser(c),
	})
}

func (s *Server) handlerAbout(c *gin.Context) {
	c.HTML(http.StatusOK, "about.html", gin.H{
		"user": s.getUser(c),
	})
}

func (s *Server) handlerProfile(c *gin.Context) {
	c.HTML(http.StatusOK, "profile/index.html", gin.H{
		"user": s.getUser(c),
	})
}

func (s *Server) handlerUpload(c *gin.Context) {
	c.HTML(http.StatusOK, "profile/upload.html", gin.H{
		"user": s.getUser(c),
	})
}

func (s *Server) handlerUploadPost(c *gin.Context) {
	user := s.getUser(c)
	formType := c.PostForm("type")
	isPublic := c.PostForm("is_public") == "on"
	tags := c.PostForm("tags")
	s.log.Debug("upload",
		zap.String("type", formType),
		zap.Bool("isPublic", isPublic),
		zap.String("tags", tags),
	)

	if formType == "file" {
		file, err := c.FormFile("file")
		if err != nil {
			s.log.Debug("failed getting form file", zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("ошибка получения файла")))
			return
		}
		_, err = s.pic.UploadImgFile(c.Request.Context(), user.ID, file, isPublic, tags)
		if err != nil {
			s.log.Error("failed save file", zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить файл")))
			return
		}
	} else {
		link := c.PostForm("url")
		_, err := s.pic.UploadImgURL(c.Request.Context(), user.ID, link, isPublic, tags)
		if err != nil {
			s.log.Error("failed save file from url", zap.String("url", link), zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить файл")))
			return
		}
	}

	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?info=%s", url.QueryEscape("файл загружен")))
}

func (s *Server) handlerPosts(c *gin.Context) {
	posts, err := s.pic.GetPosts(c.Request.Context())
	if err != nil {
		s.log.Error("failed get posts", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/posts?error=%s", url.QueryEscape("не удалось получить посты")))
		return
	}

	page := c.Query("page")
	if page == "" {
		page = "1"
	}
	curPage := atoi(page)
	if curPage < 1 {
		curPage = 1
	}

	start, end, total := pagify(len(posts), pageSize, curPage)
	// Корректируем curPage на случай, если pagify изменила его (например, если curPage > total)
	// pagify уже скорректировала curPage внутри, но мы используем переданный curPage для prev/next
	// Вычислим актуальный curPage на основе start (start = (curPage-1)*pageSize)
	// Но проще использовать curPage, который мы передали.
	prev := curPage - 1
	if prev < 1 {
		prev = 0
	}
	next := curPage + 1
	if next > total {
		next = 0
	}
	c.HTML(http.StatusOK, "posts.html", gin.H{
		"posts": posts[start:end],
		"user":  s.getUser(c),
		"pagination": gin.H{
			"total": total,
			"cur":   curPage,
			"prev":  prev,
			"next":  next,
		},
	})
}

// GET /view/:y/:m/:d/:h/:filename
func (s *Server) handlerView(c *gin.Context) {
	user := s.getUser(c)
	year := c.Param("y")
	month := c.Param("m")
	day := c.Param("d")
	hour := c.Param("h")
	filename := c.Param("filename")
	view := c.Query("view")

	s.log.Debug("view",
		zap.String("y", year),
		zap.String("m", month),
		zap.String("d", day),
		zap.String("h", hour),
		zap.String("filename", filename),
	)

	path := path.Join(year, month, day, hour, filename)

	img, err := s.pic.GetImg(c.Request.Context(), path)
	if err != nil {
		c.HTML(http.StatusBadRequest, "view.html", gin.H{
			"error": "Файл не найден",
			"user":  user,
		})
		return
	}

	if view == "file" {
		c.Data(http.StatusOK, "image/"+img.Extension, img.GetData())
		return
	}

	images, err := s.pic.GetPosts(c.Request.Context())
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": "Ошибка получения файла",
		})
		return
	}

	for _, row := range images {
		if row.ID == img.ID {
			img = row
			break
		}
	}

	prev := ""
	if img.Prev != nil {
		prev = fmt.Sprintf("/view/%s", img.Prev.Path)
	}
	next := ""
	if img.Next != nil {
		next = fmt.Sprintf("/view/%s", img.Next.Path)
	}
	c.HTML(http.StatusOK, "view.html", gin.H{
		"user":  user,
		"image": fmt.Sprintf("/image/%s/%s/%s/%s/%s", year, month, day, hour, filename),
		"img":   img,
		"prev":  prev,
		"next":  next,
	})
}
