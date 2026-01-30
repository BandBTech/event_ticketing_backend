package utils

import (
	"fmt"
	"strings"
)

// EmailHelpers provides reusable email utility functions

// ValidateEmailList checks if all emails in list are valid
func ValidateEmailList(emails []string) ([]string, []string) {
	valid := []string{}
	invalid := []string{}

	for _, email := range emails {
		if ValidateEmail(email) {
			valid = append(valid, strings.ToLower(strings.TrimSpace(email)))
		} else {
			invalid = append(invalid, email)
		}
	}

	return valid, invalid
}

// SanitizeEmailSubject removes potential injection attempts from email subjects
func SanitizeEmailSubject(subject string) string {
	// Remove newlines that could lead to header injection
	subject = strings.ReplaceAll(subject, "\r", "")
	subject = strings.ReplaceAll(subject, "\n", "")
	subject = strings.TrimSpace(subject)

	// Limit length
	if len(subject) > 200 {
		subject = subject[:200]
	}

	return subject
}

// BuildEmailTemplate creates HTML email template with consistent styling
func BuildEmailTemplate(title, body string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #4F46E5; color: white; padding: 20px; text-align: center; }
        .content { padding: 30px; background: #fff; }
        .footer { text-align: center; padding: 20px; color: #666; font-size: 12px; }
        .button { display: inline-block; padding: 12px 24px; background: #4F46E5; color: white; text-decoration: none; border-radius: 4px; margin: 20px 0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            %s
        </div>
        <div class="footer">
            <p>This is an automated email. Please do not reply.</p>
        </div>
    </div>
</body>
</html>
`, title, body)
}

// ExtractEmailDomain gets domain from email address
func ExtractEmailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

// IsCorporateEmail checks if email is from common free email providers
func IsCorporateEmail(email string) bool {
	domain := ExtractEmailDomain(email)
	freeProviders := map[string]bool{
		"gmail.com":      true,
		"yahoo.com":      true,
		"hotmail.com":    true,
		"outlook.com":    true,
		"icloud.com":     true,
		"proton.me":      true,
		"protonmail.com": true,
	}
	return !freeProviders[domain]
}
