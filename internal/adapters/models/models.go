package models

import (
	"encoding/json"

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
	Path     string
	UserID   uint
	User     User
	IsPublic bool
	Tags     string // денормализованное поле, можно удалить позже
	TagsRel  []Tag  `gorm:"many2many:image_tags;"`
}

type Images []*Image

func (i Images) MarshalBinary() ([]byte, error) {
	return json.Marshal(i)
}

func (i *Images) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, i)
}
