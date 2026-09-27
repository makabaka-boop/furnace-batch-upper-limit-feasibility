package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func doPost(t *testing.T, h http.Handler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/partition", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("response is not JSON (%d): %s", rec.Code, rec.Body.String())
		}
	}
	return rec, out
}

func TestPartitionSuccess(t *testing.T) {
	h := NewMux()
	rec, out := doPost(t, h, `{"weights":[5,1,4,2,3],"batchCount":3,"minItems":1,"maxItems":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	peak := int64(out["peak"].(float64))
	if peak != 5 {
		t.Errorf("peak = %d, want 5", peak)
	}
	if got := out["cuts"].([]any); len(got) != 2 {
		t.Errorf("cuts = %v, want 2 boundaries", got)
	}
	batches := out["batches"].([]any)
	if len(batches) != 3 {
		t.Fatalf("batches = %v, want 3", batches)
	}
	// Ranges must be contiguous, left-closed/right-open and cover everything.
	prev := float64(0)
	for _, b := range batches {
		m := b.(map[string]any)
		end := m["end"].(float64)
		if m["start"].(float64) != prev {
			t.Errorf("range start %v, want %v (order must be preserved)", m["start"], prev)
		}
		prev = end
	}
	if prev != 5 {
		t.Errorf("ranges end at %v, want 5", prev)
	}
}

func TestImpossiblePartition(t *testing.T) {
	h := NewMux()
	// 5 orders into 3 batches of at least 2 each -> 409.
	rec, out := doPost(t, h, `{"weights":[1,2,3,4,5],"batchCount":3,"minItems":2,"maxItems":4}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if out["error"] != codeImpossiblePartition {
		t.Errorf("error code = %v, want %s", out["error"], codeImpossiblePartition)
	}
}

func TestValidationFailures(t *testing.T) {
	h := NewMux()
	cases := []struct {
		name string
		body string
	}{
		{"unknown field", `{"weights":[1],"batchCount":1,"minItems":1,"maxItems":1,"extra":1}`},
		{"zero weight", `{"weights":[1,0],"batchCount":1,"minItems":1,"maxItems":2}`},
		{"negative weight", `{"weights":[-2],"batchCount":1,"minItems":1,"maxItems":1}`},
		{"weight too large", `{"weights":[1000001],"batchCount":1,"minItems":1,"maxItems":1}`},
		{"empty weights", `{"weights":[],"batchCount":1,"minItems":1,"maxItems":1}`},
		{"batchCount zero", `{"weights":[1],"batchCount":0,"minItems":1,"maxItems":1}`},
		{"batchCount too large", `{"weights":[1,2],"batchCount":31,"minItems":1,"maxItems":2}`},
		{"inconsistent bounds", `{"weights":[1,2],"batchCount":1,"minItems":3,"maxItems":2}`},
		{"missing minItems", `{"weights":[1,2],"batchCount":1,"maxItems":2}`},
		{"malformed json", `{"weights":[1,2],`},
		{"string weight", `{"weights":["1"],"batchCount":1,"minItems":1,"maxItems":1}`},
		{"two json values", `{"weights":[1],"batchCount":1,"minItems":1,"maxItems":1}{}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, out := doPost(t, h, c.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
			}
			if out["error"] != codeValidation {
				t.Errorf("error = %v, want %s", out["error"], codeValidation)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := NewMux()
	req := httptest.NewRequest(http.MethodGet, "/partition", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	h := NewMux()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestStableBoundariesAcrossEquivalentWeights(t *testing.T) {
	h := NewMux()
	// Many tied feasible partitions: the API must return the same lex-smallest
	// plan on every request.
	body := `{"weights":[1,1,1,1,1,1,1,1,1,1],"batchCount":3,"minItems":2,"maxItems":4}`
	var first []int
	for i := 0; i < 3; i++ {
		rec, out := doPost(t, h, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		raw := out["cuts"].([]any)
		cuts := make([]int, len(raw))
		for j, v := range raw {
			cuts[j] = int(v.(float64))
		}
		if first == nil {
			first = cuts
		} else if !slices.Equal(first, cuts) {
			t.Errorf("cuts drifted: %v vs %v", first, cuts)
		}
	}
	if want := []int{2, 6}; !slices.Equal(first, want) {
		t.Errorf("cuts = %v, want %v", first, want)
	}
}
