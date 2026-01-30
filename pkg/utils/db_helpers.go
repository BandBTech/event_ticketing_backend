package utils

import (
	"gorm.io/gorm"
)

// FindOneByID finds a single record by ID
func FindOneByID(db *gorm.DB, model interface{}, id interface{}, preloads ...string) error {
	query := db
	for _, preload := range preloads {
		query = query.Preload(preload)
	}
	return query.Where("id = ?", id).First(model).Error
}

// FindOneByField finds a single record by a specific field
func FindOneByField(db *gorm.DB, model interface{}, field string, value interface{}, preloads ...string) error {
	query := db
	for _, preload := range preloads {
		query = query.Preload(preload)
	}
	return query.Where(field+" = ?", value).First(model).Error
}

// FindOneByMultipleFields finds a single record by multiple fields
func FindOneByMultipleFields(db *gorm.DB, model interface{}, conditions map[string]interface{}, preloads ...string) error {
	query := db
	for _, preload := range preloads {
		query = query.Preload(preload)
	}
	for field, value := range conditions {
		query = query.Where(field+" = ?", value)
	}
	return query.First(model).Error
}

// ExistsByField checks if a record exists by a specific field
func ExistsByField(db *gorm.DB, model interface{}, field string, value interface{}) bool {
	var count int64
	db.Model(model).Where(field+" = ?", value).Count(&count)
	return count > 0
}

// ExistsByMultipleFields checks if a record exists by multiple fields
func ExistsByMultipleFields(db *gorm.DB, model interface{}, conditions map[string]interface{}) bool {
	query := db.Model(model)
	for field, value := range conditions {
		query = query.Where(field+" = ?", value)
	}
	var count int64
	query.Count(&count)
	return count > 0
}

// ApplyPagination applies pagination to a GORM query
func ApplyPagination(query *gorm.DB, page, limit int) *gorm.DB {
	offset := (page - 1) * limit
	return query.Offset(offset).Limit(limit)
}
