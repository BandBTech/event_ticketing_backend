package database

import (
	"event-ticketing-backend/internal/models"
	"fmt"
	"log"

	"gorm.io/gorm"
)

// SeedRoles creates default roles and permissions
func SeedRoles(db *gorm.DB) error {
	log.Println("Seeding initial roles and permissions...")

	// Define default permissions
	eventPermissions := []models.Permission{
		{Name: "create:event", Description: "Create events", Resource: "events", Action: "create"},
		{Name: "read:event", Description: "View events", Resource: "events", Action: "read"},
		{Name: "update:event", Description: "Update events", Resource: "events", Action: "update"},
		{Name: "delete:event", Description: "Delete events", Resource: "events", Action: "delete"},
		{Name: "approve:event", Description: "Approve events", Resource: "events", Action: "approve"},
		{Name: "hold:event", Description: "Hold events", Resource: "events", Action: "hold"},
		{Name: "reject:event", Description: "Reject events", Resource: "events", Action: "reject"},
	}

	userPermissions := []models.Permission{
		{Name: "create:user", Description: "Create users", Resource: "users", Action: "create"},
		{Name: "read:user", Description: "View users", Resource: "users", Action: "read"},
		{Name: "update:user", Description: "Update users", Resource: "users", Action: "update"},
		{Name: "delete:user", Description: "Delete users", Resource: "users", Action: "delete"},
		{Name: "approve:organizer", Description: "Approve organizers", Resource: "organizers", Action: "approve"},
		{Name: "reject:organizer", Description: "Reject organizers", Resource: "organizers", Action: "reject"},
	}

	organizerPermissions := []models.Permission{
		{Name: "manage:staff", Description: "Manage staff members", Resource: "staff", Action: "manage"},
	}

	ticketPermissions := []models.Permission{
		{Name: "create:ticket", Description: "Purchase tickets", Resource: "tickets", Action: "create"},
		{Name: "read:ticket", Description: "View tickets", Resource: "tickets", Action: "read"},
		{Name: "scan:ticket", Description: "Scan tickets for check-in/check-out", Resource: "tickets", Action: "scan"},
		{Name: "checkin:ticket", Description: "Check-in tickets", Resource: "tickets", Action: "checkin"},
		{Name: "checkout:ticket", Description: "Check-out tickets", Resource: "tickets", Action: "checkout"},
	}

	// Create permissions
	for _, perm := range append(append(append(eventPermissions, userPermissions...), organizerPermissions...), ticketPermissions...) {
		var existingPerm models.Permission
		if err := db.Where("name = ?", perm.Name).First(&existingPerm).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				if err := db.Create(&perm).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}

	// Create admin role with all permissions
	adminRole := models.Role{
		Name:        "admin",
		Description: "Administrator with all permissions",
	}

	var existingAdminRole models.Role
	if err := db.Where("name = ?", adminRole.Name).First(&existingAdminRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&adminRole).Error; err != nil {
				return err
			}

			// Add all permissions to admin
			var allPermissions []models.Permission
			if err := db.Find(&allPermissions).Error; err != nil {
				return err
			}

			if err := db.Model(&adminRole).Association("Permissions").Replace(allPermissions); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Create subadmin role with event approval permissions
	subadminRole := models.Role{
		Name:        "subadmin",
		Description: "Sub-administrator with event approval permissions",
	}

	var existingSubadminRole models.Role
	if err := db.Where("name = ?", subadminRole.Name).First(&existingSubadminRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&subadminRole).Error; err != nil {
				return err
			}

			// Add event management and organizer approval permissions to subadmin
			var subadminPerms []models.Permission
			if err := db.Where("resource IN ? OR name IN ?", []string{"events", "organizers"}, []string{"read:user"}).Find(&subadminPerms).Error; err != nil {
				return err
			}

			if err := db.Model(&subadminRole).Association("Permissions").Replace(subadminPerms); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Create organizer role with event and staff permissions
	organizerRole := models.Role{
		Name:        "organizer",
		Description: "Event organizer with event and ticket management permissions",
	}

	var existingOrganizerRole models.Role
	if err := db.Where("name = ?", organizerRole.Name).First(&existingOrganizerRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&organizerRole).Error; err != nil {
				return err
			}

			// Add relevant permissions to organizer
			var organizerPerms []models.Permission
			if err := db.Where("resource IN ? OR name LIKE ?", []string{"events", "staff"}, "%ticket%").Find(&organizerPerms).Error; err != nil {
				return err
			}

			if err := db.Model(&organizerRole).Association("Permissions").Replace(organizerPerms); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Create staff role
	staffRole := models.Role{
		Name:        "staff",
		Description: "Staff with limited event and ticket permissions",
	}

	var existingStaffRole models.Role
	if err := db.Where("name = ?", staffRole.Name).First(&existingStaffRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&staffRole).Error; err != nil {
				return err
			}

			// Add read-only event permissions and ticket scanning to staff
			var staffPerms []models.Permission
			if err := db.Where("name IN ?", []string{"read:event", "read:user", "scan:ticket", "checkin:ticket", "checkout:ticket"}).Find(&staffPerms).Error; err != nil {
				return err
			}

			if err := db.Model(&staffRole).Association("Permissions").Replace(staffPerms); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Create manager role
	managerRole := models.Role{
		Name:        "manager",
		Description: "Organization manager with expanded permissions including ticket management",
	}

	var existingManagerRole models.Role
	if err := db.Where("name = ?", managerRole.Name).First(&existingManagerRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&managerRole).Error; err != nil {
				return err
			}

			// Add permissions to manager (more than staff, fewer than organizer)
			var managerPerms []models.Permission
			if err := db.Where("name IN ?",
				[]string{
					"read:event", "read:user", "update:event",
					"create:event", "manage:staff", "read:ticket",
					"scan:ticket", "checkin:ticket", "checkout:ticket"}).Find(&managerPerms).Error; err != nil {
				return err
			}

			if err := db.Model(&managerRole).Association("Permissions").Replace(managerPerms); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Create user role
	userRole := models.Role{
		Name:        "user",
		Description: "Regular user with basic permissions including ticket purchase",
	}

	var existingUserRole models.Role
	if err := db.Where("name = ?", userRole.Name).First(&existingUserRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&userRole).Error; err != nil {
				return err
			}

			// Add basic permissions to user
			var userPerms []models.Permission
			if err := db.Where("name IN ?", []string{"read:event", "create:ticket", "read:ticket"}).Find(&userPerms).Error; err != nil {
				return err
			}

			if err := db.Model(&userRole).Association("Permissions").Replace(userPerms); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	log.Println("Roles and permissions seeded successfully!")
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
