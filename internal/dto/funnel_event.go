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

	// Drop ?query and #fragment before the length check so emails and tokens
	// in the URL are not stored and do not inflate the path cap.
	path := stripFunnelPath(strings.TrimSpace(r.Path))
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
	row.UTMSource, row.UTMCampaign, row.UTMContent, row.FBCLID = attributionColumns(props)
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
// Register, landing, and check-in events may carry optional utm_source,
// utm_campaign, utm_content, and fbclid. A value that fails sanitizing is
// omitted. error_type and button stay strict enums and still reject the
// event when they are invalid.
// Emails and other form fields are rejected on every event. The @ check is
// skipped only for attribution keys on register events, which are sanitized
// below. On every other event an attribution value that contains @ is
// omitted and the event is still stored.
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
		// Skip the blanket @ rejection only for attribution keys on register
		// events. Those keys are sanitized below, and a value that contains
		// @ is omitted. Every other event drops an attribution value that
		// contains @ here, so checkin_page_view does not store it.
		if propCarriesEmailOrForm(key, value) && !registerEventSkipsEmailCheck(event, key) {
			if registerAttributionPropKey(key) {
				delete(obj, key)
				continue
			}
			return nil, "props must not include an email"
		}
	}
	rewriteAttributionProps(obj)
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
		return normalizeLandingProps(obj)
	default:
		return ""
	}
}

func isRegisterFunnelEvent(event string) bool {
	switch event {
	case domain.FunnelRegisterFormView, domain.FunnelRegisterSubmitAttempt,
		domain.FunnelRegisterClientError, domain.FunnelRegisterEmailExists:
		return true
	default:
		return false
	}
}

// registerEventSkipsEmailCheck is true only while a register event still
// carries an attribution key. The sanitizer then drops values that contain @.
// Other events do not skip, so an @ in utm_source is removed before insert.
func registerEventSkipsEmailCheck(event, key string) bool {
	return isRegisterFunnelEvent(event) && registerAttributionPropKey(key)
}

func registerAttributionPropKey(key string) bool {
	switch key {
	case "utm_source", "utm_campaign", "utm_content", "fbclid":
		return true
	default:
		return false
	}
}

// normalizeLandingProps requires button and allows the same optional
// attribution keys as register events. Other keys are rejected.
func normalizeLandingProps(obj map[string]json.RawMessage) string {
	if msg := enumPropValue(obj, "button", domain.LandingCTAButtons, "invalid button"); msg != "" {
		return msg
	}
	for key := range obj {
		switch key {
		case "button", "utm_source", "utm_campaign", "utm_content", "fbclid":
		default:
			return "unknown prop"
		}
	}
	return ""
}

// normalizeRegisterEventProps keeps error_type (when required) and optional
// utm_source, utm_campaign, utm_content, and fbclid. Other keys are rejected.
// An attribution value that fails sanitizing is omitted; the event is still
// accepted.
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
		case "utm_source", "utm_campaign", "utm_content", "fbclid":
			cleaned := funnelAttributionValue(key, raw)
			if cleaned == "" {
				delete(obj, key)
				continue
			}
			encoded, err := json.Marshal(cleaned)
			if err != nil {
				delete(obj, key)
				continue
			}
			obj[key] = encoded
		default:
			return "unknown prop"
		}
	}
	return ""
}

// rewriteAttributionProps sanitizes attribution keys on every event.
// A value that fails is omitted. The rest of the object is left as sent.
func rewriteAttributionProps(obj map[string]json.RawMessage) {
	for _, key := range []string{"utm_source", "utm_campaign", "utm_content", "fbclid"} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		cleaned := funnelAttributionValue(key, raw)
		if cleaned == "" {
			delete(obj, key)
			continue
		}
		encoded, err := json.Marshal(cleaned)
		if err != nil {
			delete(obj, key)
			continue
		}
		obj[key] = encoded
	}
}

// funnelAttributionValue returns the sanitized string for an attribution key.
// Non-strings and values that fail validation are dropped.
func funnelAttributionValue(key string, raw json.RawMessage) string {
	val := bytes.TrimSpace(raw)
	if len(val) == 0 || val[0] != '"' {
		return ""
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return ""
	}
	var kept *string
	if key == "fbclid" {
		kept = acceptedFBCLID(s)
	} else {
		kept = acceptedUTM(s)
	}
	if kept == nil {
		return ""
	}
	return *kept
}

func attributionColumns(props json.RawMessage) (source, campaign, content, fbclid *string) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(props, &obj); err != nil {
		return nil, nil, nil, nil
	}
	return storedStringProp(obj, "utm_source"),
		storedStringProp(obj, "utm_campaign"),
		storedStringProp(obj, "utm_content"),
		storedStringProp(obj, "fbclid")
}

func storedStringProp(obj map[string]json.RawMessage, key string) *string {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(bytes.TrimSpace(raw), &s); err != nil || s == "" {
		return nil
	}
	return &s
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

// stripFunnelPath keeps the path only. The first ? or # starts the query or fragment.
func stripFunnelPath(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		return path[:i]
	}
	return path
}

func printableToken(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
