package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FileCategory represents different categories/types of files
type FileCategory string

const (
	FileCategoryEventBanner     FileCategory = "event_banner"
	FileCategoryEventThumbnail  FileCategory = "event_thumbnail"
	FileCategoryOrganizerLogo   FileCategory = "organizer_logo"
	FileCategoryOrganizerBanner FileCategory = "organizer_banner"
	FileCategoryUserAvatar      FileCategory = "user_avatar"
	FileCategoryCompanyLogo     FileCategory = "company_logo"
	FileCategoryCategoryIcon    FileCategory = "category_icon"
	FileCategoryDocument        FileCategory = "document"
	FileCategoryOther           FileCategory = "other"
)

// FileStorage represents a file stored in S3
type FileStorage struct {
	ID           uuid.UUID    `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	FileName     string       `gorm:"not null;size:255" json:"file_name"`
	OriginalName string       `gorm:"not null;size:255" json:"original_name"`
	FileSize     int64        `gorm:"not null" json:"file_size"` // in bytes
	MimeType     string       `gorm:"not null;size:100" json:"mime_type"`
	FilePath     string       `gorm:"not null;size:500" json:"file_path"` // S3 key/path
	PublicURL    string       `gorm:"not null;size:500" json:"public_url"`
	BucketName   string       `gorm:"not null;size:100" json:"bucket_name"`
	Region       string       `gorm:"not null;size:50" json:"region"`
	Category     FileCategory `gorm:"not null;type:varchar(50);default:'other'" json:"category"`

	// Optional associations (nullable foreign keys)
	EventID     *uuid.UUID `gorm:"type:uuid;index" json:"event_id,omitempty"`
	OrganizerID *uuid.UUID `gorm:"type:uuid;index" json:"organizer_id,omitempty"`
	UserID      *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	CompanyID   *uuid.UUID `gorm:"type:uuid;index" json:"company_id,omitempty"`
	CategoryID  *uuid.UUID `gorm:"type:uuid;index" json:"category_id,omitempty"`

	// Metadata
	Width       *int     `json:"width,omitempty"`  // for images
	Height      *int     `json:"height,omitempty"` // for images
	AltText     string   `gorm:"size:255" json:"alt_text"`
	Description string   `gorm:"type:text" json:"description"`
	Tags        []string `gorm:"type:text[]" json:"tags"`

	// Status and control
	IsPublic   bool      `gorm:"not null;default:true" json:"is_public"`
	IsActive   bool      `gorm:"not null;default:true" json:"is_active"`
	UploadedBy uuid.UUID `gorm:"type:uuid;not null;index" json:"uploaded_by"`
	UploadedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"uploaded_at"`

	// Optional expiry
	ExpiresAt *time.Time `gorm:"index" json:"expires_at,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// BeforeCreate hook
func (f *FileStorage) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}

// FileUploadRequest represents a file upload request
type FileUploadRequest struct {
	Category    FileCategory `json:"category" binding:"required"`
	EventID     *uuid.UUID   `json:"event_id,omitempty"`
	OrganizerID *uuid.UUID   `json:"organizer_id,omitempty"`
	UserID      *uuid.UUID   `json:"user_id,omitempty"`
	CompanyID   *uuid.UUID   `json:"company_id,omitempty"`
	CategoryID  *uuid.UUID   `json:"category_id,omitempty"`
	AltText     string       `json:"alt_text"`
	Description string       `json:"description"`
	Tags        []string     `json:"tags"`
	IsPublic    *bool        `json:"is_public"`
}

// FileCategoryStats represents statistics for file categories
type FileCategoryStats struct {
	Category   FileCategory `json:"category"`
	Count      int64        `json:"count"`
	TotalSize  int64        `json:"total_size"` // in bytes
	LastUpload *time.Time   `json:"last_upload,omitempty"`
}

// S3Config represents S3 configuration
type S3Config struct {
	BucketName      string `json:"bucket_name"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	PublicReadACL   bool   `json:"public_read_acl"`
}

// FileValidationRules represents validation rules for different file types
type FileValidationRules struct {
	Category          FileCategory `json:"category"`
	AllowedMimeTypes  []string     `json:"allowed_mime_types"`
	MaxFileSize       int64        `json:"max_file_size"`       // in bytes
	MinWidth          *int         `json:"min_width,omitempty"` // for images
	MaxWidth          *int         `json:"max_width,omitempty"`
	MinHeight         *int         `json:"min_height,omitempty"`
	MaxHeight         *int         `json:"max_height,omitempty"`
	RequiredDimension bool         `json:"required_dimension"`
}
