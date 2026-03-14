package auth

import (
	"context"
	"errors"
	"fmt"
	"picstore/internal/adapters/apperror"
	"picstore/internal/adapters/models"
)

type storage interface {
	GetUserByEmail(ctx context.Context, email string) (user *models.User, err error)
	CreateUser(ctx context.Context, login, email, passwordHash string, source string, admin bool) (*models.User, error)
	GetUserByID(ctx context.Context, userID uint) (*models.User, error)
	UpdUser(ctx context.Context, user *models.User) error

	NewRole(ctx context.Context, name string) (*models.Role, error)
	GetRole(ctx context.Context, name string) (*models.Role, error)
}

type Auth struct {
	store storage
}

func New(store storage) (*Auth, error) {
	a := &Auth{
		store: store,
	}

	return a, nil
}

func (a *Auth) NewUser(ctx context.Context, user *models.User) (*models.User, error) {
	return a.store.CreateUser(ctx, user.Username, user.Email, user.PasswordHash, user.RegSource, false)
}

func (a *Auth) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return a.store.GetUserByEmail(ctx, email)
}

func (a *Auth) GetUser(ctx context.Context, userID uint) (*models.User, error) {
	return a.store.GetUserByID(ctx, userID)
}

func (a *Auth) UpdRoles(ctx context.Context, user *models.User) (*models.User, error) {
	roles := []models.Role{}
	for _, r := range user.Roles {
		role, err := a.store.GetRole(ctx, r.Name.String())
		if err != nil && !errors.Is(err, apperror.ErrNotFoundData) {
			return nil, fmt.Errorf("failed getting role by name `%s`: %w", r.Name.String(), err)
		}
		if err != nil && errors.Is(err, apperror.ErrNotFoundData) {
			role, err = a.store.NewRole(ctx, r.Name.String())
			if err != nil {
				return nil, fmt.Errorf("failed create role: %w", err)
			}
		}
		roles = append(roles, *role)
	}
	user.Roles = roles

	return user, a.store.UpdUser(ctx, user)
}
