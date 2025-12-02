package services

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FileUploadOptions struct {
	EventID     *uuid.UUID
	OrganizerID *uuid.UUID
	UserID      *uuid.UUID
	CompanyID   *uuid.UUID
	CategoryID  *uuid.UUID
	AltText     string
	Description string
	Tags        []string
}

type FileStorageService struct {
	db         *gorm.DB
	s3Config   *models.S3Config
	s3Client   *s3.Client
	uploader   *manager.Uploader
	downloader *manager.Downloader
}

// NewFileStorageService creates a new file storage service
func NewFileStorageService(db *gorm.DB, s3Config *models.S3Config) (*FileStorageService, error) {
	// Create AWS config
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(s3Config.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			s3Config.AccessKeyID,
			s3Config.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS config: %w", err)
	}

	// Create S3 client
	s3Client := s3.NewFromConfig(cfg)
	uploader := manager.NewUploader(s3Client)
	downloader := manager.NewDownloader(s3Client)

	return &FileStorageService{
		db:         db,
		s3Config:   s3Config,
		s3Client:   s3Client,
		uploader:   uploader,
		downloader: downloader,
	}, nil
}

// UploadFile uploads a file to S3 and saves metadata to database
func (s *FileStorageService) UploadFile(file multipart.File, header *multipart.FileHeader, category models.FileCategory, uploadedBy uuid.UUID, options *FileUploadOptions) (string, error) {
	// Validate S3 configuration
	fmt.Printf("[DEBUG] S3 Config - Bucket: %s, Region: %s, AccessKeyID: %s, SecretKey: [%d chars]\n",
		s.s3Config.BucketName, s.s3Config.Region, s.s3Config.AccessKeyID, len(s.s3Config.SecretAccessKey))

	if s.s3Config.BucketName == "" {
		fmt.Printf("[ERROR] S3 bucket name not configured\n")
		return "", fmt.Errorf("S3 bucket name not configured")
	}
	if s.s3Config.AccessKeyID == "" {
		fmt.Printf("[ERROR] S3 access key ID not configured\n")
		return "", fmt.Errorf("S3 access key ID not configured")
	}
	if s.s3Config.SecretAccessKey == "" {
		fmt.Printf("[ERROR] S3 secret access key not configured\n")
		return "", fmt.Errorf("S3 secret access key not configured")
	}

	// Validate file
	fmt.Printf("[DEBUG] Validating file: %s, size: %d, category: %v\n", header.Filename, header.Size, category)
	if err := s.validateFile(file, header, category); err != nil {
		fmt.Printf("[ERROR] File validation failed: %v\n", err)
		return "", err
	}
	fmt.Printf("[DEBUG] File validation passed\n")

	// Reset file pointer to beginning
	if seeker, ok := file.(io.Seeker); ok {
		seeker.Seek(0, 0)
	}

	// Generate unique filename
	fileName := s.generateFileName(header.Filename, category)

	// Upload to S3
	fmt.Printf("[DEBUG] Uploading to S3: fileName=%s, contentType=%s\n", fileName, header.Header.Get("Content-Type"))
	filePath, err := s.uploadToS3(file, fileName, header.Header.Get("Content-Type"))
	if err != nil {
		fmt.Printf("[ERROR] S3 upload failed: %v\n", err)
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}
	fmt.Printf("[DEBUG] S3 upload successful: %s\n", filePath)

	// Get image dimensions if it's an image
	var width, height *int
	if s.isImageFile(header.Filename) {
		if seeker, ok := file.(io.Seeker); ok {
			seeker.Seek(0, 0)
		}
		if w, h, err := s.getImageDimensions(file); err == nil {
			width = &w
			height = &h
		}
		// Reset file pointer again for image processing
		if seeker, ok := file.(io.Seeker); ok {
			seeker.Seek(0, 0)
		}
	}

	// Generate public URL
	publicURL := s.generatePublicURL(filePath)

	// Create file storage record
	fileStorage := &models.FileStorage{
		FileName:     fileName,
		OriginalName: header.Filename,
		FileSize:     header.Size,
		MimeType:     header.Header.Get("Content-Type"),
		FilePath:     filePath,
		PublicURL:    publicURL,
		BucketName:   s.s3Config.BucketName,
		Region:       s.s3Config.Region,
		Category:     category,
		EventID:      options.EventID,
		OrganizerID:  options.OrganizerID,
		UserID:       options.UserID,
		CompanyID:    options.CompanyID,
		CategoryID:   options.CategoryID,
		Width:        width,
		Height:       height,
		AltText:      options.AltText,
		Description:  options.Description,
		Tags:         options.Tags,
		IsPublic:     true, // All uploaded files are public by default
		IsActive:     true,
		UploadedBy:   uploadedBy,
		UploadedAt:   time.Now(),
	}

	// Save to database
	fmt.Printf("[DEBUG] Saving file metadata to database: %s\n", fileName)
	if err := s.db.Create(fileStorage).Error; err != nil {
		fmt.Printf("[ERROR] Database save failed: %v\n", err)
		// Try to delete from S3 if database save fails
		s.deleteFromS3(filePath)
		return "", fmt.Errorf("failed to save file metadata: %w", err)
	}
	fmt.Printf("[DEBUG] File metadata saved successfully, returning URL: %s\n", publicURL)

	return publicURL, nil
}

