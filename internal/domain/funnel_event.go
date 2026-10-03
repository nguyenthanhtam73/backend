package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Check-in funnel events the client may report.
// Keep in sync with the frontend check-in analytics calls.
const (
	FunnelCheckinPageView      = "checkin_page_view"
	FunnelCheckinFormView      = "checkin_form_view"
	FunnelCheckinPhotoStaged   = "checkin_photo_staged"
	FunnelCheckinSkipSelected  = "checkin_skip_selected"
	FunnelCheckinSubmitClicked = "checkin_submit_clicked"
	FunnelCheckinSubmitSuccess = "checkin_submit_success"
	FunnelCheckinSubmitError   = "checkin_submit_error"

	FunnelRegisterFormView      = "register_form_view"
	FunnelRegisterSubmitAttempt = "register_submit_attempt"
	FunnelRegisterClientError   = "register_client_error"
	FunnelLandingCTAClick       = "landing_cta_click"
	FunnelRegisterEmailExists   = "register_email_exists"
)

// RegisterClientErrorTypes is the only allowed error_type for register_client_error.
// Values are tokens. Free text, including an email address, is rejected.
var RegisterClientErrorTypes = []string{
	"password_short",
	"email_invalid",
	"email_empty",
	"network",
}

// LandingCTAButtons is the only allowed button for landing_cta_click.
var LandingCTAButtons = []string{
	"hero_primary",
	"header_register",
	"header_login",
	"bottom_cta",
}

// AllFunnelEvents is the allow-list for POST /api/v1/funnel-events.
var AllFunnelEvents = []string{
	FunnelCheckinPageView,
	FunnelCheckinFormView,
	FunnelCheckinPhotoStaged,
	FunnelCheckinSkipSelected,
	FunnelCheckinSubmitClicked,
	FunnelCheckinSubmitSuccess,
	FunnelCheckinSubmitError,
	FunnelRegisterFormView,
	FunnelRegisterSubmitAttempt,
	FunnelRegisterClientError,
	FunnelLandingCTAClick,
	FunnelRegisterEmailExists,
}

const (
	// MaxFunnelSessionIDRunes caps the client-generated session id.
	MaxFunnelSessionIDRunes = 64
	// MaxFunnelPathRunes caps the page path stored with the event.
	MaxFunnelPathRunes = 200
	// MaxFunnelPropsBytes caps the compact JSON object stored in props.
	MaxFunnelPropsBytes = 2048
	// MaxFunnelBodyBytes rejects oversized request bodies before a deep parse.
	MaxFunnelBodyBytes = 8 * 1024
	// MaxFunnelUserAgentRunes is the trimmed User-Agent column width.
	MaxFunnelUserAgentRunes = 256
)

var funnelEventSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(AllFunnelEvents))
	for _, event := range AllFunnelEvents {
		set[event] = struct{}{}
	}
	return set
}()

// IsFunnelEvent reports whether v is an allowed check-in funnel event name.
func IsFunnelEvent(v string) bool {
	_, ok := funnelEventSet[v]
	return ok
}

// FunnelEvent is one client-reported check-in step.
// IP addresses and emails are intentionally not stored.
type FunnelEvent struct {
	ID        uuid.UUID       `gorm:"column:id;type:uuid;primaryKey" json:"id"`
	UserID    *uuid.UUID      `gorm:"column:user_id;type:uuid;index:idx_funnel_events_user_server,priority:1" json:"user_id,omitempty"`
	SessionID string          `gorm:"column:session_id;size:64;not null" json:"session_id"`
	Event     string          `gorm:"column:event;size:40;not null;index:idx_funnel_events_event_server,priority:1" json:"event"`
	Path      string          `gorm:"column:path;size:200;not null" json:"path"`
	Props     json.RawMessage `gorm:"column:props;type:jsonb;not null" json:"props"`
	// Attribution copied from props when the client sent it. Nil when the key
	// was omitted or the value was dropped. Meta ads use utm_content
	// (video_tu_do) and Meta appends fbclid.
	UTMSource   *string   `gorm:"column:utm_source;size:100" json:"-"`
	UTMCampaign *string   `gorm:"column:utm_campaign;size:100" json:"-"`
	UTMContent  *string   `gorm:"column:utm_content;size:100" json:"-"`
	FBCLID      *string   `gorm:"column:fbclid;size:256" json:"-"`
	ClientTS    time.Time `gorm:"column:client_ts;not null" json:"client_ts"`
	ServerTS    time.Time `gorm:"column:server_ts;not null;index:idx_funnel_events_user_server,priority:2;index:idx_funnel_events_event_server,priority:2" json:"server_ts"`
	UserAgent   string    `gorm:"column:user_agent;size:256;not null" json:"user_agent,omitempty"`
}

// TableName is the funnel_events table.
func (FunnelEvent) TableName() string {
	return "funnel_events"
}

// BeforeCreate assigns an id and server timestamp when the caller did not.
func (e *FunnelEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.ServerTS.IsZero() {
		e.ServerTS = time.Now().UTC()
	}
	if len(e.Props) == 0 {
		e.Props = json.RawMessage(`{}`)
	}
	return nil
}
