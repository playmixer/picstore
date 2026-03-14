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
	"picstore/internal/core/picstore"
	"sort"
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
	encryptionKey := c.PostForm("encryption_key")
	s.log.Debug("upload",
		zap.String("type", formType),
		zap.Bool("isPublic", isPublic),
		zap.String("tags", tags),
		zap.Bool("hasEncryptionKey", encryptionKey != ""),
	)

	if formType == "file" {
		file, err := c.FormFile("file")
		if err != nil {
			s.log.Debug("failed getting form file", zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("ошибка получения файла")))
			return
		}
		_, err = s.pic.UploadImgFile(c.Request.Context(), user.ID, file, isPublic, tags, encryptionKey)
		if err != nil {
			s.log.Error("failed save file", zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить файл")))
			return
		}
	} else {
		link := c.PostForm("url")
		_, err := s.pic.UploadImgURL(c.Request.Context(), user.ID, link, isPublic, tags, encryptionKey)
		if err != nil {
			s.log.Error("failed save file from url", zap.String("url", link), zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить файл")))
			return
		}
	}

	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?info=%s", url.QueryEscape("файл загружен")))
}

func (s *Server) handlerPosts(c *gin.Context) {
	tagsParam := strings.TrimSpace(c.Query("tags"))
	page := c.Query("page")
	if page == "" {
		page = "1"
	}
	curPage := atoi(page)
	if curPage < 1 {
		curPage = 1
	}

	// Получаем страницу постов через GetPostsPage
	pagePosts, err := s.pic.GetPostsPage(c.Request.Context(), curPage, pageSize, tagsParam)
	if err != nil {
		s.log.Error("failed get posts page", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/posts?error=%s", url.QueryEscape("не удалось получить посты")))
		return
	}

	// Для пагинации нужно общее количество постов (с фильтром по тегам)
	var allPosts []*picstore.PicImage
	if tagsParam == "" {
		allPosts, err = s.pic.GetPosts(c.Request.Context())
	} else {
		allPosts, err = s.pic.GetPostsWithTags(c.Request.Context(), tagsParam)
	}
	if err != nil {
		s.log.Error("failed get total posts", zap.Error(err))
		// но мы уже имеем pagePosts, можно продолжить с нулевым total
		allPosts = []*picstore.PicImage{}
	}
	total := len(allPosts)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	// Корректируем curPage, если он превышает totalPages
	if curPage > totalPages {
		curPage = totalPages
	}
	prev := curPage - 1
	if prev < 1 {
		prev = 0
	}
	next := curPage + 1
	if next > totalPages {
		next = 0
	}
	c.HTML(http.StatusOK, "posts.html", gin.H{
		"posts": pagePosts,
		"user":  s.getUser(c),
		"tags":  tagsParam,
		"pagination": gin.H{
			"total": totalPages,
			"cur":   curPage,
			"prev":  prev,
			"next":  next,
		},
	})
}

// GET /api/tags - автодополнение тегов
func (s *Server) handlerTagsAutocomplete(c *gin.Context) {
	prefix := strings.ToLower(strings.TrimSpace(c.Query("prefix")))
	tagsMap, err := s.pic.GetTagsWithCount(c.Request.Context())
	if err != nil {
		s.log.Error("failed get tags with count", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось получить теги"})
		return
	}

	type tagItem struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	result := []tagItem{}

	for tag, count := range tagsMap {
		if prefix == "" || strings.HasPrefix(strings.ToLower(tag), prefix) {
			result = append(result, tagItem{Tag: tag, Count: count})
		}
	}

	// Сортируем по убыванию количества (самые популярные первые)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Tag < result[j].Tag
		}
		return result[i].Count > result[j].Count
	})

	// Ограничим количество, чтобы не отдавать слишком много
	if len(result) > 20 {
		result = result[:20]
	}

	c.JSON(http.StatusOK, gin.H{"tags": result})
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
	tagsParam := strings.TrimSpace(c.Query("tags"))
	key := c.Query("key")

	s.log.Debug("view",
		zap.String("y", year),
		zap.String("m", month),
		zap.String("d", day),
		zap.String("h", hour),
		zap.String("filename", filename),
		zap.String("tags", tagsParam),
		zap.String("key", key),
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
		// Если изображение зашифровано и передан ключ, пытаемся расшифровать
		if img.IsEncrypted && key != "" {
			decryptedData, err := s.pic.DecryptImage(c.Request.Context(), path, key)
			if err != nil {
				c.HTML(http.StatusForbidden, "error.html", gin.H{
					"error": "Неверный ключ или не удалось расшифровать",
					"user":  user,
				})
				return
			}
			c.Data(http.StatusOK, "image/"+img.Extension, decryptedData)
			return
		}
		// Иначе отдаём как есть (зашифрованные данные или оригинал)
		c.Data(http.StatusOK, "image/"+img.Extension, img.GetData())
		return
	}

	var images []*picstore.PicImage
	if tagsParam == "" {
		images, err = s.pic.GetPosts(c.Request.Context())
	} else {
		images, err = s.pic.GetPostsWithTags(c.Request.Context(), tagsParam)
	}
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
		queryParams := make([]string, 0)
		if tagsParam != "" {
			queryParams = append(queryParams, "tags="+url.QueryEscape(tagsParam))
		}
		if key != "" {
			queryParams = append(queryParams, "key="+url.QueryEscape(key))
		}
		if len(queryParams) > 0 {
			prev += "?" + strings.Join(queryParams, "&")
		}
	}
	next := ""
	if img.Next != nil {
		next = fmt.Sprintf("/view/%s", img.Next.Path)
		queryParams := make([]string, 0)
		if tagsParam != "" {
			queryParams = append(queryParams, "tags="+url.QueryEscape(tagsParam))
		}
		if key != "" {
			queryParams = append(queryParams, "key="+url.QueryEscape(key))
		}
		if len(queryParams) > 0 {
			next += "?" + strings.Join(queryParams, "&")
		}
	}
	c.HTML(http.StatusOK, "view.html", gin.H{
		"user":  user,
		"image": fmt.Sprintf("/image/%s/%s/%s/%s/%s", year, month, day, hour, filename),
		"img":   img,
		"prev":  prev,
		"next":  next,
		"tags":  tagsParam,
		"key":   key,
	})
}

