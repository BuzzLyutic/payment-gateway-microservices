package model

import (
	"time"

	"github.com/google/uuid"
)

type Merchant struct {
	ID           int64          `json:"id" gorm:"primaryKey"`
	CompanyName  string         `json:"company_name" gorm:"uniqueIndex;not null"`
	Email        string         `json:"email" gorm:"uniqueIndex;not null"`
	PasswordHash string         `json:"-" gorm:"not null"` // "-" исключает из JSON
	APIKey       string         `json:"api_key" gorm:"uniqueIndex;not null"`
	Active       bool           `json:"active" gorm:"default:true"`
	Settings     map[string]any `json:"settings" gorm:"type:jsonb;default:'{}'"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// TableName переопределяет имя таблицы
func (Merchant) TableName() string {
	return "merchants"
}

// DTOs (Data Transfer Objects)

type RegisterRequest struct {
	CompanyName string `json:"company_name" validate:"required,min=3,max=100"`
	Email       string `json:"email" validate:"required,email"`
	Password    string `json:"password" validate:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type LoginResponse struct {
	Token    string           `json:"token"`
	APIKey   string           `json:"api_key"`
	Merchant *MerchantResponse `json:"merchant"`
}

type MerchantResponse struct {
	ID          int64     `json:"id"`
	CompanyName string    `json:"company_name"`
	Email       string    `json:"email"`
	APIKey      string    `json:"api_key"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

type ValidateResponse struct {
	Valid      bool  `json:"valid"`
	MerchantID int64 `json:"merchant_id,omitempty"`
}

type RegenerateAPIKeyResponse struct {
	APIKey string `json:"api_key"`
}

// ToResponse конвертирует Merchant в MerchantResponse
func (m *Merchant) ToResponse() *MerchantResponse {
	return &MerchantResponse{
		ID:          m.ID,
		CompanyName: m.CompanyName,
		Email:       m.Email,
		APIKey:      m.APIKey,
		Active:      m.Active,
		CreatedAt:   m.CreatedAt,
	}
}

// GenerateAPIKey генерирует новый API ключ
func GenerateAPIKey() string {
	return "pk_" + uuid.New().String()
}
