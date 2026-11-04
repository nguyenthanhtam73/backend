package repository

import (
	"encoding/json"
	"testing"
)

func TestRoutineAllStepsDone(t *testing.T) {
	done := json.RawMessage(`[{"id":"a","title":"A","completed":true}]`)
	partial := json.RawMessage(`[{"id":"a","title":"A","completed":true},{"id":"b","title":"B","completed":false}]`)
	if !routineAllStepsDone(done, nil) {
		t.Fatal("morning only")
	}
	if routineAllStepsDone(done, partial) {
		t.Fatal("evening still open")
	}
	if routineAllStepsDone(nil, nil) || routineAllStepsDone(json.RawMessage(`[]`), nil) {
		t.Fatal("empty")
	}
	if routineAllStepsDone(json.RawMessage(`["Rửa mặt"]`), nil) {
		t.Fatal("legacy strings are not ticked")
	}
}
