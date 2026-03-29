package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/auth-service/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/auth-service/internal/model"
)

func NewPostgresDB(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	// Конфигурация GORM logger
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Настройка connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxConns)
	sqlDB.SetMaxIdleConns(cfg.MinConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Проверка подключения
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Successfully connected to PostgreSQL")

	return db, nil
}

// AutoMigrate выполняет миграции
func AutoMigrate(db *gorm.DB) error {
	log.Println("Running database migrations...")
	
	if err := db.AutoMigrate(&model.Merchant{}); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}
	
	log.Println("Database migrations completed")
	return nil
}
