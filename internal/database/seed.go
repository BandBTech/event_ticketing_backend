package database

import (
	"event-ticketing-backend/internal/models"
	"fmt"
	"log"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SeedRoles creates default roles (permissions are initialized separately via API)
func SeedRoles(db *gorm.DB) error {
	log.Println("Seeding roles...")

	// Define basic roles without permissions (permissions are handled by permission service)
	roles := []models.Role{
		{
			ID:          uuid.New(),
			Name:        "admin",
			Description: "System administrator with full access",
		},
		{
			ID:          uuid.New(),
			Name:        "subadmin",
			Description: "Sub-administrator with elevated access",
		},
		{
			ID:          uuid.New(),
			Name:        "organizer",
			Description: "Event organizer",
		},
		{
			ID:          uuid.New(),
			Name:        "manager",
			Description: "Event manager",
		},
		{
			ID:          uuid.New(),
			Name:        "staff",
			Description: "Event staff",
		},
		{
			ID:          uuid.New(),
			Name:        "user",
			Description: "Regular user",
		},
	}

	for _, role := range roles {
		// Check if role already exists
		var existingRole models.Role
		if err := db.Where("name = ?", role.Name).First(&existingRole).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Role doesn't exist, create it
				if err := db.Create(&role).Error; err != nil {
					return fmt.Errorf("failed to create role %s: %w", role.Name, err)
				}
				log.Printf("Created role: %s", role.Name)
			} else {
				return fmt.Errorf("error checking for existing role %s: %w", role.Name, err)
			}
		} else {
			log.Printf("Role %s already exists, skipping...", role.Name)
		}
	}

	log.Println("Successfully seeded roles")
	return nil
}

// SeedAdminUser creates a default admin user
func SeedAdminUser(db *gorm.DB) error {
	log.Println("Seeding admin user...")

	// Check if admin user already exists
	var existingAdmin models.User
	if err := db.Where("email = ?", "admin@timroticket.com").First(&existingAdmin).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	} else {
		log.Println("Admin user already exists, skipping...")
		return nil
	}

	// Get admin role
	var adminRole models.Role
	if err := db.Where("name = ?", "admin").First(&adminRole).Error; err != nil {
		return fmt.Errorf("admin role not found: %w", err)
	}

	// Create admin user
	adminUser := models.User{
		Email:           "admin@timroticket.com",
		FirstName:       "Admin",
		LastName:        "User",
		IsEmailVerified: true,
		AccountStatus:   "active",
		Roles:           []*models.Role{&adminRole},
	}

	// Hash password
	if err := adminUser.HashPassword("admin123"); err != nil {
		return err
	}

	// Create user
	if err := db.Create(&adminUser).Error; err != nil {
		return err
	}

	log.Println("Admin user seeded successfully! Email: admin@timroticket.com, Password: admin123")
	return nil
}

func SeedSecondaryAdminUser(db *gorm.DB) error {
	log.Println("Seeding admin user...")

	// Check if admin user already exists
	var existingAdmin models.User
	if err := db.Where("email = ?", "ronit@thebandbtech.com").First(&existingAdmin).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	} else {
		log.Println("Admin user already exists, skipping...")
		return nil
	}

	// Get admin role
	var adminRole models.Role
	if err := db.Where("name = ?", "admin").First(&adminRole).Error; err != nil {
		return fmt.Errorf("admin role not found: %w", err)
	}

	// Create admin user
	adminUser := models.User{
		Email:           "ronit@thebandbtech.com",
		FirstName:       "Admin",
		LastName:        "User",
		IsEmailVerified: true,
		AccountStatus:   "active",
		Roles:           []*models.Role{&adminRole},
	}

	// Hash password
	if err := adminUser.HashPassword("Admin@123"); err != nil {
		return err
	}

	// Create user
	if err := db.Create(&adminUser).Error; err != nil {
		return err
	}

	log.Println("Admin user seeded successfully! Email: ronit@thebandbtech.com, Password: Admin@123")
	return nil
}
