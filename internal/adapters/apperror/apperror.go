package apperror

import "errors"

var (
	ErrNotFoundData   = errors.New("data not found")
	ErrLoginNotUnique = errors.New("login not unique")
)
