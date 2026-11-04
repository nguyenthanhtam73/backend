package domain

import (
	"encoding/json"
	"testing"
)

func TestRoutineAllStepsDone(t *testing.T) {
	done := json.RawMessage(`[{"id":"a","title":"A","completed":true}]`)
	partial := json.RawMessage(`[{"id":"a","title":"A","completed":true},{"id":"b","title":"B","completed":false}]`)
	if !RoutineAllStepsDone(done, nil) {
		t.Fatal("morning only")
	}
	if RoutineAllStepsDone(done, partial) {
		t.Fatal("evening still open")
	}
	if RoutineAllStepsDone(nil, nil) || RoutineAllStepsDone(json.RawMessage(`[]`), nil) || RoutineAllStepsDone(json.RawMessage(`null`), json.RawMessage(`null`)) {
		t.Fatal("empty routine is not complete")
	}
	idOnly := json.RawMessage(`[{"id":"wash","title":"  ","completed":true}]`)
	if !RoutineAllStepsDone(idOnly, json.RawMessage(`[]`)) {
		t.Fatal("id-only step counts")
	}
	blank := json.RawMessage(`[{"id":"","title":"","completed":true}]`)
	if RoutineAllStepsDone(blank, nil) {
		t.Fatal("blank step is not a routine")
	}
	legacy := json.RawMessage(`["Rửa mặt","Kem"]`)
	if RoutineAllStepsDone(legacy, nil) {
		t.Fatal("legacy strings are not ticked")
	}
	if RoutineAllStepsDone(json.RawMessage(`["", "  "]`), nil) {
		t.Fatal("blank legacy lines are not a routine")
	}
}
