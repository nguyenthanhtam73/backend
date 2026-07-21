package domain

import (
	"errors"
	"net/http"
	"testing"
)

func TestAppError_UnwrapAndAs(t *testing.T) {
	cause := errors.New("root")
	err := Wrap(cause, http.StatusConflict, "email_taken", "email already registered")

	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is to find cause")
	}
	ae, ok := AsAppError(err)
	if !ok || ae.Code != "email_taken" || ae.HTTPStatus != http.StatusConflict {
		t.Fatalf("AsAppError: %+v ok=%v", ae, ok)
	}
}
