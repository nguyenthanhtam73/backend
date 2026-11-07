package dto

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/pkg/usermsg"
	"github.com/google/uuid"
)

func TestAnalysisPending(t *testing.T) {
	outage := domain.AIAnalysisOutageSince.Add(time.Minute)
	before := domain.AIAnalysisOutageSince.Add(-time.Hour)
	check := &domain.SkinCheck{ID: uuid.New(), CreatedAt: outage}
	old := &domain.SkinCheck{ID: uuid.New(), CreatedAt: before}

	cases := []struct {
		name  string
		check *domain.SkinCheck
		a     *domain.SkinAnalysis
		down  bool
		want  bool
	}{
		{
			name:  "completed stays empty even when the pipeline is down",
			check: check,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusCompleted},
			down:  true,
			want:  false,
		},
		{
			name:  "deferred message",
			check: old,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusFailed, ErrorMessage: domain.DeferredAnalysisMessage},
			want:  true,
		},
		{
			name:  "pipeline down while the job is still pending",
			check: check,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusPending},
			down:  true,
			want:  true,
		},
		{
			name:  "healthy pending job is a normal poll",
			check: check,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusPending},
			want:  false,
		},
		{
			name:  "generic failure inside the credit window",
			check: check,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusFailed, ErrorMessage: usermsg.Generic},
			want:  true,
		},
		{
			name:  "empty failure inside the credit window",
			check: check,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusFailed},
			want:  true,
		},
		{
			name:  "specific failure inside the window is not a maintenance note",
			check: check,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusFailed, ErrorMessage: "Phân tích hơi lâu nên dừng lại. Bạn gửi lại giúp mình nhé."},
			want:  false,
		},
		{
			name:  "generic failure before the window",
			check: old,
			a:     &domain.SkinAnalysis{Status: domain.AnalysisStatusFailed, ErrorMessage: usermsg.Generic},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnalysisPending(tc.check, tc.a, tc.down); got != tc.want {
				t.Fatalf("AnalysisPending()=%v, want %v", got, tc.want)
			}
		})
	}

	res := NewCreateSkinCheckResponse(check, &domain.SkinAnalysis{
		ID: uuid.New(), SkinCheckID: check.ID, Status: domain.AnalysisStatusFailed, ErrorMessage: usermsg.Generic,
	}, nil)
	MarkAnalysisPending(&res, check, &domain.SkinAnalysis{Status: domain.AnalysisStatusFailed, ErrorMessage: usermsg.Generic}, false)
	if !res.AnalysisPending {
		t.Fatal("MarkAnalysisPending should set analysis_pending")
	}
}
