package airecovery

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestCheckNeedsReplay(t *testing.T) {
	since := domain.AIAnalysisOutageSince
	now := since.Add(2 * time.Hour)
	photo := json.RawMessage(`["checks/a.jpg"]`)
	inWindow := since.Add(time.Minute)

	cases := []struct {
		name string
		c    CheckSnapshot
		want bool
	}{
		{
			name: "failed photo inside the window",
			c: CheckSnapshot{
				CreatedAt: inWindow, ImageURLs: photo, HasAnalysis: true,
				Status: domain.AnalysisStatusFailed, AnalysisUpdatedAt: inWindow,
			},
			want: true,
		},
		{
			name: "missing analysis row",
			c:    CheckSnapshot{CreatedAt: inWindow, ImageURLs: photo},
			want: true,
		},
		{
			name: "completed analysis is skipped",
			c: CheckSnapshot{
				CreatedAt: inWindow, ImageURLs: photo, HasAnalysis: true,
				Status: domain.AnalysisStatusCompleted,
			},
			want: false,
		},
		{
			name: "no photo",
			c: CheckSnapshot{
				CreatedAt: inWindow, ImageURLs: json.RawMessage(`[]`), HasAnalysis: true,
				Status: domain.AnalysisStatusFailed,
			},
			want: false,
		},
		{
			name: "blank photo key",
			c: CheckSnapshot{
				CreatedAt: inWindow, ImageURLs: json.RawMessage(`[""]`), HasAnalysis: true,
				Status: domain.AnalysisStatusFailed,
			},
			want: false,
		},
		{
			name: "before the window",
			c: CheckSnapshot{
				CreatedAt: since.Add(-time.Second), ImageURLs: photo, HasAnalysis: true,
				Status: domain.AnalysisStatusFailed,
			},
			want: false,
		},
		{
			name: "exactly at the window start",
			c: CheckSnapshot{
				CreatedAt: since, ImageURLs: photo, HasAnalysis: true,
				Status: domain.AnalysisStatusFailed,
			},
			want: true,
		},
		{
			name: "fresh processing job is left alone",
			c: CheckSnapshot{
				CreatedAt: inWindow, ImageURLs: photo, HasAnalysis: true,
				Status: domain.AnalysisStatusProcessing, AnalysisUpdatedAt: now.Add(-time.Minute),
			},
			want: false,
		},
		{
			name: "stale pending job is replayed",
			c: CheckSnapshot{
				CreatedAt: inWindow, ImageURLs: photo, HasAnalysis: true,
				Status: domain.AnalysisStatusPending, AnalysisUpdatedAt: now.Add(-InFlightGrace - time.Second),
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.c.ID = uuid.New()
			if got := CheckNeedsReplay(tc.c, since, now); got != tc.want {
				t.Fatalf("CheckNeedsReplay()=%v, want %v", got, tc.want)
			}
		})
	}
}