// GET /i/posts - список постов текущего пользователя
func (s *Server) handlerUserPosts(c *gin.Context) {
	user := s.getUser(c)
	if user.ID == 0 {
		// не аутентифицирован, но middleware authMiddleware уже должен был отклонить
		c.Redirect(http.StatusSeeOther, "/sso/auth")
		return
	}

	// Получаем все посты пользователя
	allPosts, err := s.pic.GetUserPosts(c.Request.Context(), user.ID)
	if err != nil {
		s.log.Error("failed get user posts", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/posts?error=%s", url.QueryEscape("не удалось получить посты")))
		return
	}

	// Фильтрация по тегам
	tagsParam := strings.TrimSpace(c.Query("tags"))
	var filteredPosts []*picstore.PicImage
	if tagsParam != "" {
		searchTags := strings.Fields(tagsParam)
		// Вспомогательная функция для проверки тегов
		containsAllTags := func(imageTags string, searchTags []string) bool {
			if len(searchTags) == 0 {
				return true
			}
			tagMap := make(map[string]bool)
			for _, t := range strings.Fields(imageTags) {
				tagMap[t] = true
			}
			for _, st := range searchTags {
				if !tagMap[st] {
					return false
				}
			}
			return true
		}
		for _, img := range allPosts {
			if containsAllTags(img.Tags, searchTags) {
				filteredPosts = append(filteredPosts, img)
			}
		}
	} else {
		filteredPosts = allPosts
	}

	// Пагинация
	page := c.Query("page")
	if page == "" {
		page = "1"
	}
	curPage := atoi(page)
	if curPage < 1 {
		curPage = 1
	}

	start, end, total := pagify(len(filteredPosts), pageSize, curPage)
	prev := curPage - 1
	if prev < 1 {
		prev = 0
	}
	next := curPage + 1
	if next > total {
		next = 0
	}
	c.HTML(http.StatusOK, "profile/posts.html", gin.H{
		"posts": filteredPosts[start:end],
		"user":  user,
		"tags":  tagsParam,
		"pagination": gin.H{
			"total": total,
			"cur":   curPage,
			"prev":  prev,
			"next":  next,
		},
	})
}

// GET /i/view/:y/:m/:d/:h/:filename - просмотр своих картинок с навигацией по своим изображениям
func (s *Server) handlerUserView(c *gin.Context) {
	user := s.getUser(c)
	if user.ID == 0 {
		// не аутентифицирован, но middleware authMiddleware уже должен был отклонить
		c.Redirect(http.StatusSeeOther, "/sso/auth")
		return
	}

	year := c.Param("y")
	month := c.Param("m")
	day := c.Param("d")
	hour := c.Param("h")
	filename := c.Param("filename")
	view := c.Query("view")
	tagsParam := strings.TrimSpace(c.Query("tags"))

	s.log.Debug("user view",
		zap.String("y", year),
		zap.String("m", month),
		zap.String("d", day),
		zap.String("h", hour),
		zap.String("filename", filename),
		zap.String("tags", tagsParam),
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

	// Проверяем, что изображение принадлежит пользователю
	if img.UserID != user.ID {
		c.HTML(http.StatusForbidden, "error.html", gin.H{
			"error": "Нет прав на просмотр этого изображения",
			"user":  user,
		})
		return
	}

	key := c.Query("key")
	if view == "file" {
		// Если изображение зашифровано и передан ключ, пытаемся расшифровать
		if img.IsEncrypted && key != "" {
			decryptedData, err := s.pic.DecryptImage(c.Request.Context(), path, key)
			if err != nil {
				c.HTML(http.StatusForbidden, "error.html", gin.H{
					"error": "Неверный ключ или не удалось расшифровать",
					"user":  user,
				})
				return
			}
			c.Data(http.StatusOK, "image/"+img.Extension, decryptedData)
			return
		}
		// Иначе отдаём как есть (зашифрованные данные или оригинал)
		c.Data(http.StatusOK, "image/"+img.Extension, img.GetData())
		return
	}

	// Получаем все изображения пользователя
	allPosts, err := s.pic.GetUserPosts(c.Request.Context(), user.ID)
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": "Ошибка получения файла",
		})
		return
	}

	// Фильтрация по тегам
	var filteredPosts []*picstore.PicImage
	if tagsParam != "" {
		searchTags := strings.Fields(tagsParam)
		containsAllTags := func(imageTags string, searchTags []string) bool {
			if len(searchTags) == 0 {
				return true
			}
			tagMap := make(map[string]bool)
			for _, t := range strings.Fields(imageTags) {
				tagMap[t] = true
			}
			for _, st := range searchTags {
				if !tagMap[st] {
					return false
				}
			}
			return true
		}
		for _, post := range allPosts {
			if containsAllTags(post.Tags, searchTags) {
				filteredPosts = append(filteredPosts, post)
			}
		}
	} else {
		filteredPosts = allPosts
	}

	// Устанавливаем связи Prev/Next для отфильтрованного списка
	for i := range filteredPosts {
		if i > 0 {
			filteredPosts[i].Prev = filteredPosts[i-1]
		} else {
			filteredPosts[i].Prev = nil
		}
		if i < len(filteredPosts)-1 {
			filteredPosts[i].Next = filteredPosts[i+1]
		} else {
			filteredPosts[i].Next = nil
		}
	}

	// Находим текущее изображение в отфильтрованном списке
	var currentImg *picstore.PicImage
	for _, row := range filteredPosts {
		if row.ID == img.ID {
			currentImg = row
			break
		}
	}
	if currentImg == nil {
		// Если изображение не попало в фильтр (например, не соответствует тегам), показываем его без навигации
		currentImg = img
	}

	prev := ""
	if currentImg.Prev != nil {
		prev = fmt.Sprintf("/i/view/%s", currentImg.Prev.Path)
		queryParams := make([]string, 0)
		if tagsParam != "" {
			queryParams = append(queryParams, "tags="+url.QueryEscape(tagsParam))
		}
		if key != "" {
			queryParams = append(queryParams, "key="+url.QueryEscape(key))
		}
		if len(queryParams) > 0 {
			prev += "?" + strings.Join(queryParams, "&")
		}
	}
	next := ""
	if currentImg.Next != nil {
		next = fmt.Sprintf("/i/view/%s", currentImg.Next.Path)
		queryParams := make([]string, 0)
		if tagsParam != "" {
			queryParams = append(queryParams, "tags="+url.QueryEscape(tagsParam))
		}
		if key != "" {
			queryParams = append(queryParams, "key="+url.QueryEscape(key))
		}
		if len(queryParams) > 0 {
			next += "?" + strings.Join(queryParams, "&")
		}
	}
	c.HTML(http.StatusOK, "profile/view.html", gin.H{
		"user":  user,
		"image": fmt.Sprintf("/image/%s/%s/%s/%s/%s", year, month, day, hour, filename),
		"img":   currentImg,
		"prev":  prev,
		"next":  next,
		"tags":  tagsParam,
		"key":   key,
	})
}

