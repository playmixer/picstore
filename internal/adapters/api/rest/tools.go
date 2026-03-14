package rest

import (
	"fmt"
	"picstore/internal/adapters/models"
	"reflect"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/playmixer/single-auth/pkg/authtools"
	"go.uber.org/zap"
)

func (s *Server) resetCookie(c *gin.Context) {
	for _, domain := range s.cookieDomain {
		s.log.Debug("clear cookie", zap.String("host", domain))
		c.SetCookie(CookieJWT, "", s.cookieLifeTime, "/", domain, s.cookieSecure, true)
		c.SetCookie(CookieRefreshToken, "", s.cookieLifeTime, "/", domain, s.cookieSecure, true)
	}
}

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

func atoi(n string) int {
	i, err := strconv.Atoi(n)
	if err != nil {
		return 0
	}
	return i
}

func datetimeF(t time.Time) string {
	return t.Format("02.01.2006 15:04")
}

func genNumbers(total int) []int {
	res := make([]int, total)
	for i := 0; i < total; i++ {
		res[i] = i + 1
	}
	return res
}

func afterI(cur, new int) bool {
	return new > cur
}

func beforeI(cur, new int) bool {
	return new < cur
}
