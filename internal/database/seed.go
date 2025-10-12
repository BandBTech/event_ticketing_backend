package database

import (
	"event-ticketing-backend/internal/models"
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
	}

	userPermissions := []models.Permission{
		{Name: "create:user", Description: "Create users", Resource: "users", Action: "create"},
		{Name: "read:user", Description: "View users", Resource: "users", Action: "read"},
		{Name: "update:user", Description: "Update users", Resource: "users", Action: "update"},
		{Name: "delete:user", Description: "Delete users", Resource: "users", Action: "delete"},
	}

	organizerPermissions := []models.Permission{
		{Name: "manage:staff", Description: "Manage staff members", Resource: "staff", Action: "manage"},
	}

	// Create permissions
	for _, perm := range append(append(eventPermissions, userPermissions...), organizerPermissions...) {
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

	// Create organizer role with event and staff permissions
	organizerRole := models.Role{
		Name:        "organizer",
		Description: "Event organizer with event management permissions",
	}

	var existingOrganizerRole models.Role
	if err := db.Where("name = ?", organizerRole.Name).First(&existingOrganizerRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&organizerRole).Error; err != nil {
				return err
			}

			// Add relevant permissions to organizer
			var organizerPerms []models.Permission
			if err := db.Where("resource IN ?", []string{"events", "staff"}).Find(&organizerPerms).Error; err != nil {
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
		Description: "Staff with limited event permissions",
	}

	var existingStaffRole models.Role
	if err := db.Where("name = ?", staffRole.Name).First(&existingStaffRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&staffRole).Error; err != nil {
				return err
			}

			// Add read-only event permissions to staff
			var staffPerms []models.Permission
			if err := db.Where("name IN ?", []string{"read:event", "read:user"}).Find(&staffPerms).Error; err != nil {
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
		Description: "Organization manager with expanded permissions",
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
					"create:event", "manage:staff"}).Find(&managerPerms).Error; err != nil {
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
		Description: "Regular user with basic permissions",
	}

	var existingUserRole models.Role
	if err := db.Where("name = ?", userRole.Name).First(&existingUserRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&userRole).Error; err != nil {
				return err
			}

			// Add basic permissions to user
			var userPerms []models.Permission
			if err := db.Where("name = ?", "read:event").Find(&userPerms).Error; err != nil {
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
