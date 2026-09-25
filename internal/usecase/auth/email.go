package auth

import (
	"net/mail"
	"strings"
)

// validAccountEmail reports whether a trimmed, lowercased address is a mailbox
// with a domain and a letter TLD. net/mail accepts hostnames with no dot
// (for example user@1995); those cannot receive reminder mail. No DNS lookup.
func validAccountEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	dot := strings.LastIndex(domain, ".")
	// TLD is the label after the last dot, at least two ASCII letters (.vn, .com).
	if dot <= 0 || dot >= len(domain)-2 {
		return false
	}
	tld := domain[dot+1:]
	for i := 0; i < len(tld); i++ {
		c := tld[i]
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