// POST /view/:y/:m/:d/:h/:filename/update - обновление изображения (публичность и теги)
func (s *Server) handlerUpdateImage(c *gin.Context) {
	user := s.getUser(c)
	if user.ID == 0 {
		c.Redirect(http.StatusSeeOther, "/sso/auth")
		return
	}

	year := c.Param("y")
	month := c.Param("m")
	day := c.Param("d")
	hour := c.Param("h")
	filename := c.Param("filename")
	path := path.Join(year, month, day, hour, filename)

	// Получаем изображение, чтобы узнать его ID и проверить владельца
	img, err := s.pic.GetImg(c.Request.Context(), path)
	if err != nil {
		s.log.Error("failed get image for update", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/view/%s?error=%s", path, url.QueryEscape("изображение не найдено")))
		return
	}
	if img.UserID != user.ID {
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/view/%s?error=%s", path, url.QueryEscape("нет прав на редактирование")))
		return
	}

	// Парсим форму
	isPublic := c.PostForm("is_public") == "on"
	tags := c.PostForm("tags")
	var isPublicPtr *bool
	var tagsPtr *string
	// Обновляем только если переданы значения (можно передать пустые, но мы будем обновлять всегда)
	isPublicPtr = &isPublic
	tagsPtr = &tags

	err = s.pic.UpdateImage(c.Request.Context(), user.ID, img.ID, isPublicPtr, tagsPtr)
	if err != nil {
		s.log.Error("failed update image", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/view/%s?error=%s", path, url.QueryEscape("не удалось обновить изображение")))
		return
	}

	// Получаем параметры фильтрации из запроса (tags и key)
	tagsParam := strings.TrimSpace(c.Query("tags"))
	keyParam := strings.TrimSpace(c.Query("key"))
	// Строим URL для редиректа
	redirectURL := fmt.Sprintf("/i/view/%s", path)
	queryParams := make([]string, 0)
	if tagsParam != "" {
		queryParams = append(queryParams, "tags="+url.QueryEscape(tagsParam))
	}
	if keyParam != "" {
		queryParams = append(queryParams, "key="+url.QueryEscape(keyParam))
	}
	queryParams = append(queryParams, "info="+url.QueryEscape("изображение обновлено"))
	if len(queryParams) > 0 {
		redirectURL += "?" + strings.Join(queryParams, "&")
	}
	c.Redirect(http.StatusSeeOther, redirectURL)
}

// POST /view/:y/:m/:d/:h/:filename/delete - удаление изображения
func (s *Server) handlerDeleteImage(c *gin.Context) {
	user := s.getUser(c)
	if user.ID == 0 {
		c.Redirect(http.StatusSeeOther, "/sso/auth")
		return
	}

	year := c.Param("y")
	month := c.Param("m")
	day := c.Param("d")
	hour := c.Param("h")
	filename := c.Param("filename")
	path := path.Join(year, month, day, hour, filename)

	img, err := s.pic.GetImg(c.Request.Context(), path)
	if err != nil {
		s.log.Error("failed get image for delete", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/view/%s?error=%s", path, url.QueryEscape("изображение не найдено")))
		return
	}
	if img.UserID != user.ID {
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/view/%s?error=%s", path, url.QueryEscape("нет прав на удаление")))
		return
	}

	err = s.pic.DeleteImage(c.Request.Context(), user.ID, img.ID)
	if err != nil {
		s.log.Error("failed delete image", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/view/%s?error=%s", path, url.QueryEscape("не удалось удалить изображение")))
		return
	}

	// После удаления перенаправляем на список постов пользователя
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/posts?info=%s", url.QueryEscape("изображение удалено")))
}
