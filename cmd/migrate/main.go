package main

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"fmt"
	"log"
	"os"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Connect to database
	if err := database.Connect(cfg); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	db := database.GetDB()

	fmt.Println("Starting database migration to fix UUID issues...")

	// Enable UUID extension
	fmt.Println("Enabling UUID extension...")
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";").Error; err != nil {
		log.Printf("Warning: Failed to create uuid-ossp extension: %v", err)
	}

	// Check if events table has integer ID
	var dataType string
	err = db.Raw(`
		SELECT data_type 
		FROM information_schema.columns 
		WHERE table_name = 'events' AND column_name = 'id'
	`).Scan(&dataType).Error

	if err == nil && dataType == "integer" {
		fmt.Println("Found events table with integer ID. Converting to UUID...")

		// Drop the events table to recreate it with UUID
		if err := db.Exec("DROP TABLE IF EXISTS events CASCADE;").Error; err != nil {
			log.Printf("Warning: Failed to drop events table: %v", err)
		}

		fmt.Println("Events table dropped. Recreating with UUID...")
	}

	// Migrate all models
	fmt.Println("Running database migrations...")
	if err := database.Migrate(
		&models.Role{},
		&models.Permission{},
		&models.User{},
		&models.Organization{},
		&models.Event{},
		&models.EventTier{},
		&models.OrganizerTierTemplate{},
		&models.IndividualTicket{},
		&models.GuestUser{},
		&models.Token{},
		&models.OTP{},
		&models.FileStorage{},
		&models.EmailJob{},
		&models.RegistrationRequest{},
		&models.OrganizerOnboarding{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	fmt.Println("Database migration completed successfully!")

	// Verify the events table structure
	var result struct {
		ColumnName string
		DataType   string
	}

	if err := db.Raw(`
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = 'events' AND column_name = 'id'
	`).Scan(&result).Error; err == nil {
		fmt.Printf("Events table ID column: %s (%s)\n", result.ColumnName, result.DataType)
		if result.DataType == "uuid" {
			fmt.Println("✅ Events table now uses UUID for ID column!")
		} else {
			fmt.Printf("⚠️  Events table ID is still %s, not UUID\n", result.DataType)
		}
	}

	// Seed default data if needed
	if len(os.Args) > 1 && os.Args[1] == "--seed" {
		fmt.Println("Seeding default roles and permissions...")
		if err := database.SeedRoles(db); err != nil {
			log.Printf("Warning: Failed to seed roles: %v", err)
		} else {
			fmt.Println("Default roles and permissions seeded successfully!")
		}
	}

	fmt.Println("Migration script completed!")
}
