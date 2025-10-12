package validators

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

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
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("The %s field is required", e.Field())
	case "email":
		return fmt.Sprintf("The %s field must be a valid email address", e.Field())
	case "phone":
		return fmt.Sprintf("The %s field must be a valid phone number", e.Field())
	case "strong_password":
		return fmt.Sprintf("The %s must be at least 8 characters long and contain uppercase, lowercase, number, and special character", e.Field())
	case "min":
		return fmt.Sprintf("The %s field must be at least %s characters long", e.Field(), e.Param())
	case "max":
		return fmt.Sprintf("The %s field must not exceed %s characters", e.Field(), e.Param())
	case "url":
		return fmt.Sprintf("The %s field must be a valid URL", e.Field())
	case "credit_card":
		return fmt.Sprintf("The %s field must be a valid credit card number", e.Field())
	case "expiry_date":
		return fmt.Sprintf("The %s field must be a valid expiry date in MM/YY format", e.Field())
	case "cvv":
		return fmt.Sprintf("The %s field must be a valid CVV/CVC code (3-4 digits)", e.Field())
	case "otp":
		return fmt.Sprintf("The %s field must be a valid OTP (4-6 digits)", e.Field())
	case "uuid":
		return fmt.Sprintf("The %s field must be a valid UUID", e.Field())
	case "username":
		return fmt.Sprintf("The %s field must be 3-20 characters long and contain only letters, numbers, and underscores", e.Field())
	case "name":
		return fmt.Sprintf("The %s field must be 2-50 characters long and contain only letters, spaces, hyphens, and apostrophes", e.Field())
	case "address":
		return fmt.Sprintf("The %s field must be 5-200 characters long and contain only valid address characters", e.Field())
	case "zip_code":
		return fmt.Sprintf("The %s field must be a valid zip/postal code", e.Field())
	case "currency_amount":
		return fmt.Sprintf("The %s field must be a valid currency amount (e.g., 10.99)", e.Field())
	default:
		return fmt.Sprintf("The %s field is invalid", e.Field())
	}
}
