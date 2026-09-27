// Package api exposes the batching solver as a JSON-over-HTTP service.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"heattreat/internal/partition"
)

const (
	// Input contract limits.
	maxWeights  = 600
	maxWeight   = 1_000_000
	maxBatches  = 30
	maxBodySize = 1 << 20 // 1 MiB; far above any valid request

	// Error codes returned in the JSON error body.
	codeValidation          = "VALIDATION_ERROR"
	codeImpossiblePartition = "IMPOSSIBLE_PARTITION"
)

// Request is the JSON body of POST /partition.
type Request struct {
	Weights    []int64 `json:"weights"`
	BatchCount int     `json:"batchCount"`
	MinItems   int     `json:"minItems"`
	MaxItems   int     `json:"maxItems"`
}

// Response is the JSON body of a successful POST /partition.
type Response struct {
	Batches []partition.Batch `json:"batches"`
	Cuts    []int             `json:"cuts"`
	Peak    int64             `json:"peak"`
	Items   int               `json:"items"`
}

// ErrorResponse is the JSON body of a failed request.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// NewMux builds the HTTP routing table for the service.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/partition", handlePartition)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func handlePartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	req, err := decodeRequest(w, r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, codeValidation, err.Error())
		return
	}

	if !partition.CountFeasible(len(req.Weights), req.BatchCount, req.MinItems, req.MaxItems) {
		writeError(w, http.StatusConflict, codeImpossiblePartition,
			fmt.Sprintf("cannot split %d orders into %d batches of %d..%d items each",
				len(req.Weights), req.BatchCount, req.MinItems, req.MaxItems))
		return
	}

	plan := partition.Optimal(req.Weights, req.BatchCount, req.MinItems, req.MaxItems)
	writeJSON(w, http.StatusOK, Response{
		Batches: plan.Batches,
		Cuts:    plan.Cuts,
		Peak:    plan.Peak,
		Items:   plan.Items,
	})
}

// decodeRequest parses and validates the request body. Every failure is a
// 422-class problem: malformed JSON, unknown fields, out-of-range values,
// zero weights, or inconsistent min/max item bounds.
func decodeRequest(w http.ResponseWriter, r *http.Request) (Request, error) {
	var req Request

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fmt.Errorf("invalid JSON body: %v", err)
	}
	if dec.More() {
		return req, errors.New("request body must contain a single JSON object")
	}

	if len(req.Weights) == 0 {
		return req, errors.New("weights must contain at least 1 order")
	}
	if len(req.Weights) > maxWeights {
		return req, fmt.Errorf("weights must contain at most %d orders", maxWeights)
	}
	for i, wgt := range req.Weights {
		if wgt <= 0 {
			return req, fmt.Errorf("weights[%d] must be a positive integer", i)
		}
		if wgt > maxWeight {
			return req, fmt.Errorf("weights[%d] exceeds the maximum of %d", i, maxWeight)
		}
	}
	if req.BatchCount < 1 || req.BatchCount > maxBatches {
		return req, fmt.Errorf("batchCount must be between 1 and %d", maxBatches)
	}
	if req.MinItems < 1 {
		return req, errors.New("minItems must be at least 1")
	}
	if req.MaxItems < 1 {
		return req, errors.New("maxItems must be at least 1")
	}
	if req.MinItems > req.MaxItems {
		return req, fmt.Errorf("minItems (%d) must not exceed maxItems (%d)", req.MinItems, req.MaxItems)
	}
	return req, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: code, Message: message})
}
