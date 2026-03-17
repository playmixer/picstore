package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type TRole string

type User struct {
	gorm.Model
	Username     string
	Email        string
	PasswordHash string
	RegSource    string
	Roles        []Role `gorm:"many2many:user_roles;"`
}

func (u User) MarshalBinary() ([]byte, error) {
	return json.Marshal(u)
}

func (u *User) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, u)
}

// HasRole проверяет, есть ли у пользователя указанная роль.
func (u *User) HasRole(role TRole) bool {
	for _, r := range u.Roles {
		if r.Name == role {
			return true
		}
	}
	return false
}

// IsPublisher проверяет, есть ли у пользователя роль publisher.
func (u *User) IsPublisher() bool {
	return u.HasRole(ROLE_PUBLISHER)
}

const (
	ROLE_ADMIN     TRole = "admin"
	ROLE_USER      TRole = "user"
	ROLE_PUBLISHER TRole = "publisher"
)

var (
	MapRole = map[string]TRole{
		"PIC_ADMIN":     ROLE_ADMIN,
		"PIC_PUBLISHER": ROLE_PUBLISHER,
		"PIC_USER":      ROLE_USER,
	}
)

func (r TRole) String() string {
	return string(r)
}

type Role struct {
	gorm.Model
	Name   TRole `gorm:"index:idx_role_name,unique"`
	Active bool
}

type Tag struct {
	gorm.Model
	Name string `gorm:"uniqueIndex"`
}

type ImageTag struct {
	ImageID uint `gorm:"primaryKey"`
	TagID   uint `gorm:"primaryKey"`
}

type Image struct {
	gorm.Model
	Path        string
	UserID      uint
	User        User
	IsPublic    bool
	IsEncrypted bool   `gorm:"default:false"`
	Salt        []byte `gorm:"type:bytea"` // соль для вывода ключа из пароля
	Nonce       []byte `gorm:"type:bytea"` // nonce для AES-GCM
	Tags        string // денормализованное поле, можно удалить позже
	TagsRel     []Tag  `gorm:"many2many:image_tags;"`

	// Поля для асинхронной обработки
	ProcessingStatus string     `gorm:"type:varchar(32);default:'pending'"`
	ProcessingError  string     `gorm:"type:text"`
	OriginalPath     string     `gorm:"type:varchar(512)"` // путь к исходному файлу
	TempStoragePath  string     `gorm:"type:varchar(512)"` // путь во временном хранилище
	ProcessedAt      *time.Time // время завершения обработки

	// Поля для превью
	PreviewPath  string `gorm:"type:varchar(512)"`
	PreviewSalt  []byte `gorm:"type:bytea"`
	PreviewNonce []byte `gorm:"type:bytea"`
}

type Images []*Image

func (i Images) MarshalBinary() ([]byte, error) {
	return json.Marshal(i)
}

func (i *Images) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, i)
}
