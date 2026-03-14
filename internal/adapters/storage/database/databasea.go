package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"picstore/internal/adapters/apperror"
	"picstore/internal/adapters/models"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Config struct {
	DSN string `env:"DATABASE_ADDRESS"`
}

type Storage struct {
	db *gorm.DB
}

func New(dsn string) (*Storage, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed open connect: %w", err)
	}
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlDB,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed open gorm connect: %w", err)
	}

	db := &Storage{
		db: gormDB,
	}

	if err := db.migration(); err != nil {
		return nil, fmt.Errorf("failed auto migrations: %w", err)
	}

	return db, nil
}

func (s *Storage) migration() error {
	if err := s.db.AutoMigrate(
		&models.User{},
		&models.Image{},
		&models.Tag{},
		&models.ImageTag{},
	); err != nil {
		return fmt.Errorf("failed migrations: %w", err)
	}
	return nil
}

func (s *Storage) CreateUser(ctx context.Context, login, email, passwordHash string, source string, admin bool) (*models.User, error) {
	login = strings.ToLower(login)
	email = strings.ToLower(email)
	user := &models.User{
		Username:     login,
		PasswordHash: passwordHash,
		Email:        email,
		RegSource:    source,
		Model: gorm.Model{
			CreatedAt: time.Now(),
		},
	}

	err := s.db.WithContext(ctx).Create(user).Error
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, fmt.Errorf("login not unique: %w %w", err, apperror.ErrLoginNotUnique)
		}
		return nil, fmt.Errorf("failed create user: %w", err)
	}

	return user, nil
}

func (s *Storage) GetUser(ctx context.Context, login string) (*models.User, error) {
	login = strings.ToLower(login)
	user := &models.User{}
	err := s.db.WithContext(ctx).Where("login = ?", login).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.Join(apperror.ErrNotFoundData, err)
		}
		return nil, fmt.Errorf("failed find user: %w", err)
	}

	return user, nil
}

func (s *Storage) GetUserByID(ctx context.Context, userID uint) (*models.User, error) {
	user := &models.User{}
	err := s.db.WithContext(ctx).Where("id = ?", userID).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.Join(apperror.ErrNotFoundData, err)
		}
		return nil, fmt.Errorf("failed find user: %w", err)
	}

	return user, nil
}

func (s *Storage) Close() error {
	return nil
}

func (s *Storage) UpdUser(ctx context.Context, user *models.User) error {
	err := s.db.WithContext(ctx).Where("id = ?", user.ID).Save(user).Association("Roles").Error
	if err != nil {
		return fmt.Errorf("failed update user: %w", err)
	}

	return nil
}

func (s *Storage) FindUsersByLogin(ctx context.Context, login string) ([]models.User, error) {
	login = strings.ToLower(login)
	users := []models.User{}

	err := s.db.WithContext(ctx).Where("login like ?", "%"+login+"%").Find(&users).Error
	if err != nil && !errors.Is(gorm.ErrRecordNotFound, err) {
		return users, fmt.Errorf("failes find users: %w", err)
	}

	return users, nil
}

func (s *Storage) RemoveUser(ctx context.Context, userID uint) error {
	return s.db.WithContext(ctx).Where("id = ?", userID).Delete(&models.User{}).Error
}

func (s *Storage) GetUserByEmail(ctx context.Context, email string) (user *models.User, err error) {
	user = &models.User{}
	err = s.db.WithContext(ctx).Where("email = ?", email).First(user).Error
	if err != nil {
		if errors.Is(gorm.ErrRecordNotFound, err) {
			return nil, apperror.ErrNotFoundData
		}
		return user, fmt.Errorf("failes find user: %w", err)
	}
	return user, nil
}

func (s *Storage) GetRole(ctx context.Context, name string) (*models.Role, error) {
	role := &models.Role{}
	err := s.db.WithContext(ctx).Where("name = ?", name).First(role).Error
	if err != nil {
		if errors.Is(gorm.ErrRecordNotFound, err) {
			return nil, apperror.ErrNotFoundData
		}
		return role, fmt.Errorf("failes find role: %w", err)
	}
	return role, nil
}

func (s *Storage) NewRole(ctx context.Context, name string) (*models.Role, error) {
	role := &models.Role{
		Name: models.TRole(name),
	}
	err := s.db.WithContext(ctx).Save(role).Error
	if err != nil {
		return nil, fmt.Errorf("failed create role: %w", err)
	}

	return role, nil
}

func (s *Storage) NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string) (*models.Image, error) {
	// Создаём изображение
	img := &models.Image{
		UserID:   userID,
		Path:     path,
		IsPublic: isPublic,
		Tags:     tags, // сохраняем исходную строку для обратной совместимости
	}
	err := s.db.WithContext(ctx).Save(img).Error
	if err != nil {
		return nil, fmt.Errorf("failed save image to db: %w", err)
	}

	// Обрабатываем теги
	if tags != "" {
		tagNames := strings.Fields(tags) // разделяем по пробелам
		validTagRegex := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
		for _, tagName := range tagNames {
			tagName = strings.TrimSpace(tagName)
			if tagName == "" {
				continue
			}
			if !validTagRegex.MatchString(tagName) {
				// Пропускаем невалидные теги (можно вернуть ошибку, но пока просто игнорируем)
				continue
			}
			// Ищем или создаём тег
			tag := &models.Tag{}
			result := s.db.WithContext(ctx).Where("name = ?", tagName).First(tag)
			if result.Error != nil && errors.Is(result.Error, gorm.ErrRecordNotFound) {
				tag.Name = tagName
				if err := s.db.WithContext(ctx).Create(tag).Error; err != nil {
					return nil, fmt.Errorf("failed create tag: %w", err)
				}
			} else if result.Error != nil {
				return nil, fmt.Errorf("failed find tag: %w", result.Error)
			}
			// Создаём связь
			imageTag := &models.ImageTag{
				ImageID: img.ID,
				TagID:   tag.ID,
			}
			if err := s.db.WithContext(ctx).Create(imageTag).Error; err != nil {
				// Игнорируем ошибку дублирования связи
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
					continue
				}
				return nil, fmt.Errorf("failed create image-tag link: %w", err)
			}
		}
	}

	// Загружаем связи для возврата
	s.db.WithContext(ctx).Preload("TagsRel").First(img, img.ID)
	return img, nil
}

// TODO
func (s *Storage) DelImage(ctx context.Context, userID uint, imageID uint) error {

	return nil
	// return errors.New("failed")
}

func (s *Storage) GetImage(ctx context.Context, path string) (*models.Image, error) {
	i := &models.Image{}
	err := s.db.WithContext(ctx).Preload("TagsRel").Where("path = ?", path).First(i).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperror.ErrNotFoundData
		}
		return nil, fmt.Errorf("failed getting img by path `%s`: %w", path, err)
	}

	return i, nil
}

func (s *Storage) GetImages(ctx context.Context) ([]*models.Image, error) {
	images := []*models.Image{}
	err := s.db.WithContext(ctx).Preload("TagsRel").Find(&images).Error
	if err != nil {
		return nil, fmt.Errorf("failed getting images: %w", err)
	}

	return images, nil
}
