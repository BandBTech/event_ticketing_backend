package models

// CreateOrgUserRequest is the request structure for creating a user within an organization
type CreateOrgUserRequest struct {
	Email     string `json:"email" binding:"required,email" example:"staff@example.com"`
	Password  string `json:"password" binding:"required" example:"StaffPass123!"`
	FirstName string `json:"first_name" binding:"required,min=2,max=50" example:"Jane"`
	LastName  string `json:"last_name" binding:"required,min=2,max=50" example:"Smith"`
	RoleName  string `json:"role_name" binding:"required,oneof=staff manager" example:"staff"` // Only allow staff or manager roles
	Phone     string `json:"phone" binding:"omitempty,phone" example:"+12345678901"`
}

// UpdateUserRoleRequest is the request structure for updating a user's role
type UpdateUserRoleRequest struct {
	UserID   string `json:"user_id" binding:"required,uuid4" example:"123e4567-e89b-12d3-a456-426614174000"`
	RoleName string `json:"role_name" binding:"required,oneof=staff manager" example:"manager"` // Only allow staff or manager roles
}

// UpdateOrgUserRequest is used to update a user's role within an organization
type UpdateOrgUserRequest struct {
	RoleType string `json:"role_type" binding:"required,oneof=staff manager" example:"manager"` // Only allow staff or manager roles
	Active   *bool  `json:"active" example:"true"`
}

// UpdateOrganizationRequest is used to update an organization
type UpdateOrganizationRequest struct {
	Name        string `json:"name" binding:"omitempty,min=3,max=100" example:"Updated Event Company"`
	Description string `json:"description" binding:"omitempty,max=1000" example:"Updated description for the organization"`
	WebsiteURL  string `json:"website_url" binding:"omitempty,url" example:"https://updated-events.com"`
	LogoURL     string `json:"logo_url" binding:"omitempty,url" example:"https://updated-events.com/new-logo.png"`
}
