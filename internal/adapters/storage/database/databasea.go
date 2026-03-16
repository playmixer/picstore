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
	err := s.db.WithContext(ctx).Preload("Roles").Where("id = ?", userID).First(user).Error
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

func (s *Storage) NewImage(ctx context.Context, userID uint, path string, isPublic bool, tags string, isEncrypted bool, salt, nonce []byte) (*models.Image, error) {
	// Создаём изображение
	img := &models.Image{
		UserID:      userID,
		Path:        path,
		IsPublic:    isPublic,
		Tags:        tags, // сохраняем исходную строку для обратной совместимости
		IsEncrypted: isEncrypted,
		Salt:        salt,
		Nonce:       nonce,
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
			tagName = strings.ToLower(tagName) // нормализуем к нижнему регистру
			if !validTagRegex.MatchString(tagName) {
				// Пропускаем невалидные теги (можно вернуть ошибку, но пока просто игнорируем)
				continue
			}
			// Ищем или создаём тег (регистронезависимо)
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
	err := s.db.WithContext(ctx).Preload("TagsRel").Order("created_at DESC").Find(&images).Error
	if err != nil {
		return nil, fmt.Errorf("failed getting images: %w", err)
	}

	return images, nil
}

// DelImage удаляет изображение по ID, принадлежащее указанному пользователю.
func (s *Storage) DelImage(ctx context.Context, userID uint, imageID uint) error {
	// Сначала проверим, существует ли изображение и принадлежит ли пользователю
	img := &models.Image{}
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", imageID, userID).First(img).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperror.ErrNotFoundData
		}
		return fmt.Errorf("failed find image: %w", err)
	}
	// Удаляем связи с тегами
	if err := s.db.WithContext(ctx).Where("image_id = ?", imageID).Delete(&models.ImageTag{}).Error; err != nil {
		return fmt.Errorf("failed delete image tags: %w", err)
	}
	// Удаляем само изображение
	if err := s.db.WithContext(ctx).Delete(img).Error; err != nil {
		return fmt.Errorf("failed delete image: %w", err)
	}
	// TODO: удалить физический файл
	return nil
}

// DelImages удаляет несколько изображений по IDs, принадлежащих указанному пользователю.
func (s *Storage) DelImages(ctx context.Context, userID uint, imageIDs []uint) error {
	if len(imageIDs) == 0 {
		return nil
	}
	// Проверяем, что все изображения принадлежат пользователю и существуют
	var count int64
	err := s.db.WithContext(ctx).Model(&models.Image{}).Where("id IN ? AND user_id = ?", imageIDs, userID).Count(&count).Error
	if err != nil {
		return fmt.Errorf("failed count images: %w", err)
	}
	if int(count) != len(imageIDs) {
		return apperror.ErrNotFoundData
	}
	// Удаляем связи с тегами
	if err := s.db.WithContext(ctx).Where("image_id IN ?", imageIDs).Delete(&models.ImageTag{}).Error; err != nil {
		return fmt.Errorf("failed delete image tags: %w", err)
	}
	// Удаляем сами изображения
	if err := s.db.WithContext(ctx).Where("id IN ?", imageIDs).Delete(&models.Image{}).Error; err != nil {
		return fmt.Errorf("failed delete images: %w", err)
	}
	// TODO: удалить физические файлы
	return nil
}

// UpdateImage обновляет публичность и/или теги изображения.
// Если isPublic == nil, поле не обновляется. Если tags == nil, теги не меняются.
func (s *Storage) UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
	// Проверяем существование изображения и принадлежность пользователю
	img := &models.Image{}
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", imageID, userID).First(img).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperror.ErrNotFoundData
		}
		return fmt.Errorf("failed find image: %w", err)
	}

	// Обновляем IsPublic, если передано
	if isPublic != nil {
		img.IsPublic = *isPublic
	}
	// Обновляем теги, если передано
	if tags != nil {
		img.Tags = *tags
		// Удаляем старые связи с тегами
		if err := s.db.WithContext(ctx).Where("image_id = ?", imageID).Delete(&models.ImageTag{}).Error; err != nil {
			return fmt.Errorf("failed delete old image tags: %w", err)
		}
		// Создаём новые связи (аналогично NewImage)
		if *tags != "" {
			tagNames := strings.Fields(*tags)
			validTagRegex := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
			for _, tagName := range tagNames {
				tagName = strings.TrimSpace(tagName)
				if tagName == "" {
					continue
				}
				tagName = strings.ToLower(tagName) // нормализуем к нижнему регистру
				if !validTagRegex.MatchString(tagName) {
					continue
				}
				tag := &models.Tag{}
				result := s.db.WithContext(ctx).Where("name = ?", tagName).First(tag)
				if result.Error != nil && errors.Is(result.Error, gorm.ErrRecordNotFound) {
					tag.Name = tagName
					if err := s.db.WithContext(ctx).Create(tag).Error; err != nil {
						return fmt.Errorf("failed create tag: %w", err)
					}
				} else if result.Error != nil {
					return fmt.Errorf("failed find tag: %w", result.Error)
				}
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
					return fmt.Errorf("failed create image-tag link: %w", err)
				}
			}
		}
	}
	// Сохраняем изменения в изображении
	if err := s.db.WithContext(ctx).Save(img).Error; err != nil {
		return fmt.Errorf("failed update image: %w", err)
	}
	return nil
}
