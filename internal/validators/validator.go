package validators

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// Custom validation error messages
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validation errors response
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

// Regular expressions for common validations
var (
	emailRegex          = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	phoneRegex          = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
	passwordRegex       = regexp.MustCompile(`^.{8,}$`) // Min 8 characters
	urlRegex            = regexp.MustCompile(`^(http|https)://[a-zA-Z0-9\-\.]+\.[a-zA-Z]{2,}(?:/[^/]*)*$`)
	creditCardRegex     = regexp.MustCompile(`^(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|3(?:0[0-5]|[68][0-9])[0-9]{11}|6(?:011|5[0-9]{2})[0-9]{12}|(?:2131|1800|35\d{3})\d{11})$`)
	expiryDateRegex     = regexp.MustCompile(`^(0[1-9]|1[0-2])\/([0-9]{2})$`)
	cvvRegex            = regexp.MustCompile(`^[0-9]{3,4}$`)
	otpRegex            = regexp.MustCompile(`^[0-9]{4,6}$`)
	uuidRegex           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	usernameRegex       = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)
	nameRegex           = regexp.MustCompile(`^[a-zA-Z\s\-']{2,50}$`)
	addressRegex        = regexp.MustCompile(`^[a-zA-Z0-9\s\.,#\-']{5,200}$`)
	zipCodeRegex        = regexp.MustCompile(`^[0-9]{5}(?:-[0-9]{4})?$`)
	currencyAmountRegex = regexp.MustCompile(`^\d+(\.\d{1,2})?$`) // For amount validation with 2 decimal places
)

// Initialize sets up custom validators
func Initialize() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		// Register custom validation tags
		_ = v.RegisterValidation("email", validateEmail)
		_ = v.RegisterValidation("phone", validatePhone)
		_ = v.RegisterValidation("strong_password", validateStrongPassword)
		_ = v.RegisterValidation("url", validateURL)
		_ = v.RegisterValidation("credit_card", validateCreditCard)
		_ = v.RegisterValidation("expiry_date", validateExpiryDate)
		_ = v.RegisterValidation("cvv", validateCVV)
		_ = v.RegisterValidation("otp", validateOTP)
		_ = v.RegisterValidation("uuid", validateUUID)
		_ = v.RegisterValidation("username", validateUsername)
		_ = v.RegisterValidation("name", validateName)
		_ = v.RegisterValidation("address", validateAddress)
		_ = v.RegisterValidation("zip_code", validateZipCode)
		_ = v.RegisterValidation("currency_amount", validateCurrencyAmount)

		// Register custom error messages
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}
}

// Custom validators
func validateEmail(fl validator.FieldLevel) bool {
	return emailRegex.MatchString(fl.Field().String())
}

func validatePhone(fl validator.FieldLevel) bool {
	return phoneRegex.MatchString(fl.Field().String())
}

func validateStrongPassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	// Basic password strength requirements
	hasMinLength := len(password) >= 8
	hasUppercase := regexp.MustCompile(`[A-Z]`).MatchString(password)
	hasLowercase := regexp.MustCompile(`[a-z]`).MatchString(password)
	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)
	hasSpecialChar := regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]`).MatchString(password)

	return hasMinLength && hasUppercase && hasLowercase && hasNumber && hasSpecialChar
}

func validateURL(fl validator.FieldLevel) bool {
	return urlRegex.MatchString(fl.Field().String())
}

func validateCreditCard(fl validator.FieldLevel) bool {
	return creditCardRegex.MatchString(fl.Field().String())
}

func validateExpiryDate(fl validator.FieldLevel) bool {
	return expiryDateRegex.MatchString(fl.Field().String())
}

func validateCVV(fl validator.FieldLevel) bool {
	return cvvRegex.MatchString(fl.Field().String())
}

func validateOTP(fl validator.FieldLevel) bool {
	return otpRegex.MatchString(fl.Field().String())
}

func validateUUID(fl validator.FieldLevel) bool {
	return uuidRegex.MatchString(fl.Field().String())
}

func validateUsername(fl validator.FieldLevel) bool {
	return usernameRegex.MatchString(fl.Field().String())
}

func validateName(fl validator.FieldLevel) bool {
	return nameRegex.MatchString(fl.Field().String())
}

func validateAddress(fl validator.FieldLevel) bool {
	return addressRegex.MatchString(fl.Field().String())
}

func validateZipCode(fl validator.FieldLevel) bool {
	return zipCodeRegex.MatchString(fl.Field().String())
}

func validateCurrencyAmount(fl validator.FieldLevel) bool {
	return currencyAmountRegex.MatchString(fl.Field().String())
}

// FormatErrors formats validation errors into a user-friendly format
func FormatErrors(err error) ValidationErrors {
	var validationErrors ValidationErrors

	if validationErrs, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrs {
			validationErrors.Errors = append(validationErrors.Errors, ValidationError{
				Field:   e.Field(),
				Message: getErrorMsg(e),
			})
		}
	} else {
		// If it's not a validation error, still provide some feedback
		validationErrors.Errors = append(validationErrors.Errors, ValidationError{
			Field:   "request",
			Message: err.Error(),
		})
	}

	return validationErrors
}

// getErrorMsg returns a user-friendly error message based on the validation tag
func getErrorMsg(e validator.FieldError) string {
	fieldName := getFieldDisplayName(e.Field())

	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", fieldName)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", fieldName)
	case "phone":
		return fmt.Sprintf("%s must be a valid phone number", fieldName)
	case "strong_password":
		return fmt.Sprintf("%s must be at least 8 characters long and contain uppercase, lowercase, number, and special character", fieldName)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters long", fieldName, e.Param())
	case "max":
		return fmt.Sprintf("%s must not exceed %s characters", fieldName, e.Param())
	case "url":
		return fmt.Sprintf("%s must be a valid URL", fieldName)
	case "credit_card":
		return fmt.Sprintf("%s must be a valid credit card number", fieldName)
	case "expiry_date":
		return fmt.Sprintf("%s must be a valid expiry date in MM/YY format", fieldName)
	case "cvv":
		return fmt.Sprintf("%s must be a valid CVV/CVC code (3-4 digits)", fieldName)
	case "otp":
		return fmt.Sprintf("%s must be a valid OTP (4-6 digits)", fieldName)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", fieldName)
	case "username":
		return fmt.Sprintf("%s must be 3-20 characters long and contain only letters, numbers, and underscores", fieldName)
	case "name":
		return fmt.Sprintf("%s must be 2-50 characters long and contain only letters, spaces, hyphens, and apostrophes", fieldName)
	case "address":
		return fmt.Sprintf("%s must be 5-200 characters long and contain only valid address characters", fieldName)
	case "zip_code":
		return fmt.Sprintf("%s must be a valid zip/postal code", fieldName)
	case "currency_amount":
		return fmt.Sprintf("%s must be a valid currency amount (e.g., 10.99)", fieldName)
	case "eqfield":
		return fmt.Sprintf("%s and %s do not match", fieldName, getFieldDisplayName(e.Param()))
	case "nefield":
		return fmt.Sprintf("%s must be different from %s", fieldName, getFieldDisplayName(e.Param()))
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", fieldName, e.Param())
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", fieldName, e.Param())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", fieldName, e.Param())
	case "lt":
		return fmt.Sprintf("%s must be less than %s", fieldName, e.Param())
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters long", fieldName, e.Param())
	case "alphanum":
		return fmt.Sprintf("%s must contain only letters and numbers", fieldName)
	case "alpha":
		return fmt.Sprintf("%s must contain only letters", fieldName)
	case "numeric":
		return fmt.Sprintf("%s must contain only numbers", fieldName)
	case "datetime":
		return fmt.Sprintf("%s must be a valid date and time", fieldName)
	case "oneof":
		return fmt.Sprintf("%s must be one of the following values: %s", fieldName, e.Param())
	default:
		return fmt.Sprintf("%s is invalid", fieldName)
	}
}

// getFieldDisplayName converts technical field names to user-friendly display names
func getFieldDisplayName(fieldName string) string {
	fieldDisplayNames := map[string]string{
		"first_name":        "First name",
		"last_name":         "Last name",
		"email":             "Email address",
		"password":          "Password",
		"new_password":      "New password",
		"confirm_password":  "Confirm password",
		"old_password":      "Current password",
		"phone":             "Phone number",
		"phone_number":      "Phone number",
		"address":           "Address",
		"street_address":    "Street address",
		"city":              "City",
		"state":             "State",
		"zip_code":          "ZIP code",
		"postal_code":       "Postal code",
		"country":           "Country",
		"date_of_birth":     "Date of birth",
		"birth_date":        "Birth date",
		"organization_name": "Organization name",
		"company_name":      "Company name",
		"event_name":        "Event name",
		"event_title":       "Event title",
		"description":       "Description",
		"start_date":        "Start date",
		"end_date":          "End date",
		"start_time":        "Start time",
		"end_time":          "End time",
		"price":             "Price",
		"ticket_price":      "Ticket price",
		"quantity":          "Quantity",
		"capacity":          "Capacity",
		"location":          "Location",
		"venue":             "Venue",
		"category":          "Category",
		"credit_card":       "Credit card number",
		"card_number":       "Card number",
		"expiry_date":       "Expiry date",
		"cvv":               "CVV/CVC",
		"cardholder_name":   "Cardholder name",
		"otp_code":          "OTP code",
		"verification_code": "Verification code",
		"reset_token":       "Reset code",
		"email_token":       "Email",
		"refresh_token":     "Refresh token",
		"access_token":      "Access token",
		"user_id":           "User ID",
		"organization_id":   "Organization ID",
		"event_id":          "Event ID",
		"ticket_id":         "Ticket ID",
		"NewPassword":       "New password",     // For struct field names
		"ConfirmPassword":   "Confirm password", // For struct field names
		"FirstName":         "First name",       // For struct field names
		"LastName":          "Last name",        // For struct field names
		"Email":             "Email address",    // For struct field names
		"Password":          "Password",         // For struct field names
	}

	if displayName, exists := fieldDisplayNames[fieldName]; exists {
		return displayName
	}

	// Convert camelCase to Title Case with spaces
	// e.g., "firstName" -> "First Name"
	return convertCamelCaseToTitleCase(fieldName)
}

// convertCamelCaseToTitleCase converts camelCase field names to readable format
func convertCamelCaseToTitleCase(fieldName string) string {
	// Convert snake_case to Title Case
	if strings.Contains(fieldName, "_") {
		parts := strings.Split(fieldName, "_")
		for i, part := range parts {
			if len(part) > 0 {
				parts[i] = strings.ToUpper(string(part[0])) + strings.ToLower(part[1:])
			}
		}
		return strings.Join(parts, " ")
	}

	// Convert camelCase to Title Case
	var result []rune
	for i, r := range fieldName {
		if i > 0 && unicode.IsUpper(r) {
			result = append(result, ' ')
		}
		if i == 0 {
			result = append(result, unicode.ToUpper(r))
		} else {
			result = append(result, unicode.ToLower(r))
		}
	}
	return string(result)
}
