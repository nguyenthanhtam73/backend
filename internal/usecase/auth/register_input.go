package auth

import (
	"unicode"
	"unicode/utf8"
)

// registerInputError rejects register text that must not reach a database
// query. email is already trimmed and lowercased. username and displayName
// are already trimmed. Vietnamese letters in a username or display name are
// allowed; the email must stay ASCII.
func registerInputError(email, username, displayName string) error {
	if accountEmailHasStrangeRunes(email) || textHasUnsafeRunes(username) || textHasUnsafeRunes(displayName) {
		return appInvalidInput(StrangeInputMessage)
	}
	return nil
}

func accountEmailHasStrangeRunes(email string) bool {
	if !utf8.ValidString(email) {
		return true
	}
	for _, r := range email {
		if r == 0 || r > unicode.MaxASCII || unicode.IsControl(r) || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func textHasUnsafeRunes(s string) bool {
	if s == "" {
		return false
	}
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r == 0 || unicode.IsControl(r) {
			return true
		}
	}
	return false
}
