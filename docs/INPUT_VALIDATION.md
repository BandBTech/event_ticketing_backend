# Input Validation Guide

This document provides guidelines for implementing secure and robust input validation in the Event Ticketing API.

## Why Input Validation Matters

- **Security**: Prevents injection attacks (SQL, NoSQL, command injection)
- **Data Integrity**: Ensures data quality and consistency
- **User Experience**: Provides immediate feedback about incorrect inputs
- **Resource Protection**: Prevents DoS attacks through malicious inputs

## Validation Framework

We use the `github.com/go-playground/validator/v10` package for input validation. This package is integrated with Gin and provides:

- A wide range of built-in validation tags
- Custom validation functions
- Localized error messages
- Support for cross-field validations

## Validation Strategy

1. **Middleware-based validation**: Use middleware to validate input before reaching handlers
2. **Model-based validation**: Define validation tags directly on struct fields
3. **Error formatting**: Format validation errors into user-friendly messages

## Common Validation Tags

| Tag        | Description                                         | Example                            |
| ---------- | --------------------------------------------------- | ---------------------------------- |
| `required` | Field must be present                               | `binding:"required"`               |
| `min`      | Minimum value (for numbers) or length (for strings) | `binding:"min=3"`                  |
| `max`      | Maximum value or length                             | `binding:"max=100"`                |
| `email`    | Must be a valid email                               | `binding:"email"`                  |
| `oneof`    | Must be one of the specified values                 | `binding:"oneof=admin user guest"` |
| `url`      | Must be a valid URL                                 | `binding:"url"`                    |
| `uuid`     | Must be a valid UUID                                | `binding:"uuid"`                   |
| `eqfield`  | Must be equal to another field                      | `binding:"eqfield=Password"`       |
| `gtfield`  | Greater than another field                          | `binding:"gtfield=StartDate"`      |
| `phone`    | Must be a valid phone number                        | `binding:"phone"`                  |

## Custom Validation Tags

We have implemented several custom validation tags:

| Tag               | Description                                                    |
| ----------------- | -------------------------------------------------------------- |
| `strong_password` | Requires uppercase, lowercase, number, and special char        |
| `currency_amount` | Validates currency amounts with 2 decimal places               |
| `name`            | Validates proper names (letters, spaces, hyphens, apostrophes) |
| `address`         | Validates postal addresses                                     |
| `zip_code`        | Validates zip/postal codes                                     |
| `credit_card`     | Validates credit card numbers                                  |
| `expiry_date`     | Validates MM/YY format                                         |
| `cvv`             | Validates CVV format (3-4 digits)                              |
| `otp`             | Validates OTP format (4-6 digits)                              |

## Implementation Guidelines

### 1. Define Model Validations

```go
type CreateUserRequest struct {
    Email     string `json:"email" binding:"required,email" example:"user@example.com"`
    Password  string `json:"password" binding:"required,strong_password" example:"Password123!"`
    FirstName string `json:"first_name" binding:"required,min=2,max=50" example:"John"`
    LastName  string `json:"last_name" binding:"required,min=2,max=50" example:"Doe"`
    Phone     string `json:"phone" binding:"omitempty,phone" example:"+12345678901"`
}
```

### 2. Apply Validation Middleware in Routes

```go
auth.POST("/register", middleware.ValidateJSON(&models.CreateUserRequest{}), authHandler.Register)
```

### 3. Access Validated Data in Handlers

```go
func (h *AuthHandler) Register(c *gin.Context) {
    // Get the validated data from context
    validated, exists := c.Get("validatedData")
    if !exists {
        utils.BadRequestErrorResponse(c, "Invalid request data", nil)
        return
    }

    req := validated.(*models.CreateUserRequest)

    // Process the validated request...
}
```

## Validation Best Practices

1. **Whitelist, not blacklist**: Validate against what is allowed, not what is disallowed
2. **Validate all inputs**: Query parameters, path variables, form fields, and JSON bodies
3. **Server-side validation**: Never trust client-side validation alone
4. **Appropriate validation**: Choose validation that makes sense for each field
5. **Normalize data**: Convert input to canonical form before validation when appropriate
6. **Use validation middleware**: Don't repeat validation logic in handlers
7. **Friendly error messages**: Return clear, actionable error messages
8. **Secure defaults**: Fail closed, not open

## Common Input Types and Their Validations

### User Input

- **Email**: `binding:"required,email"`
- **Password**: `binding:"required,min=8,strong_password"`
- **Names**: `binding:"required,min=2,max=50,name"`
- **Phone**: `binding:"omitempty,phone"`

### Numeric Values

- **IDs**: `binding:"required,min=1"`
- **Quantities**: `binding:"required,min=1"`
- **Percentages**: `binding:"required,min=0,max=100"`
- **Currency**: `binding:"required,min=0,currency_amount"`

### Date/Time Values

- **Dates**: `binding:"required,datetime=2006-01-02"`
- **DateTime**: `binding:"required,datetime=2006-01-02T15:04:05Z07:00"`
- **Future Dates**: Custom validation to ensure date is in the future

### Special Types

- **UUIDs**: `binding:"required,uuid"`
- **URLs**: `binding:"required,url"`
- **Enums**: `binding:"required,oneof=value1 value2 value3"`

## Example: Complex Validation

```go
type EventCreateRequest struct {
    Title       string    `json:"title" binding:"required,min=3,max=200"`
    Description string    `json:"description" binding:"max=5000"`
    Location    string    `json:"location" binding:"required,min=3,max=200"`
    StartDate   time.Time `json:"start_date" binding:"required"`
    EndDate     time.Time `json:"end_date" binding:"required,gtfield=StartDate"`
    Price       float64   `json:"price" binding:"required,min=0,max=100000"`
    Capacity    int       `json:"capacity" binding:"required,min=1,max=100000"`
    Category    string    `json:"category" binding:"omitempty,oneof=conference workshop seminar party concert sports other"`
    IsPublic    bool      `json:"is_public" binding:"omitempty"`
    ImageURL    string    `json:"image_url" binding:"omitempty,url"`
    Tags        []string  `json:"tags" binding:"omitempty,dive,min=2,max=50"`
}
```

## Security Considerations

- **Sensitive Data**: Don't include sensitive data in validation error messages
- **Error Messages**: Don't expose internal details in error messages
- **Timing Attacks**: Be aware of timing attacks in validations
- **DoS Protection**: Set reasonable limits on input sizes and complexity
- **Secure Defaults**: Always use secure defaults for optional validations

## Resources

- [go-playground/validator Documentation](https://github.com/go-playground/validator)
- [OWASP Input Validation Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Input_Validation_Cheat_Sheet.html)
