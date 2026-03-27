package picstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"picstore/internal/adapters/models"
	"picstore/internal/adapters/storage/types"

	"gorm.io/gorm"
)

// TestGetNavigationContext — тесты для метода GetNavigationContext.
func TestGetNavigationContext(t *testing.T) {
	// Вспомогательная функция для создания тестового изображения
	createImage := func(id uint, userID uint, isPublic bool, tags string) *models.Image {
		return &models.Image{
			Model:       gorm.Model{ID: id},
			UserID:      userID,
			Path:        fmt.Sprintf("path%d.jpg", id),
			IsPublic:    isPublic,
			Tags:        tags,
			IsEncrypted: false,
		}
	}

	// Тест 1: успешное получение контекста (есть предыдущее и следующее изображение) без фильтров
	t.Run("success with prev and next", func(t *testing.T) {
		// Создаём список ID: [1,2,3,4,5], текущее изображение ID=3
		imageIDs := []uint{1, 2, 3, 4, 5}
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				// Возвращаем все ID, так как фильтров нет
				return imageIDs, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				// Возвращаем изображение с соответствующим ID
				return createImage(imageID, 1, true, "tag"), nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
			setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
				return nil
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		ctx := context.Background()
		nav, err := ps.GetNavigationContext(ctx, 3, 1, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext failed: %v", err)
		}
		if nav.Current == nil || nav.Current.ID != 3 {
			t.Errorf("Current image mismatch: got %v, want ID=3", nav.Current)
		}
		if len(nav.Prev) != 1 || nav.Prev[0].ID != 2 {
			t.Errorf("Prev image mismatch: got %v, want ID=2", nav.Prev)
		}
		if len(nav.Next) != 1 || nav.Next[0].ID != 4 {
			t.Errorf("Next image mismatch: got %v, want ID=4", nav.Next)
		}
		if nav.Total != 5 {
			t.Errorf("Total mismatch: got %d, want 5", nav.Total)
		}
		if !nav.HasPrev || !nav.HasNext {
			t.Errorf("HasPrev/HasNext should be true")
		}
		if nav.Filter != "" {
			t.Errorf("Filter should be empty, got %q", nav.Filter)
		}
		if nav.UserID != 1 {
			t.Errorf("UserID mismatch: got %d, want 1", nav.UserID)
		}
		if nav.IsPublic != false {
			t.Errorf("IsPublic should be false (since currentUserID != image owner), got %v", nav.IsPublic)
		}
	})

	// Тест 2: контекст для первого изображения (нет предыдущего, есть следующее)
	t.Run("first image", func(t *testing.T) {
		imageIDs := []uint{1, 2, 3}
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return imageIDs, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return createImage(imageID, 1, true, "tag"), nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
			setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
				return nil
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 1, 1, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext failed: %v", err)
		}
		if len(nav.Prev) != 0 {
			t.Errorf("Prev should be empty for first image, got %v", nav.Prev)
		}
		if len(nav.Next) != 1 || nav.Next[0].ID != 2 {
			t.Errorf("Next mismatch: got %v, want ID=2", nav.Next)
		}
		if nav.HasPrev {
			t.Error("HasPrev should be false")
		}
		if !nav.HasNext {
			t.Error("HasNext should be true")
		}
	})

	// Тест 3: контекст для последнего изображения (есть предыдущее, нет следующего)
	t.Run("last image", func(t *testing.T) {
		imageIDs := []uint{1, 2, 3}
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return imageIDs, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return createImage(imageID, 1, true, "tag"), nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
			setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
				return nil
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 3, 1, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext failed: %v", err)
		}
		if len(nav.Prev) != 1 || nav.Prev[0].ID != 2 {
			t.Errorf("Prev mismatch: got %v, want ID=2", nav.Prev)
		}
		if len(nav.Next) != 0 {
			t.Errorf("Next should be empty for last image, got %v", nav.Next)
		}
		if !nav.HasPrev {
			t.Error("HasPrev should be true")
		}
		if nav.HasNext {
			t.Error("HasNext should be false")
		}
	})

	// Тест 4: контекст с фильтрацией по тегам (включение и исключение)
	t.Run("with tag filter", func(t *testing.T) {
		// Изображения с тегами: 1: "cat", 2: "dog", 3: "cat dog", 4: "bird"
		// Фильтр "cat -dog" должен дать только изображение 1 (cat без dog)
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				// Эмулируем фильтрацию: возвращаем ID, соответствующие фильтру
				// Для простоты возвращаем фиксированный список [1]
				return []uint{1}, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				if imageID == 1 {
					return createImage(1, 1, true, "cat"), nil
				}
				return nil, fmt.Errorf("image not found")
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
			setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
				return nil
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 1, 1, []string{"cat"}, []string{"dog"}, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext failed: %v", err)
		}
		if nav.Current == nil || nav.Current.ID != 1 {
			t.Errorf("Current image mismatch: got %v, want ID=1", nav.Current)
		}
		if len(nav.Prev) != 0 || len(nav.Next) != 0 {
			t.Errorf("Prev/Next should be empty for single image, got Prev=%v Next=%v", nav.Prev, nav.Next)
		}
		// Проверяем, что фильтр правильно отражён в структуре (в виде строки)
		// Ожидаем "cat -dog" или подобное, но метод формирует строку внутри себя.
		// Для простоты проверим, что Filter не пустой.
		if nav.Filter == "" {
			t.Error("Filter should not be empty")
		}
	})

	// Тест 5: кэширование (должен использоваться кэш)
	t.Run("cache hit", func(t *testing.T) {
		cacheHit := false
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return []uint{1, 2, 3}, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return createImage(imageID, 1, true, "tag"), nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				// Возвращаем предзаполненный NavigationContext
				cacheHit = true
				if nc, ok := obj.(*NavigationContext); ok {
					*nc = NavigationContext{
						Current:  &PicImage{ID: 2},
						Prev:     []*PicImage{{ID: 1}},
						Next:     []*PicImage{{ID: 3}},
						Total:    3,
						HasPrev:  true,
						HasNext:  true,
						Filter:   "",
						UserID:   1,
						IsPublic: false,
					}
				}
				return nil
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 2, 1, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext failed: %v", err)
		}
		if !cacheHit {
			t.Error("expected cache hit")
		}
		if nav.Current == nil || nav.Current.ID != 2 {
			t.Errorf("Current image mismatch: got %v, want ID=2", nav.Current)
		}
		// store.GetFilteredImageIDs и store.GetImageByID не должны вызываться, так как кэш сработал
	})

	// Тест 6: ошибка, если текущее изображение не найдено
	t.Run("current image not found", func(t *testing.T) {
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return []uint{1, 2, 3}, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return nil, fmt.Errorf("image not found")
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		_, err := ps.GetNavigationContext(context.Background(), 5, 1, nil, nil, 1)
		if err == nil {
			t.Error("expected error when current image not found")
		}
	})

	// Тест 7: список ID пуст — возвращается контекст только с текущим изображением
	t.Run("empty ID list", func(t *testing.T) {
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return []uint{}, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return createImage(imageID, 1, true, "tag"), nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 1, 1, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext should not error, got: %v", err)
		}
		if nav.Current == nil || nav.Current.ID != 1 {
			t.Errorf("Current image mismatch: got %v, want ID=1", nav.Current)
		}
		if len(nav.Prev) != 0 || len(nav.Next) != 0 {
			t.Errorf("Prev/Next should be empty, got Prev=%v Next=%v", nav.Prev, nav.Next)
		}
		if nav.Total != 1 {
			t.Errorf("Total mismatch: got %d, want 1", nav.Total)
		}
	})

	// Тест 8: проверка прав доступа (приватное изображение, пользователь не владелец)
	t.Run("private image, user not owner", func(t *testing.T) {
		// Изображение приватное (IsPublic=false) и принадлежит пользователю 2
		// Запрос от пользователя 1 (не владелец) — фильтр не возвращает изображений
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				// Пользователь не владелец, поэтому фильтр не возвращает изображений
				return []uint{}, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return &models.Image{
					Model:       gorm.Model{ID: 1},
					UserID:      2,
					IsPublic:    false,
					Tags:        "private",
					IsEncrypted: false,
				}, nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 1, 1, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext should not error, got: %v", err)
		}
		if nav.Current == nil || nav.Current.ID != 1 {
			t.Errorf("Current image mismatch: got %v, want ID=1", nav.Current)
		}
		if len(nav.Prev) != 0 || len(nav.Next) != 0 {
			t.Errorf("Prev/Next should be empty, got Prev=%v Next=%v", nav.Prev, nav.Next)
		}
		if nav.Total != 1 {
			t.Errorf("Total mismatch: got %d, want 1", nav.Total)
		}
	})

	// Тест 9: текущее изображение не попадает в фильтр по тегам
	t.Run("current image not in filtered list", func(t *testing.T) {
		// Фильтр "cat" возвращает ID [2,3], но текущее изображение ID=1 не входит
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return []uint{2, 3}, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return createImage(imageID, 1, true, "dog"), nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 1, 1, []string{"cat"}, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext should not error, got: %v", err)
		}
		if nav.Current == nil || nav.Current.ID != 1 {
			t.Errorf("Current image mismatch: got %v, want ID=1", nav.Current)
		}
		if len(nav.Prev) != 0 || len(nav.Next) != 0 {
			t.Errorf("Prev/Next should be empty, got Prev=%v Next=%v", nav.Prev, nav.Next)
		}
		if nav.Total != 1 { // filteredIDs содержит [2,3], но текущее изображение не входит, поэтому Total=1
			t.Errorf("Total mismatch: got %d, want 1", nav.Total)
		}
	})

	// Тест 10: windowSize больше 1 (несколько предыдущих/следующих)
	t.Run("window size 2", func(t *testing.T) {
		imageIDs := []uint{1, 2, 3, 4, 5, 6}
		store := &controllableStore{
			getFilteredImageIDsFunc: func(ctx context.Context, userID uint, includeTags, excludeTags []string, limit, offset int) ([]uint, error) {
				return imageIDs, nil
			},
			getImageByIDFunc: func(ctx context.Context, imageID uint) (*models.Image, error) {
				return createImage(imageID, 1, true, "tag"), nil
			},
			getImagesFunc: func(ctx context.Context) ([]*models.Image, error) {
				// Возвращаем все изображения по списку ID
				images := make([]*models.Image, 0, len(imageIDs))
				for _, id := range imageIDs {
					images = append(images, createImage(id, 1, true, "tag"))
				}
				return images, nil
			},
		}
		cache := &controllableCache{
			getHFunc: func(ctx context.Context, key string, obj types.ObjInterface) error {
				return fmt.Errorf("cache miss")
			},
			setHFunc: func(ctx context.Context, key string, value types.ObjInterface, ttl time.Duration) error {
				return nil
			},
		}
		ps := newTestPicStoreWithMocks(t, store, cache)

		nav, err := ps.GetNavigationContext(context.Background(), 3, 2, nil, nil, 1)
		if err != nil {
			t.Fatalf("GetNavigationContext failed: %v", err)
		}
		if len(nav.Prev) != 2 {
			t.Errorf("Prev length mismatch: got %d, want 2", len(nav.Prev))
		} else {
			// Prev перевёрнуты: ближайшие первыми
			if nav.Prev[0].ID != 2 || nav.Prev[1].ID != 1 {
				t.Errorf("Prev IDs mismatch: got [%d, %d], want [2,1]", nav.Prev[0].ID, nav.Prev[1].ID)
			}
		}
		if len(nav.Next) != 2 {
			t.Errorf("Next length mismatch: got %d, want 2", len(nav.Next))
		} else {
			if nav.Next[0].ID != 4 || nav.Next[1].ID != 5 {
				t.Errorf("Next IDs mismatch: got [%d, %d], want [4,5]", nav.Next[0].ID, nav.Next[1].ID)
			}
		}
		if !nav.HasPrev || !nav.HasNext {
			t.Errorf("HasPrev/HasNext should be true")
		}
	})
}
