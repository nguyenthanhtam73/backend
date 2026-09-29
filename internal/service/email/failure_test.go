package email

import (
	"errors"
	"testing"
)

func TestClassifySendFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want FailureClass
	}{
		{"nil", nil, FailureTransient},
		{"not configured", ErrNotConfigured, FailureTransient},
		{"plain", errors.New("boom"), FailureTransient},
		{"wrapped send", errors.Join(ErrSendFailed, errors.New("boom")), FailureTransient},
		{"transport", &Failure{cause: errors.New("dial tcp")}, FailureTransient},
		{"422", &Failure{Status: 422}, FailurePermanent},
		{"400", &Failure{Status: 400}, FailurePermanent},
		{"409", &Failure{Status: 409}, FailureRejection},
		{"401", &Failure{Status: 401}, FailureTransient},
		{"403", &Failure{Status: 403}, FailureTransient},
		{"408", &Failure{Status: 408}, FailureTransient},
		{"429", &Failure{Status: 429}, FailureTransient},
		{"500", &Failure{Status: 500}, FailureTransient},
		{"503", &Failure{Status: 503}, FailureTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Fatalf("Classify=%v want %v", got, tc.want)
			}
		})
	}
	if !errors.Is(&Failure{Status: 422}, ErrSendFailed) {
		t.Fatal("permanent failure should still be ErrSendFailed")
	}
	if StatusOf(&Failure{Status: 422}) != 422 || StatusOf(errors.New("x")) != 0 {
		t.Fatal("StatusOf")
	}
}

func TestAddressHashHidesMailbox(t *testing.T) {
	const addr = "danghaiduong@1995"
	hash := AddressHash("  " + addr + " ")
	if hash == "" || hash == addr || len(hash) != 64 {
		t.Fatalf("hash=%q", hash)
	}
	if AddressHash(addr) != hash {
		t.Fatal("hash should ignore surrounding space and case via normalization")
	}
	if AddressHash("DangHaiDuong@1995") != hash {
		t.Fatal("hash should be case-insensitive")
	}
}