// DeleteFileByURL deletes a file from S3 and database by URL (hard delete)
func (s *FileStorageService) DeleteFileByURL(fileURL string) error {
	if fileURL == "" {
		return nil // No file to delete
	}

	// Find file record by URL
	var fileStorage models.FileStorage
	if err := s.db.Where("public_url = ?", fileURL).First(&fileStorage).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// File not found in database, but URL provided - might be external URL
			return nil
		}
		return fmt.Errorf("failed to find file: %w", err)
	}

	// Delete from S3
	if err := s.deleteFromS3(fileStorage.FilePath); err != nil {
		// Log error but continue with database deletion
		fmt.Printf("Warning: failed to delete from S3: %v\n", err)
	}

	// Hard delete from database
	if err := s.db.Unscoped().Delete(&fileStorage).Error; err != nil {
		return fmt.Errorf("failed to delete file record: %w", err)
	}

	return nil
}

// DeleteFilesByEntity deletes all files associated with an entity (hard delete)
func (s *FileStorageService) DeleteFilesByEntity(entityType string, entityID uuid.UUID) error {
	var files []models.FileStorage
	query := s.db.Where("is_active = ?", true)

	switch entityType {
	case "event":
		query = query.Where("event_id = ?", entityID)
	case "organizer":
		query = query.Where("organizer_id = ?", entityID)
	case "user":
		query = query.Where("user_id = ?", entityID)
	case "company":
		query = query.Where("company_id = ?", entityID)
	case "category":
		query = query.Where("category_id = ?", entityID)
	default:
		return fmt.Errorf("invalid entity type: %s", entityType)
	}

	if err := query.Find(&files).Error; err != nil {
		return fmt.Errorf("failed to find files for %s: %w", entityType, err)
	}

	// Delete each file from S3 and database
	for _, file := range files {
		if err := s.deleteFromS3(file.FilePath); err != nil {
			fmt.Printf("Warning: failed to delete from S3: %v\n", err)
		}
		if err := s.db.Unscoped().Delete(&file).Error; err != nil {
			fmt.Printf("Warning: failed to delete file record: %v\n", err)
		}
	}

	return nil
}

// Private methods

