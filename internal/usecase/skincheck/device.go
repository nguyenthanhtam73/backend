package skincheck

import (
	"context"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

// Coarse client classes for skin-check measurement logs.
// These are the only device values that may be logged. The raw User-Agent is not.
const (
	DeviceWebAndroid = "web-android"
	DeviceWebIOS     = "web-ios"
	DeviceWebDesktop = "web-desktop"
	DeviceAndroidApp = "android-app"
	DeviceUnknown    = "unknown"
)

type deviceTypeContextKey struct{}

// WithDeviceType attaches the coarse device class to ctx for the background
// analysis job. The value must already be a ClassifyDevice result.
func WithDeviceType(ctx context.Context, deviceType string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, deviceTypeContextKey{}, deviceType)
}

// DeviceTypeFromContext returns the coarse device class, or "" when unset.
func DeviceTypeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(deviceTypeContextKey{}).(string)
	return v
}

// ClassifyDevice maps a check-in request to a coarse device class.
//
// The native Android app sends X-DaDiary-Client exactly "android" (the same
// rule as refresh sessions and coach voice). A native HTTP stack (okhttp or
// Dalvik) is also android-app, because browsers do not send those agents.
// Other clients are classified from User-Agent: web-ios, web-android,
// web-desktop, or unknown. iPadOS desktop mode reports a Macintosh User-Agent
// and is counted as web-desktop.
//
// The result is one of the Device* constants. The header and User-Agent are
// not copied into it.
func ClassifyDevice(clientHeader, userAgent string) string {
	if domain.NormalizeRefreshClient(clientHeader) == domain.RefreshClientAndroid {
		return DeviceAndroidApp
	}
	return classifyUserAgent(userAgent)
}

func classifyUserAgent(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return DeviceUnknown
	}
	// Native stacks first. Dalvik agents also contain "Android" and "Linux".
	if strings.Contains(ua, "okhttp/") || strings.Contains(ua, "dalvik/") {
		return DeviceAndroidApp
	}
	if isIOSUserAgent(ua) {
		return DeviceWebIOS
	}
	if strings.Contains(ua, "android") {
		return DeviceWebAndroid
	}
	if isDesktopUserAgent(ua) {
		return DeviceWebDesktop
	}
	return DeviceUnknown
}

func isIOSUserAgent(ua string) bool {
	for _, marker := range []string{"iphone", "ipad", "ipod", "crios/", "fxios/", "edgios/"} {
		if strings.Contains(ua, marker) {
			return true
		}
	}
	return false
}

func isDesktopUserAgent(ua string) bool {
	// Checked after Android and iOS. Android agents contain "Linux";
	// iPhone agents contain "Mac OS X".
	for _, marker := range []string{"windows nt", "macintosh", "mac os x", "cros", "x11", "linux"} {
		if strings.Contains(ua, marker) {
			return true
		}
	}
	return false
}
