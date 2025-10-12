package database

import (
	"fmt"
	"log"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Event = models.Event
type Organization = models.Organization
type Role = models.Role
type Permission = models.Permission
type User = models.User
type Token = models.Token

var DB *gorm.DB

func Connect(cfg *config.Config) error {
	dsn := cfg.GetDSN()

	// Configure GORM logger
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	if cfg.App.Env == "production" {
		gormConfig.Logger = logger.Default.LogMode(logger.Error)
	}

	// Connect to database
	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying SQL DB
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	// Set connection pool settings
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	DB = db
	log.Println("Database connected successfully")
	return nil
}

func Close() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func Migrate(models ...interface{}) error {
	// Create the uuid extension if it doesn't exist
	if err := DB.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";").Error; err != nil {
		log.Printf("Warning: Failed to create uuid-ossp extension: %v", err)
	}

	// Disable foreign key checks during migration
	disableForeignKeyChecks := DB.DisableForeignKeyConstraintWhenMigrating
	DB.DisableForeignKeyConstraintWhenMigrating = true

	// Perform the migration
	err := DB.AutoMigrate(models...)

	// Restore the previous foreign key check setting
	DB.DisableForeignKeyConstraintWhenMigrating = disableForeignKeyChecks

	if err != nil {
		return err
	}

	// Seed default roles and permissions
	return SeedRoles(DB)
}

func IsHealthy() bool {
	sqlDB, err := DB.DB()
	if err != nil {
		return false
	}
	return sqlDB.Ping() == nil
}

func GetDB() *gorm.DB {
	return DB
}
