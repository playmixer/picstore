package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"picstore/internal/adapters/apperror"
	"picstore/internal/adapters/models"
	"picstore/internal/core/picstore"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/playmixer/single-auth/pkg/authtools"
	"go.uber.org/zap"
)

var (
	pageSize int = 10
)

// handlerSSOLogin обрабатывает callback от SSO провайдера, проверяет state, расшифровывает параметры,
// создаёт или обновляет пользователя, устанавливает JWT cookie и перенаправляет на оригинальный URL.
func (s *Server) handlerSSOLogin(ctx *gin.Context) {
	// Проверка state для защиты от CSRF
	stateParam := ctx.Query("state")
	// Декодируем процентное кодирование (например, %3D -> =)
	decodedState, err := url.QueryUnescape(stateParam)
	if err != nil {
		// Если ошибка декодирования, используем оригинальный параметр
		decodedState = stateParam
	}
	savedState := s.getAuthStateCookie(ctx)
	s.log.Debug("handlerSSOLogin state check",
		zap.String("stateParam", stateParam),
		zap.String("decodedState", decodedState),
		zap.String("savedState", savedState),
	)
	if decodedState == "" || savedState == "" || decodedState != savedState {
		s.log.Warn("invalid state parameter",
			zap.String("stateParam", stateParam),
			zap.String("decodedState", decodedState),
			zap.String("savedState", savedState),
		)
		ctx.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": "Invalid state parameter",
		})
		return
	}
	// Очищаем cookie состояния сразу после проверки, чтобы предотвратить повторное использование
	s.clearAuthStateCookie(ctx)

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

	// Редирект на оригинальный URL, сохранённый перед авторизацией
	originalURL := s.getOriginalURLCookie(ctx)
	s.clearOriginalURLCookie(ctx)
	s.log.Debug("handlerSSOLogin originalURL from cookie",
		zap.String("originalURL", originalURL),
	)
	if originalURL == "" {
		originalURL = "/"
	}
	// Декодируем процентное кодирование (на случай, если URL был закодирован при сохранении)
	decodedURL, err := url.QueryUnescape(originalURL)
	if err != nil {
		s.log.Warn("failed to decode originalURL, using as-is", zap.Error(err))
		decodedURL = originalURL
	}
	s.log.Debug("handlerSSOLogin redirecting to",
		zap.String("redirectURL", decodedURL),
	)
	ctx.Redirect(http.StatusMovedPermanently, decodedURL)
}

// handlerMain отображает главную страницу (index.html) с информацией о пользователе.
func (s *Server) handlerMain(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"user": s.getUser(c),
	})
}

// handlerAbout отображает страницу "О проекте" (about.html).
func (s *Server) handlerAbout(c *gin.Context) {
	c.HTML(http.StatusOK, "about.html", gin.H{
		"user": s.getUser(c),
	})
}

// handlerProfile отображает страницу профиля пользователя (profile/index.html).
func (s *Server) handlerProfile(c *gin.Context) {
	c.HTML(http.StatusOK, "profile/index.html", gin.H{
		"user": s.getUser(c),
	})
}

// handlerUpload отображает страницу загрузки изображений (profile/upload.html) с информацией о максимальном размере файла.
func (s *Server) handlerUpload(c *gin.Context) {
	c.HTML(http.StatusOK, "profile/upload.html", gin.H{
		"user":        s.getUser(c),
		"maxFileSize": s.pic.GetMaxFileSize(),
	})
}

