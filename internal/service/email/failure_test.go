package email

import (
	"encoding/json"
	"errors"
	"strings"
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
		{"422 bare", &Failure{Status: 422}, FailureRejection},
		{"400 bare", &Failure{Status: 400}, FailureRejection},
		{"422 recipient", &Failure{Status: 422, Body: resendBody(t, "validation_error", "Invalid `to` field. The email address needs to follow the `email@example.com` or `Name <email@example.com>` format.")}, FailurePermanent},
		{"400 recipient", &Failure{Status: 400, Body: resendBody(t, "validation_error", "Invalid `to` field.")}, FailurePermanent},
		{"422 from", &Failure{Status: 422, Body: resendBody(t, "validation_error", "Invalid `from` field. The email address needs to follow the `email@example.com` or `Name <email@example.com>` format.")}, FailureRejection},
		{"422 invalid_from_address", &Failure{Status: 422, Body: resendBody(t, "invalid_from_address", "Invalid `from` field.")}, FailureRejection},
		{"422 missing field", &Failure{Status: 422, Body: resendBody(t, "missing_required_field", "The request body is missing one or more required fields.")}, FailureRejection},
		{"422 both fields", &Failure{Status: 422, Body: resendBody(t, "validation_error", "Invalid `to` field and invalid `from` field.")}, FailureRejection},
		{"422 name only", &Failure{Status: 422, Body: resendBody(t, "invalid_to_address", "")}, FailurePermanent},
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
	withMailbox := &Failure{
		Status: 422,
		Body:   resendBody(t, "validation_error", "Invalid `to` field: danghaiduong@1995"),
	}
	if strings.Contains(withMailbox.Error(), "danghaiduong@1995") {
		t.Fatalf("error text includes mailbox: %s", withMailbox.Error())
	}
	other := &Failure{Status: 422, Body: resendBody(t, "validation_error", "bad other@example.com")}
	plain := &Failure{Status: 422, Body: resendBody(t, "validation_error", "bad danghaiduong@1995")}
	if FailureKey(withMailbox) == FailureKey(plain) {
		t.Fatal("recipient error should not share a key with an unrelated body")
	}
	if FailureKey(plain) != FailureKey(other) {
		t.Fatal("mailbox text should not split one sender error into many keys")
	}
}

func resendBody(t *testing.T, name, message string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"name": name, "message": message})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
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
