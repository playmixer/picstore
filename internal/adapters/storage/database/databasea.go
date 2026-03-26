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
		if errors.Is(err, gorm.ErrRecordNotFound) {
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

func (s *Storage) NewImageWithStatus(ctx context.Context, userID uint, path, previewPath string, isPublic bool, tags string,
	isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
	processingStatus, originalPath, tempStoragePath string) (*models.Image, error) {
	// Создаём изображение с расширенными полями
	img := &models.Image{
		UserID:           userID,
		Path:             path,
		PreviewPath:      previewPath,
		IsPublic:         isPublic,
		Tags:             tags,
		IsEncrypted:      isEncrypted,
		Salt:             salt,
		Nonce:            nonce,
		PreviewSalt:      previewSalt,
		PreviewNonce:     previewNonce,
		ProcessingStatus: processingStatus,
		OriginalPath:     originalPath,
		TempStoragePath:  tempStoragePath,
	}
	err := s.db.WithContext(ctx).Save(img).Error
	if err != nil {
		return nil, fmt.Errorf("failed save image to db: %w", err)
	}

	// Обрабатываем теги
	if tags != "" {
		tagNames := strings.Fields(tags)
		validTagRegex := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
		for _, tagName := range tagNames {
			tagName = strings.TrimSpace(tagName)
			if tagName == "" {
				continue
			}
			tagName = strings.ToLower(tagName)
			if !validTagRegex.MatchString(tagName) {
				continue
			}
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
			imageTag := &models.ImageTag{
				ImageID: img.ID,
				TagID:   tag.ID,
			}
			if err := s.db.WithContext(ctx).Create(imageTag).Error; err != nil {
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

func (s *Storage) UpdateProcessingStatus(ctx context.Context, imageID uint, status string, errorMsg *string) error {
	updateData := map[string]interface{}{
		"processing_status": status,
	}
	if errorMsg != nil {
		updateData["processing_error"] = *errorMsg
	}
	if status == "completed" || status == "failed" {
		now := time.Now()
		updateData["processed_at"] = &now
	}
	err := s.db.WithContext(ctx).Model(&models.Image{}).Where("id = ?", imageID).Updates(updateData).Error
	if err != nil {
		return fmt.Errorf("failed update processing status: %w", err)
	}
	return nil
}

func (s *Storage) UpdateImageAfterProcessing(ctx context.Context, imageID uint, finalPath, previewPath string,
	isEncrypted bool, salt, nonce, previewSalt, previewNonce []byte,
	status string, processedAt *time.Time) error {
	updateData := map[string]interface{}{
		"path":              finalPath,
		"preview_path":      previewPath,
		"is_encrypted":      isEncrypted,
		"salt":              salt,
		"nonce":             nonce,
		"preview_salt":      previewSalt,
		"preview_nonce":     previewNonce,
		"processing_status": status,
		"processed_at":      processedAt,
		"temp_storage_path": "", // очищаем временный путь
		"original_path":     "", // очищаем оригинальный путь
	}
	err := s.db.WithContext(ctx).Model(&models.Image{}).Where("id = ?", imageID).Updates(updateData).Error
	if err != nil {
		return fmt.Errorf("failed update image after processing: %w", err)
	}
	return nil
}

func (s *Storage) GetImageByID(ctx context.Context, imageID uint) (*models.Image, error) {
	img := &models.Image{}
	err := s.db.WithContext(ctx).Preload("TagsRel").Where("id = ?", imageID).First(img).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.Join(apperror.ErrNotFoundData, err)
		}
		return nil, fmt.Errorf("failed find image by id: %w", err)
	}
	return img, nil
}

func (s *Storage) GetImage(ctx context.Context, path string) (*models.Image, error) {
	img := &models.Image{}
	err := s.db.WithContext(ctx).Preload("TagsRel").Where("path = ? or preview_path = ?", path, path).First(img).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.Join(apperror.ErrNotFoundData, err)
		}
		return nil, fmt.Errorf("failed find image by path: %w", err)
	}
	return img, nil
}

func (s *Storage) GetImages(ctx context.Context) ([]*models.Image, error) {
	var images []*models.Image
	err := s.db.WithContext(ctx).Preload("TagsRel").Order("created_at DESC").Find(&images).Error
	if err != nil {
		return nil, fmt.Errorf("failed get images: %w", err)
	}
	return images, nil
}

func (s *Storage) DelImage(ctx context.Context, userID uint, imageID uint) error {
	result := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", imageID, userID).Delete(&models.Image{})
	if result.Error != nil {
		return fmt.Errorf("failed delete image: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.ErrNotFoundData
	}
	return nil
}

func (s *Storage) DelImages(ctx context.Context, userID uint, imageIDs []uint) error {
	result := s.db.WithContext(ctx).Where("user_id = ? AND id IN ?", userID, imageIDs).Delete(&models.Image{})
	if result.Error != nil {
		return fmt.Errorf("failed delete images: %w", result.Error)
	}
	return nil
}

func (s *Storage) UpdateImage(ctx context.Context, userID uint, imageID uint, isPublic *bool, tags *string) error {
	// Начинаем транзакцию
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer tx.Rollback()

	// Обновляем основные поля изображения
	updateData := make(map[string]interface{})
	if isPublic != nil {
		updateData["is_public"] = *isPublic
	}
	if tags != nil {
		updateData["tags"] = *tags
	}
	if len(updateData) == 0 {
		tx.Rollback()
		return nil
	}
	result := tx.Model(&models.Image{}).Where("id = ? AND user_id = ?", imageID, userID).Updates(updateData)
	if result.Error != nil {
		return fmt.Errorf("failed update image: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return apperror.ErrNotFoundData
	}

	// Если обновляются теги, пересоздаём связи
	if tags != nil {
		// Удаляем все существующие связи ImageTag для этого изображения
		if err := tx.Where("image_id = ?", imageID).Delete(&models.ImageTag{}).Error; err != nil {
			return fmt.Errorf("failed to delete old image-tag links: %w", err)
		}

		// Обрабатываем новые теги (аналогично NewImage)
		tagStr := *tags
		if tagStr != "" {
			tagNames := strings.Fields(tagStr) // разделяем по пробелам
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
				// Ищем или создаём тег (регистронезависимо)
				tag := &models.Tag{}
				result := tx.Where("name = ?", tagName).First(tag)
				if result.Error != nil && errors.Is(result.Error, gorm.ErrRecordNotFound) {
					tag.Name = tagName
					if err := tx.Create(tag).Error; err != nil {
						return fmt.Errorf("failed create tag: %w", err)
					}
				} else if result.Error != nil {
					return fmt.Errorf("failed find tag: %w", result.Error)
				}
				// Создаём связь
				imageTag := &models.ImageTag{
					ImageID: imageID,
					TagID:   tag.ID,
				}
				if err := tx.Create(imageTag).Error; err != nil {
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

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed commit transaction: %w", err)
	}
	return nil
}

func (s *Storage) IncrementViews(ctx context.Context, imageID uint, delta uint) error {
	result := s.db.WithContext(ctx).Model(&models.Image{}).Where("id = ?", imageID).UpdateColumn("views", gorm.Expr("views + ?", delta))
	if result.Error != nil {
		return fmt.Errorf("failed increment views: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.ErrNotFoundData
	}
	return nil
}

func (s *Storage) GetFilteredImageIDs(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
	// Базовый запрос: изображения пользователя (или публичные)
	query := s.db.WithContext(ctx).Model(&models.Image{}).Select("images.id")
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	} else {
		query = query.Where("is_public = ?", true)
		// В публичном контексте исключаем зашифрованные изображения и неудачные обработки
		query = query.Where("is_encrypted = ?", false)
	}
	// Исключаем неудачные обработки для всех контекстов
	query = query.Where("processing_status IS NULL OR processing_status != ?", "failed")

	// Фильтр includeTags: изображения должны иметь все указанные теги
	for _, tag := range includeTags {
		subQuery := s.db.WithContext(ctx).Model(&models.ImageTag{}).
			Select("image_id").
			Joins("JOIN tags ON image_tags.tag_id = tags.id").
			Where("tags.name = ?", strings.ToLower(tag))
		query = query.Where("images.id IN (?)", subQuery)
	}

	// Фильтр excludeTags: изображения не должны иметь ни одного из указанных тегов
	if len(excludeTags) > 0 {
		subQuery := s.db.WithContext(ctx).Model(&models.ImageTag{}).
			Select("image_id").
			Joins("JOIN tags ON image_tags.tag_id = tags.id").
			Where("tags.name IN ?", excludeTags)
		query = query.Where("images.id NOT IN (?)", subQuery)
	}

	// Пагинация
	query = query.Order("images.created_at DESC").Limit(limit).Offset(offset)

	var ids []uint
	err := query.Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get filtered image IDs: %w", err)
	}
	return ids, nil
}

func (s *Storage) GetPostsPage(ctx context.Context, userID uint, isPublic bool, includeTags, excludeTags []string, limit, offset int) ([]*models.Image, int64, error) {
	query := s.db.WithContext(ctx).Model(&models.Image{}).Preload("TagsRel")
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	} else if isPublic {
		query = query.Where("is_public = ?", true)
	}
	// исключаем зашифрованные изображения и неудачные обработки
	query = query.Where("is_encrypted = ?", false)
	query = query.Where("processing_status IS NULL OR processing_status != ?", "failed")
	// фильтр includeTags
	for _, tag := range includeTags {
		subQuery := s.db.WithContext(ctx).Model(&models.ImageTag{}).
			Select("image_id").
			Joins("JOIN tags ON image_tags.tag_id = tags.id").
			Where("tags.name = ?", strings.ToLower(tag))
		query = query.Where("images.id IN (?)", subQuery)
	}
	// фильтр excludeTags
	if len(excludeTags) > 0 {
		subQuery := s.db.WithContext(ctx).Model(&models.ImageTag{}).
			Select("image_id").
			Joins("JOIN tags ON image_tags.tag_id = tags.id").
			Where("tags.name IN ?", excludeTags)
		query = query.Where("images.id NOT IN (?)", subQuery)
	}
	// подсчёт total
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count posts: %w", err)
	}
	// получение данных
	var images []*models.Image
	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&images).Error
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get posts page: %w", err)
	}
	return images, total, nil
}