func (s *FileStorageService) validateFile(file multipart.File, header *multipart.FileHeader, category models.FileCategory) error {
	rules := s.getValidationRules(category)

	// Check file size
	if header.Size > rules.MaxFileSize {
		return fmt.Errorf("file size exceeds maximum allowed size of %d bytes", rules.MaxFileSize)
	}

	// Check MIME type
	contentType := header.Header.Get("Content-Type")
	allowed := false
	for _, allowedType := range rules.AllowedMimeTypes {
		if contentType == allowedType {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("file type %s not allowed for category %s", contentType, category)
	}

	// Check image dimensions if required
	if rules.RequiredDimension && s.isImageFile(header.Filename) {
		width, height, err := s.getImageDimensions(file)
		if err != nil {
			return fmt.Errorf("failed to get image dimensions: %w", err)
		}

		if rules.MinWidth != nil && width < *rules.MinWidth {
			return fmt.Errorf("image width must be at least %d pixels", *rules.MinWidth)
		}
		if rules.MaxWidth != nil && width > *rules.MaxWidth {
			return fmt.Errorf("image width must not exceed %d pixels", *rules.MaxWidth)
		}
		if rules.MinHeight != nil && height < *rules.MinHeight {
			return fmt.Errorf("image height must be at least %d pixels", *rules.MinHeight)
		}
		if rules.MaxHeight != nil && height > *rules.MaxHeight {
			return fmt.Errorf("image height must not exceed %d pixels", *rules.MaxHeight)
		}
	}

	return nil
}

func (s *FileStorageService) getValidationRules(category models.FileCategory) models.FileValidationRules {
	// Default rules
	rules := models.FileValidationRules{
		Category:          category,
		MaxFileSize:       5 * 1024 * 1024, // 5MB default
		RequiredDimension: false,
	}

	switch category {
	case models.FileCategoryEventBanner:
		minWidth, maxWidth, minHeight, maxHeight := 800, 2000, 400, 1200
		rules.AllowedMimeTypes = []string{"image/jpeg", "image/png", "image/webp"}
		rules.MaxFileSize = 10 * 1024 * 1024 // 10MB
		rules.MinWidth = &minWidth
		rules.MaxWidth = &maxWidth
		rules.MinHeight = &minHeight
		rules.MaxHeight = &maxHeight
		rules.RequiredDimension = true

	case models.FileCategoryEventThumbnail:
		minWidth, maxWidth, minHeight, maxHeight := 300, 800, 200, 600
		rules.AllowedMimeTypes = []string{"image/jpeg", "image/png", "image/webp"}
		rules.MaxFileSize = 5 * 1024 * 1024 // 5MB
		rules.MinWidth = &minWidth
		rules.MaxWidth = &maxWidth
		rules.MinHeight = &minHeight
		rules.MaxHeight = &maxHeight
		rules.RequiredDimension = true

	case models.FileCategoryOrganizerLogo, models.FileCategoryCompanyLogo:
		minWidth, maxWidth, minHeight, maxHeight := 100, 500, 100, 500
		rules.AllowedMimeTypes = []string{"image/jpeg", "image/png", "image/webp"}
		rules.MaxFileSize = 2 * 1024 * 1024 // 2MB
		rules.MinWidth = &minWidth
		rules.MaxWidth = &maxWidth
		rules.MinHeight = &minHeight
		rules.MaxHeight = &maxHeight
		rules.RequiredDimension = true

	case models.FileCategoryUserAvatar:
		minWidth, maxWidth, minHeight, maxHeight := 100, 500, 100, 500
		rules.AllowedMimeTypes = []string{"image/jpeg", "image/png", "image/webp"}
		rules.MaxFileSize = 2 * 1024 * 1024 // 2MB
		rules.MinWidth = &minWidth
		rules.MaxWidth = &maxWidth
		rules.MinHeight = &minHeight
		rules.MaxHeight = &maxHeight
		rules.RequiredDimension = true

	case models.FileCategoryDocument:
		rules.AllowedMimeTypes = []string{
			"application/pdf",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"text/plain",
		}
		rules.MaxFileSize = 10 * 1024 * 1024 // 10MB

	default:
		rules.AllowedMimeTypes = []string{"image/jpeg", "image/png", "image/webp", "application/pdf"}
	}

	return rules
}

func (s *FileStorageService) generateFileName(originalName string, category models.FileCategory) string {
	ext := filepath.Ext(originalName)
	name := strings.TrimSuffix(originalName, ext)

	// Create organized folder structure based on category
	folder := s.getFolderName(category)

	// Generate unique filename
	uniqueID := uuid.New().String()
	timestamp := time.Now().Format("20060102_150405")

	return fmt.Sprintf("%s/%s_%s_%s%s", folder, timestamp, uniqueID, name, ext)
}

// getFolderName maps FileCategory to organized folder names
func (s *FileStorageService) getFolderName(category models.FileCategory) string {
	switch category {
	case models.FileCategoryEventBanner, models.FileCategoryEventThumbnail:
		return "events"
	case models.FileCategoryOrganizerLogo, models.FileCategoryOrganizerBanner:
		return "organizers"
	case models.FileCategoryCompanyLogo:
		return "company"
	case models.FileCategoryUserAvatar:
		return "users"
	case models.FileCategoryCategoryIcon:
		return "categories"
	case models.FileCategoryDocument:
		return "documents"
	default:
		return "other"
	}
}

func (s *FileStorageService) uploadToS3(file io.Reader, fileName, contentType string) (string, error) {
	fmt.Printf("[DEBUG] uploadToS3 - bucket: %s, key: %s, contentType: %s\n", s.s3Config.BucketName, fileName, contentType)

	// Prepare upload input
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.s3Config.BucketName),
		Key:         aws.String(fileName),
		Body:        file,
		ContentType: aws.String(contentType),
		// ACL removed - bucket should use bucket policies for public access instead
	}

	// Upload file
	fmt.Printf("[DEBUG] Starting S3 upload...\n")
	_, err := s.uploader.Upload(context.Background(), input)
	if err != nil {
		fmt.Printf("[ERROR] S3 upload error: %v\n", err)
		return "", err
	}

	fmt.Printf("[DEBUG] S3 upload completed successfully\n")
	return fileName, nil
}

func (s *FileStorageService) deleteFromS3(filePath string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.s3Config.BucketName),
		Key:    aws.String(filePath),
	}

	_, err := s.s3Client.DeleteObject(context.Background(), input)
	return err
}

func (s *FileStorageService) generatePublicURL(filePath string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.s3Config.BucketName, s.s3Config.Region, filePath)
}

func (s *FileStorageService) isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp"
}

func (s *FileStorageService) getImageDimensions(file multipart.File) (int, int, error) {
	// Read the file into memory to get dimensions
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return 0, 0, err
	}

	// Decode image
	reader := bytes.NewReader(fileBytes)
	config, _, err := image.DecodeConfig(reader)
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}
