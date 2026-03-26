package rest

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"
	"picstore/internal/adapters/models"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/playmixer/single-auth/pkg/authtools"
	"go.uber.org/zap"
)

// resetCookie очищает JWT и refresh‑token cookie для всех настроенных доменов.
func (s *Server) resetCookie(c *gin.Context) {
	for _, domain := range s.cookieDomain {
		s.log.Debug("clear cookie", zap.String("host", domain))
		c.SetCookie(CookieJWT, "", s.cookieLifeTime, "/", domain, s.cookieSecure, true)
		c.SetCookie(CookieRefreshToken, "", s.cookieLifeTime, "/", domain, s.cookieSecure, true)
	}
}

// generateState создает криптографически случайную строку для state.
func (s *Server) generateState() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// setAuthStateCookie сохраняет state в cookie.
func (s *Server) setAuthStateCookie(c *gin.Context, state string) {
	for _, domain := range s.cookieDomain {
		c.SetCookie(CookieState, state, s.cookieLifeTime, "/", domain, s.cookieSecure, true)
		s.log.Debug("set auth state cookie", zap.String("state", state), zap.String("domain", domain))
	}
}

// getAuthStateCookie возвращает сохраненный state из cookie.
func (s *Server) getAuthStateCookie(c *gin.Context) string {
	cookie, err := c.Request.Cookie(CookieState)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// clearAuthStateCookie удаляет cookie состояния.
func (s *Server) clearAuthStateCookie(c *gin.Context) {
	for _, domain := range s.cookieDomain {
		c.SetCookie(CookieState, "", -1, "/", domain, s.cookieSecure, true)
		s.log.Debug("clear auth state cookie", zap.String("domain", domain))
	}
}

// setOriginalURLCookie сохраняет оригинальный URL для возврата после авторизации.
func (s *Server) setOriginalURLCookie(c *gin.Context, originalURL string) {
	if len(s.cookieDomain) == 0 {
		c.SetCookie(CookieOriginalURL, originalURL, 300, "/", "", s.cookieSecure, true)
		s.log.Debug("set original URL cookie (no domain)", zap.String("url", originalURL))
		return
	}
	for _, domain := range s.cookieDomain {
		c.SetCookie(CookieOriginalURL, originalURL, 300, "/", domain, s.cookieSecure, true)
		s.log.Debug("set original URL cookie", zap.String("url", originalURL), zap.String("domain", domain))
	}
}

// getOriginalURLCookie возвращает сохраненный оригинальный URL.
func (s *Server) getOriginalURLCookie(c *gin.Context) string {
	cookie, err := c.Request.Cookie(CookieOriginalURL)
	if err != nil {
		s.log.Debug("getOriginalURLCookie: no cookie found", zap.Error(err))
		return ""
	}
	s.log.Debug("getOriginalURLCookie", zap.String("value", cookie.Value))
	return cookie.Value
}

// clearOriginalURLCookie удаляет cookie оригинального URL.
func (s *Server) clearOriginalURLCookie(c *gin.Context) {
	if len(s.cookieDomain) == 0 {
		c.SetCookie(CookieOriginalURL, "", -1, "/", "", s.cookieSecure, true)
		s.log.Debug("clear original URL cookie (no domain)")
		return
	}
	for _, domain := range s.cookieDomain {
		c.SetCookie(CookieOriginalURL, "", -1, "/", domain, s.cookieSecure, true)
		s.log.Debug("clear original URL cookie", zap.String("domain", domain))
	}
}

// buildAuthURL возвращает URL для авторизации с добавленными параметрами state и callback.
func (s *Server) buildAuthURL(c *gin.Context) (string, error) {
	state, err := s.generateState()
	if err != nil {
		return "", err
	}
	s.log.Debug("generated state", zap.String("state", state))
	s.setAuthStateCookie(c, state)
	originalURL := c.Request.URL.String()
	s.log.Debug("buildAuthURL initial originalURL", zap.String("originalURL", originalURL))
	// Если текущий путь является частью потока авторизации, не сохраняем его как оригинальный URL,
	// чтобы избежать рекурсии. Вместо этого используем корень или referer.
	if strings.HasPrefix(originalURL, "/sso/") {
		// Попробуем взять referer из заголовка
		referer := c.Request.Referer()
		s.log.Debug("buildAuthURL referer", zap.String("referer", referer), zap.String("baseURL", s.baseURL))
		if referer != "" && !strings.HasPrefix(referer, "/sso/") {
			// Извлекаем путь из referer, если он относится к нашему домену
			if strings.HasPrefix(referer, s.baseURL) {
				originalURL = strings.TrimPrefix(referer, s.baseURL)
				if originalURL == "" {
					originalURL = "/"
				}
			} else {
				// referer с другого домена, используем корень
				originalURL = "/"
			}
		} else {
			originalURL = "/"
		}
	}
	s.log.Debug("original URL for redirect", zap.String("originalURL", originalURL))
	s.setOriginalURLCookie(c, originalURL)

	u, err := url.Parse(s.ssoAuthURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("state", state)
	q.Set("callback", s.baseURL+"/sso/login") // callback endpoint
	u.RawQuery = q.Encode()
	authURL := u.String()
	s.log.Debug("built auth URL", zap.String("authURL", authURL))
	return authURL, nil
}

// empty возвращает true, если значение типа string пустое или типа int равно нулю.
// Поддерживаются только string и int; для других типов всегда возвращает false.
func empty[T string | int](s T) bool {
	val := reflect.ValueOf(s)

	switch val.Kind() {
	case reflect.String:
		return val.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() == 0
	default:
		// Для других типов данных можно добавить дополнительную логику
		return false
	}
}

// checkAuth проверяет JWT cookie и возвращает идентификатор пользователя, если токен валиден.
// В случае ошибки (отсутствие cookie, невалидный токен, отсутствие userID) возвращает ошибку.
func (s *Server) checkAuth(c *gin.Context) (userID string, err error) {
	var ok bool
	var tknData map[string]string
	cookieUserID, err := c.Request.Cookie(CookieJWT)
	if err == nil {
		tknData, ok = authtools.VerifyJWT([]byte(s.secretKey), cookieUserID.Value)
	}
	if err != nil {
		return "", fmt.Errorf("failed reade user cookie: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("unverify usercookie")
	}
	if userID, ok = tknData["userID"]; ok {
		return userID, nil
	}

	return "", fmt.Errorf("failed to read user id from token")
}

// getUser возвращает объект пользователя из контекста запроса.
// Если аутентификация не удалась, возвращает пустую структуру User.
func (s *Server) getUser(c *gin.Context) *models.User {
	u := &models.User{}
	userIDs, err := s.checkAuth(c)
	if err != nil {
		s.log.Error("failed check auth", zap.Error(err))
		return u
	}
	userID, err := strconv.Atoi(userIDs)
	if err != nil {
		s.log.Error("failed conver userID", zap.Error(err))
		return u
	}
	user, err := s.auth.GetUser(c.Request.Context(), uint(userID))
	if err != nil {
		s.log.Error("failed get user", zap.Error(err))
		return u
	}

	s.log.Debug("get auth user", zap.Any("user", user))

	return user
}

// pagify вычисляет начальный и конечный индексы строк для пагинации, а также общее количество страниц.
// totalRows - общее количество строк, pageSize - размер страницы, curPage - текущая страница (начиная с 1).
// Возвращает rowStart (индекс первой строки на странице, 0‑based), rowEnd (индекс строки после последней на странице),
// и totalPage (общее количество страниц). Если totalRows == 0, totalPage будет 0.
func pagify(totalRows int, pageSize int, curPage int) (rowStart, rowEnd int, totalPage int) {
	// Корректируем curPage, если он меньше 1
	if curPage < 1 {
		curPage = 1
	}
	rowStart = (curPage - 1) * pageSize
	if rowStart > totalRows {
		rowStart = totalRows
	}
	if rowStart < 0 {
		rowStart = 0
	}
	rowEnd = curPage * pageSize
	if rowEnd > totalRows {
		rowEnd = totalRows
	}
	totalPage = totalRows / pageSize
	if totalRows%pageSize > 0 {
		totalPage += 1
	}
	// Если totalRows == 0, то totalPage должен быть 0 (нет страниц)
	if totalPage < 0 {
		totalPage = 0
	}
	return
}

// atoi преобразует строку в целое число. Если преобразование невозможно, возвращает 0.
func atoi(n string) int {
	i, err := strconv.Atoi(n)
	if err != nil {
		return 0
	}
	return i
}

// datetimeF форматирует время в строку "дд.мм.гггг чч:мм".
func datetimeF(t time.Time) string {
	return t.Format("02.01.2006 15:04")
}

// genNumbers возвращает срез целых чисел от 1 до total включительно.
func genNumbers(total int) []int {
	res := make([]int, total)
	for i := 0; i < total; i++ {
		res[i] = i + 1
	}
	return res
}

// afterI возвращает true, если new больше cur (новое значение находится «после» текущего).
func afterI(cur, new int) bool {
	return new > cur
}

// beforeI возвращает true, если new меньше cur (новое значение находится «до» текущего).
func beforeI(cur, new int) bool {
	return new < cur
}
