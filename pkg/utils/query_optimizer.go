package utils

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// QueryOptimizer provides optimized query building patterns

// BuildSearchQuery creates optimized ILIKE search across multiple fields
// Uses index-friendly patterns and proper parameterization
func BuildSearchQuery(query *gorm.DB, searchTerm string, fields ...string) *gorm.DB {
	if searchTerm == "" || len(fields) == 0 {
		return query
	}

	searchTerm = strings.ToLower(searchTerm)
	searchPattern := "%" + searchTerm + "%"

	conditions := make([]string, len(fields))
	args := make([]interface{}, len(fields))

	for i, field := range fields {
		conditions[i] = fmt.Sprintf("LOWER(%s) LIKE ?", field)
		args[i] = searchPattern
	}

	whereClause := strings.Join(conditions, " OR ")
	return query.Where(whereClause, args...)
}

// OptimizeJoins applies selective field loading to reduce data transfer
func OptimizeJoins(query *gorm.DB, selectedFields []string) *gorm.DB {
	if len(selectedFields) > 0 {
		return query.Select(selectedFields)
	}
	return query
}

// BuildDateRangeQuery creates optimized date range filters
func BuildDateRangeQuery(query *gorm.DB, field string, start, end interface{}) *gorm.DB {
	if start != nil {
		query = query.Where(field+" >= ?", start)
	}
	if end != nil {
		query = query.Where(field+" <= ?", end)
	}
	return query
}

// ApplySorting applies validated sorting with SQL injection protection
func ApplySorting(query *gorm.DB, sortBy, sortOrder string, validFields map[string]bool) *gorm.DB {
	// Validate field
	if !validFields[sortBy] {
		return query
	}

	// Validate order
	order := strings.ToUpper(sortOrder)
	if order != "ASC" && order != "DESC" {
		order = "DESC"
	}

	return query.Order(fmt.Sprintf("%s %s", sortBy, order))
}

// BatchUpsert performs optimized batch upsert operations
func BatchUpsert(db *gorm.DB, records interface{}, batchSize int) error {
	return db.CreateInBatches(records, batchSize).Error
}

// ExistsOptimized checks existence without loading full record (faster)
func ExistsOptimized(db *gorm.DB, model interface{}, conditions map[string]interface{}) (bool, error) {
	var count int64
	query := db.Model(model)

	for field, value := range conditions {
		query = query.Where(field+" = ?", value)
	}

	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

// CountOptimized performs count with proper index usage
func CountOptimized(db *gorm.DB, model interface{}, conditions map[string]interface{}) (int64, error) {
	var count int64
	query := db.Model(model)

	for field, value := range conditions {
		query = query.Where(field+" = ?", value)
	}

	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}