// handlerUploadPost обрабатывает POST-запрос загрузки изображений (файлы или URL), выполняет валидацию и сохраняет через PicStore.
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
		// Попробуем получить несколько файлов
		form, err := c.MultipartForm()
		if err != nil {
			s.log.Debug("failed getting multipart form", zap.Error(err))
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("ошибка получения формы")))
			return
		}
		files := form.File["files"]
		if len(files) == 0 {
			// fallback на одиночный файл (для обратной совместимости)
			file, err := c.FormFile("file")
			if err != nil {
				s.log.Debug("failed getting form file", zap.Error(err))
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("ошибка получения файла")))
				return
			}
			files = []*multipart.FileHeader{file}
		}
		// Проверка ограничения количества файлов
		if len(files) > s.maxUploadItems {
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape(fmt.Sprintf("превышено максимальное количество файлов (максимум %d)", s.maxUploadItems))))
			return
		}
		if len(files) == 1 {
			_, err := s.pic.UploadImgFile(c.Request.Context(), user.ID, files[0], isPublic, tags, encryptionKey)
			if err != nil {
				s.log.Error("failed save file", zap.Error(err))
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить файл")))
				return
			}
		} else {
			_, err := s.pic.UploadMultipleImgFiles(c.Request.Context(), user.ID, files, isPublic, tags, encryptionKey)
			if err != nil {
				s.log.Error("failed save multiple files", zap.Error(err))
				// В ошибке может быть информация о частичной загрузке, но мы просто редиректим с ошибкой
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить некоторые файлы")))
				return
			}
		}
	} else {
		// Обработка URL
		urlsText := c.PostForm("urls")
		var urls []string
		if urlsText != "" {
			// Разделяем по переносу строки, удаляем пустые
			lines := strings.Split(urlsText, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					urls = append(urls, line)
				}
			}
		}
		if len(urls) == 0 {
			// fallback на одиночный URL (для обратной совместимости)
			link := c.PostForm("url")
			if link == "" {
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не указан URL")))
				return
			}
			urls = []string{link}
		}
		// Проверка ограничения количества URL
		if len(urls) > s.maxUploadItems {
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape(fmt.Sprintf("превышено максимальное количество URL (максимум %d)", s.maxUploadItems))))
			return
		}
		if len(urls) == 1 {
			_, err := s.pic.UploadImgURL(c.Request.Context(), user.ID, urls[0], isPublic, tags, encryptionKey)
			if err != nil {
				s.log.Error("failed save file from url", zap.String("url", urls[0]), zap.Error(err))
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить файл")))
				return
			}
		} else {
			_, err := s.pic.UploadMultipleImgURLs(c.Request.Context(), user.ID, urls, isPublic, tags, encryptionKey)
			if err != nil {
				s.log.Error("failed save multiple urls", zap.Error(err))
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?error=%s", url.QueryEscape("не удалось сохранить некоторые URL")))
				return
			}
		}
	}

	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/upload?info=%s", url.QueryEscape("файл(ы) загружены")))
}

