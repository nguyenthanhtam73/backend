package skincheck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestClassifyDevice(t *testing.T) {
	const (
		androidChrome = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
		iphoneSafari  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
		ipad          = "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
		windowsChrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
		macSafari     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
		linuxFirefox  = "Mozilla/5.0 (X11; Linux x86_64; rv:126.0) Gecko/20100101 Firefox/126.0"
		chromeOS      = "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
		okhttp        = "okhttp/4.12.0"
		dalvik        = "Dalvik/2.1.0 (Linux; U; Android 14; Pixel 8 Build/UP1A.231005.007)"
	)

	cases := []struct {
		name   string
		header string
		ua     string
		want   string
	}{
		{"android header wins over a browser agent", "android", iphoneSafari, DeviceAndroidApp},
		{"padded android header", "  android  ", "", DeviceAndroidApp},
		{"wrong-case header is not the app", "Android", windowsChrome, DeviceWebDesktop},
		{"dadiary-android header is not the app", "dadiary-android", androidChrome, DeviceWebAndroid},
		{"explicit web header still uses the agent", "web", androidChrome, DeviceWebAndroid},
		{"okhttp without the header is the app", "", okhttp, DeviceAndroidApp},
		{"dalvik without the header is the app", "", dalvik, DeviceAndroidApp},
		{"chrome on android is the mobile site", "", androidChrome, DeviceWebAndroid},
		{"firefox on android", "", "Mozilla/5.0 (Android 14; Mobile; rv:126.0) Gecko/126.0 Firefox/126.0", DeviceWebAndroid},
		{"iphone safari", "", iphoneSafari, DeviceWebIOS},
		{"ipad", "", ipad, DeviceWebIOS},
		{"chrome on ios", "", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.6478.54 Mobile/15E148 Safari/604.1", DeviceWebIOS},
		{"windows", "", windowsChrome, DeviceWebDesktop},
		{"mac", "", macSafari, DeviceWebDesktop},
		{"linux", "", linuxFirefox, DeviceWebDesktop},
		{"chrome os", "", chromeOS, DeviceWebDesktop},
		{"empty", "", "", DeviceUnknown},
		{"curl", "", "curl/8.5.0", DeviceUnknown},
		{"header noise is not echoed", "android\nuser@x.com", "", DeviceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyDevice(tc.header, tc.ua)
			if got != tc.want {
				t.Fatalf("ClassifyDevice(%q, %q)=%q want %q", tc.header, tc.ua, got, tc.want)
			}
			if got != DeviceWebAndroid && got != DeviceWebIOS && got != DeviceWebDesktop && got != DeviceAndroidApp && got != DeviceUnknown {
				t.Fatalf("unclassified result %q", got)
			}
		})
	}
}

func TestClassifyDevice_DoesNotEchoUserAgent(t *testing.T) {
	ua := "Mozilla/5.0 (Linux; Android 14; Pixel 8) user@secret.example https://cdn.example/uploads/checks/a.jpg r2.cloudflarestorage.com/bucket/key"
	got := ClassifyDevice("", ua)
	if got != DeviceWebAndroid {
		t.Fatalf("got %q", got)
	}
	for _, leaked := range []string{"Mozilla", "@", "http", "uploads/", "r2.cloudflarestorage", "Pixel", ua} {
		if strings.Contains(got, leaked) {
			t.Fatalf("device type leaked %q in %q", leaked, got)
		}
	}
}

func TestDeviceTypeContextRoundTrip(t *testing.T) {
	ctx := WithDeviceType(context.Background(), DeviceWebDesktop)
	if got := DeviceTypeFromContext(ctx); got != DeviceWebDesktop {
		t.Fatalf("got %q", got)
	}
	if got := DeviceTypeFromContext(context.Background()); got != "" {
		t.Fatalf("unset context got %q", got)
	}
	if got := DeviceTypeFromContext(nil); got != "" {
		t.Fatalf("nil context got %q", got)
	}
}

func TestCreateAndReanalyze_PassDeviceTypeToAnalysis(t *testing.T) {
	enq := &captureDeviceEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1"

	if _, err := svc.Create(WithDeviceType(context.Background(), ClassifyDevice("", ua)), owner, CreateInput{
		UserNote: "má hơi đỏ",
		SkipMode: true,
	}); err != nil {
		t.Fatal(err)
	}
	if enq.device != DeviceWebIOS {
		t.Fatalf("create device=%q", enq.device)
	}

	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)
	desktop := ClassifyDevice("web", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0.0.0 Safari/537.36")
	if _, err := svc.Reanalyze(WithDeviceType(context.Background(), desktop), owner, check.ID, ""); err != nil {
		t.Fatal(err)
	}
	if enq.device != DeviceWebDesktop {
		t.Fatalf("reanalyze device=%q", enq.device)
	}
	if enq.calls != 2 {
		t.Fatalf("enqueue calls=%d", enq.calls)
	}
}

// captureDeviceEnqueuer records the coarse device handed to the real analysis
// service. It implements the optional EnqueueAnalysisWithDevice method.
type captureDeviceEnqueuer struct {
	calls  int
	device string
}

func (e *captureDeviceEnqueuer) EnqueueAnalysis(id uuid.UUID) {
	e.EnqueueAnalysisWithDevice(id, "")
}

func (e *captureDeviceEnqueuer) EnqueueAnalysisWithDevice(id uuid.UUID, device string) {
	if id == uuid.Nil {
		return
	}
	e.calls++
	e.device = device
}
