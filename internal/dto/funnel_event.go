package dto

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// LogFunnelEventRequest is the POST /api/v1/funnel-events body.
type LogFunnelEventRequest struct {
	Event     string          `json:"event"`
	SessionID string          `json:"session_id"`
	Path      string          `json:"path"`
	Props     json.RawMessage `json:"props"`
	ClientTS  string          `json:"client_ts"`
}

// ValidateAndMap checks the request and maps it to a domain row.
// userID may be uuid.Nil for guests; those rows store a NULL user_id.
func (r LogFunnelEventRequest) ValidateAndMap(userID uuid.UUID) (*domain.FunnelEvent, string) {
	event := strings.TrimSpace(r.Event)
	if event == "" {
		return nil, "event is required"
	}
	if !domain.IsFunnelEvent(event) {
		return nil, "unknown event"
	}

	sessionID := strings.TrimSpace(r.SessionID)
	if sessionID == "" {
		return nil, "session_id is required"
	}
	if utf8.RuneCountInString(sessionID) > domain.MaxFunnelSessionIDRunes {
		return nil, "session_id is too long"
	}
	if !printableToken(sessionID) {
		return nil, "session_id is invalid"
	}

	path := strings.TrimSpace(r.Path)
	if utf8.RuneCountInString(path) > domain.MaxFunnelPathRunes {
		return nil, "path is too long"
	}

	props, msg := normalizeFunnelProps(event, r.Props)
	if msg != "" {
		return nil, msg
	}

	clientTS, ok := parseClientTS(r.ClientTS)
	if !ok {
		return nil, "client_ts must be ISO8601"
	}

	row := &domain.FunnelEvent{
		SessionID: sessionID,
		Event:     event,
		Path:      path,
		Props:     props,
		ClientTS:  clientTS,
	}
	if userID != uuid.Nil {
		id := userID
		row.UserID = &id
	}
	return row, ""
}

func parseClientTS(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

// normalizeFunnelProps accepts a small flat JSON object (scalars only).
// Omitted and JSON null props are stored as {}.
// The four register_* events may also carry optional utm_source and
// utm_campaign. Those values use the same charset, 200-rune cap, and "@"
// drop as register attribution. error_type stays a strict enum.
// Emails and other form fields are rejected on every event.
func normalizeFunnelProps(event string, raw json.RawMessage) (json.RawMessage, string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		trimmed = json.RawMessage(`{}`)
	}
	if len(trimmed) > domain.MaxFunnelPropsBytes {
		return nil, "props is too large"
	}
	if trimmed[0] != '{' {
		return nil, "props must be a flat object"
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, "props must be a flat object"
	}
	for key, value := range obj {
		if key == "" || !flatJSONScalar(value) {
			return nil, "props must be a flat object"
		}
		// utm_source / utm_campaign are sanitized below. An "@" drops that
		// value, matching attribution, instead of failing the whole event.
		if propCarriesEmailOrForm(key, value) && !registerAttributionPropKey(key) {
			return nil, "props must not include an email"
		}
	}
	if msg := validateEventProps(event, obj); msg != "" {
		return nil, msg
	}
	compact, err := json.Marshal(obj)
	if err != nil || len(compact) > domain.MaxFunnelPropsBytes {
		return nil, "props is too large"
	}
	return compact, ""
}

func validateEventProps(event string, obj map[string]json.RawMessage) string {
	switch event {
	case domain.FunnelRegisterClientError:
		return normalizeRegisterEventProps(obj, true)
	case domain.FunnelRegisterFormView, domain.FunnelRegisterSubmitAttempt, domain.FunnelRegisterEmailExists:
		return normalizeRegisterEventProps(obj, false)
	case domain.FunnelLandingCTAClick:
		return requireEnumProp(obj, "button", domain.LandingCTAButtons, "invalid button")
	default:
		return ""
	}
}

func registerAttributionPropKey(key string) bool {
	return key == "utm_source" || key == "utm_campaign"
}

// normalizeRegisterEventProps keeps error_type (when required) and optional
// utm_source / utm_campaign. Other keys are rejected. Sanitized UTM values
// are written back; an empty result (including an "@") omits the key.
func normalizeRegisterEventProps(obj map[string]json.RawMessage, requireErrorType bool) string {
	if requireErrorType {
		if msg := enumPropValue(obj, "error_type", domain.RegisterClientErrorTypes, "invalid error_type"); msg != "" {
			return msg
		}
	}
	for key, raw := range obj {
		switch key {
		case "error_type":
			if !requireErrorType {
				return "unknown prop"
			}
		case "utm_source", "utm_campaign":
			cleaned, ok := sanitizeRegisterAttributionProp(raw)
			if !ok {
				return "invalid " + key
			}
			if cleaned == "" {
				delete(obj, key)
				continue
			}
			encoded, err := json.Marshal(cleaned)
			if err != nil {
				return "invalid " + key
			}
			obj[key] = encoded
		default:
			return "unknown prop"
		}
	}
	return ""
}

func sanitizeRegisterAttributionProp(raw json.RawMessage) (string, bool) {
	val := bytes.TrimSpace(raw)
	if len(val) == 0 || val[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return "", false
	}
	return sanitizeAttribution(s), true
}

func requireEnumProp(obj map[string]json.RawMessage, key string, allowed []string, invalidMsg string) string {
	if len(obj) == 0 {
		return invalidMsg
	}
	for k := range obj {
		if k != key {
			return "unknown prop"
		}
	}
	return enumPropValue(obj, key, allowed, invalidMsg)
}

func enumPropValue(obj map[string]json.RawMessage, key string, allowed []string, invalidMsg string) string {
	raw, ok := obj[key]
	if !ok {
		return invalidMsg
	}
	var value string
	if err := json.Unmarshal(bytes.TrimSpace(raw), &value); err != nil {
		return invalidMsg
	}
	for _, candidate := range allowed {
		if value == candidate {
			return ""
		}
	}
	return invalidMsg
}

func propCarriesEmailOrForm(key string, raw json.RawMessage) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "email", "e-mail", "password", "passwd":
		return true
	}
	val := bytes.TrimSpace(raw)
	if len(val) == 0 || val[0] != '"' {
		return false
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return false
	}
	return strings.Contains(s, "@")
}

func flatJSONScalar(raw json.RawMessage) bool {
	val := bytes.TrimSpace(raw)
	if len(val) == 0 || val[0] == '{' || val[0] == '[' {
		return false
	}
	var probe any
	if err := json.Unmarshal(val, &probe); err != nil {
		return false
	}
	switch probe.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

func printableToken(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