// handlerPosts отображает страницу публичных постов с пагинацией и фильтрацией по тегам.
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
	pagePosts, total, err := s.pic.GetPostsPage(c.Request.Context(), 0, curPage, pageSize, tagsParam)
	if err != nil {
		s.log.Error("failed get posts page", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/posts?error=%s", url.QueryEscape("не удалось получить посты")))
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize
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
	// Если префикс начинается с минуса, игнорируем его для поиска
	searchPrefix := strings.TrimPrefix(prefix, "-")
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
		if searchPrefix == "" || strings.HasPrefix(strings.ToLower(tag), searchPrefix) {
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
	if err != nil || !img.IsPublic {
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

	// Учитываем просмотр (только для HTML страницы, не для скачивания файла)
	userID := uint(0)
	if user != nil && user.ID != 0 {
		userID = user.ID
	}
	_, _ = s.pic.RecordView(c.Request.Context(), img.ID, userID)

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

	// Устанавливаем связи Prev/Next для навигации
	for i := range images {
		if i > 0 {
			images[i].Prev = images[i-1]
		} else {
			images[i].Prev = nil
		}
		if i < len(images)-1 {
			images[i].Next = images[i+1]
		} else {
			images[i].Next = nil
		}
	}

	for _, row := range images {
		if row.ID == img.ID {
			img = row
			break
		}
	}

	// Отладочное логирование навигационных связей
	s.log.Debug("handlerView navigation",
		zap.Uint("imageID", img.ID),
		zap.String("imagePath", img.Path),
		zap.Bool("hasPrev", img.Prev != nil),
		zap.Bool("hasNext", img.Next != nil),
		zap.String("tags", tagsParam),
		zap.Int("totalImages", len(images)),
	)

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

	s.log.Debug("handlerView generated links",
		zap.String("prev", prev),
		zap.String("next", next),
	)
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
	pagePosts, total, err := s.pic.GetPostsPage(c.Request.Context(), user.ID, curPage, pageSize, tagsParam)
	if err != nil {
		s.log.Error("failed get posts page", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/posts?error=%s", url.QueryEscape("не удалось получить посты")))
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize
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

	c.HTML(http.StatusOK, "profile/posts.html", gin.H{
		"posts": pagePosts,
		"user":  user,
		"tags":  tagsParam,
		"pagination": gin.H{
			"total": totalPages,
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
				s.log.Debug("failed decrypt file", zap.Error(err))
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

	// Учитываем просмотр (только для HTML страницы, не для скачивания файла)
	_, _ = s.pic.RecordView(c.Request.Context(), img.ID, user.ID)

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
			// Разделяем теги на включающие и исключающие
			var includeTags, excludeTags []string
			for _, t := range searchTags {
				if strings.HasPrefix(t, "-") && len(t) > 1 {
					excludeTags = append(excludeTags, strings.ToLower(t[1:]))
				} else {
					includeTags = append(includeTags, strings.ToLower(t))
				}
			}
			// Строим карту тегов изображения (в нижнем регистре)
			tagMap := make(map[string]bool)
			for _, t := range strings.Fields(imageTags) {
				tagMap[strings.ToLower(t)] = true
			}
			// Проверяем наличие всех включающих тегов
			for _, t := range includeTags {
				if !tagMap[t] {
					return false
				}
			}
			// Проверяем отсутствие исключающих тегов
			for _, t := range excludeTags {
				if tagMap[t] {
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

	// Отладочное логирование навигационных связей
	s.log.Debug("handlerUserView navigation",
		zap.Uint("imageID", currentImg.ID),
		zap.String("imagePath", currentImg.Path),
		zap.Bool("hasPrev", currentImg.Prev != nil),
		zap.Bool("hasNext", currentImg.Next != nil),
		zap.String("tags", tagsParam),
		zap.Int("totalFiltered", len(filteredPosts)),
	)

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

	// Отладочное логирование сгенерированных ссылок
	s.log.Debug("handlerUserView generated links",
		zap.String("prev", prev),
		zap.String("next", next),
	)

	// Предзагрузка соседних изображений
	prevImageURL := ""
	nextImageURL := ""
	if currentImg.Prev != nil {
		prevImageURL = fmt.Sprintf("/i/view/%s?view=file", currentImg.Prev.Path)
		if key != "" {
			prevImageURL += "&key=" + url.QueryEscape(key)
		}
	}
	if currentImg.Next != nil {
		nextImageURL = fmt.Sprintf("/i/view/%s?view=file", currentImg.Next.Path)
		if key != "" {
			nextImageURL += "&key=" + url.QueryEscape(key)
		}
	}
	c.HTML(http.StatusOK, "profile/view.html", gin.H{
		"user":         user,
		"image":        fmt.Sprintf("/image/%s/%s/%s/%s/%s", year, month, day, hour, filename),
		"img":          currentImg,
		"prev":         prev,
		"next":         next,
		"prevImageURL": prevImageURL,
		"nextImageURL": nextImageURL,
		"tags":         tagsParam,
		"key":          key,
	})
}

// POST /i/view/:y/:m/:d/:h/:filename/update - обновление изображения (публичность и теги)
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
	callback := strings.TrimSpace(c.PostForm("callback"))
	if callback != "" {
		redirectURL = callback
		s.log.Debug("update callback", zap.String("url", callback))
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

	err = s.pic.DeleteImages(c.Request.Context(), user.ID, []uint{img.ID})
	if err != nil {
		s.log.Error("failed delete image", zap.Error(err))
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/view/%s?error=%s", path, url.QueryEscape("не удалось удалить изображение")))
		return
	}

	// После удаления перенаправляем на список постов пользователя
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/i/posts?info=%s", url.QueryEscape("изображение удалено")))
}

// POST /i/posts/delete - массовое удаление изображений
func (s *Server) handlerDeleteImages(c *gin.Context) {
	user := s.getUser(c)
	if user.ID == 0 {
		c.Redirect(http.StatusSeeOther, "/sso/auth")
		return
	}

	var req struct {
		ImageIDs []uint `json:"image_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		s.log.Error("failed bind json", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "неверный формат запроса"})
		return
	}

	if len(req.ImageIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "список изображений пуст"})
		return
	}

	err := s.pic.DeleteImages(c.Request.Context(), user.ID, req.ImageIDs)
	if err != nil {
		s.log.Error("failed delete images", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось удалить изображения"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "изображения удалены"})
}

// GET /health - health check endpoint
func (s *Server) handlerHealth(c *gin.Context) {
	// Проверяем доступность базы данных
	dbOK := true
	if storage, ok := s.pic.(interface{ Ping(context.Context) error }); ok {
		ctx := c.Request.Context()
		if err := storage.Ping(ctx); err != nil {
			s.log.Warn("database ping failed", zap.Error(err))
			dbOK = false
		}
	} else {
		// Если интерфейс Ping не реализован, считаем, что БД работает
		s.log.Debug("database ping not implemented")
	}

	// Проверяем доступность кэша
	cacheOK := true
	if s.cache != nil {
		ctx := c.Request.Context()
		if err := s.cache.Set(ctx, "healthcheck", []byte("ping"), 1*time.Second); err != nil {
			s.log.Warn("cache set failed", zap.Error(err))
			cacheOK = false
		}
	}

	status := http.StatusOK
	message := "OK"
	if !dbOK || !cacheOK {
		status = http.StatusServiceUnavailable
		message = "degraded"
	}

	c.JSON(status, gin.H{
		"status":  message,
		"db":      dbOK,
		"cache":   cacheOK,
		"version": "1.0",
	})
}

// GET /api/navigation/:imageID - получение NavigationContext для навигации по изображениям
func (s *Server) handlerNavigationContext(c *gin.Context) {
	imageIDStr := c.Param("imageID")
	imageID, err := strconv.ParseUint(imageIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "неверный ID изображения"})
		return
	}

	windowStr := c.DefaultQuery("window", "5")
	window, err := strconv.Atoi(windowStr)
	if err != nil || window < 1 || window > 100 {
		window = 5
	}

	includeTags := strings.Fields(c.DefaultQuery("include", ""))
	excludeTags := strings.Fields(c.DefaultQuery("exclude", ""))

	// Определяем текущего пользователя (если авторизован)
	var currentUserID uint
	user := s.getUser(c)
	if user != nil {
		currentUserID = user.ID
	}

	// Если запрос с /view, может быть передан параметр isPublic для принудительной публичной навигации
	isPublicParam := c.DefaultQuery("isPublic", "false")
	if isPublicParam == "true" {
		currentUserID = 0
	}

	s.log.Debug("handlerNavigationContext",
		zap.Uint64("imageID", imageID),
		zap.Int("window", window),
		zap.Strings("includeTags", includeTags),
		zap.Strings("excludeTags", excludeTags),
		zap.Uint("currentUserID", currentUserID),
		zap.String("isPublic", isPublicParam),
	)

	ctx := c.Request.Context()
	navCtx, err := s.pic.GetNavigationContext(ctx, uint(imageID), window, includeTags, excludeTags, currentUserID)
	if err != nil {
		s.log.Error("failed to get navigation context", zap.Error(err), zap.Uint64("imageID", imageID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось получить контекст навигации"})
		return
	}

	c.JSON(http.StatusOK, navCtx)
}

// POST /api/view/:imageID/record - увеличение счетчика просмотров для изображения
func (s *Server) handlerRecordView(c *gin.Context) {
	imageIDStr := c.Param("imageID")
	imageID, err := strconv.ParseUint(imageIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "неверный ID изображения"})
		return
	}

	// Определяем текущего пользователя (если авторизован)
	var userID uint
	user := s.getUser(c)
	if user != nil {
		userID = user.ID
	} else {
		userID = 0
	}

	s.log.Debug("handlerRecordView",
		zap.Uint64("imageID", imageID),
		zap.Uint("userID", userID),
	)

	ctx := c.Request.Context()
	_, err = s.pic.RecordView(ctx, uint(imageID), userID)
	if err != nil {
		s.log.Error("failed to record view", zap.Error(err), zap.Uint64("imageID", imageID))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось зафиксировать просмотр"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
