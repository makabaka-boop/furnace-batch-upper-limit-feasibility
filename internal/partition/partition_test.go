package partition

import (
	"math/rand"
	"slices"
	"testing"
)

// bruteForce enumerates every combination of k-1 cut positions over the small
// input and returns the best plan: minimal peak, ties broken by the
// lexicographically smallest cut sequence. It is the test oracle; production
// code must never do this.
func bruteForce(weights []int64, k, minItems, maxItems int) (cuts []int, peak int64, ok bool) {
	n := len(weights)
	prefix := make([]int64, n+1)
	for i, w := range weights {
		prefix[i+1] = prefix[i] + w
	}

	best := int64(-1)
	var bestCuts []int
	cur := make([]int, 0, k-1)

	var rec func(start, remaining int)
	rec = func(start, remaining int) {
		if remaining == 1 {
			if n-start < minItems || n-start > maxItems {
				return
			}
			// Evaluate this complete partition.
			peak := int64(0)
			prev := 0
			for _, c := range cur {
				if w := prefix[c] - prefix[prev]; w > peak {
					peak = w
				}
				prev = c
			}
			if w := prefix[n] - prefix[prev]; w > peak {
				peak = w
			}
			if best == -1 || peak < best || (peak == best && slices.Compare(cur, bestCuts) < 0) {
				best = peak
				bestCuts = slices.Clone(cur)
			}
			return
		}
		upper := n - (remaining-1)*minItems
		if u := start + maxItems; u < upper {
			upper = u
		}
		for e := start + minItems; e <= upper; e++ {
			cur = append(cur, e)
			rec(e, remaining-1)
			cur = cur[:len(cur)-1]
		}
	}
	rec(0, k)

	if best == -1 {
		return nil, 0, false
	}
	return bestCuts, best, true
}

// assertPlan checks that plan is a valid partition and matches the oracle.
func assertPlan(t *testing.T, weights []int64, k, minItems, maxItems int, plan Plan) {
	t.Helper()
	n := len(weights)

	wantCuts, wantPeak, ok := bruteForce(weights, k, minItems, maxItems)
	if !ok {
		t.Fatalf("oracle found no feasible partition for n=%d k=%d", n, k)
	}
	if plan.Peak != wantPeak {
		t.Errorf("peak = %d, want %d (weights=%v k=%d min=%d max=%d)",
			plan.Peak, wantPeak, weights, k, minItems, maxItems)
	}
	if !slices.Equal(plan.Cuts, wantCuts) {
		t.Errorf("cuts = %v, want %v (weights=%v k=%d min=%d max=%d)",
			plan.Cuts, wantCuts, weights, k, minItems, maxItems)
	}

	// Structural validity of the returned batches.
	if len(plan.Batches) != k {
		t.Fatalf("got %d batches, want %d", len(plan.Batches), k)
	}
	prev := 0
	for i, b := range plan.Batches {
		if b.Start != prev {
			t.Errorf("batch %d starts at %d, want %d (order must be preserved)", i, b.Start, prev)
		}
		if b.End <= b.Start || b.End > n {
			t.Errorf("batch %d has invalid range [%d,%d)", i, b.Start, b.End)
		}
		if cnt := b.End - b.Start; cnt < minItems || cnt > maxItems {
			t.Errorf("batch %d holds %d items, outside [%d,%d]", i, cnt, minItems, maxItems)
		}
		var sum int64
		for _, w := range weights[b.Start:b.End] {
			sum += w
		}
		if b.Weight != sum {
			t.Errorf("batch %d weight = %d, want %d", i, b.Weight, sum)
		}
		if b.Weight > plan.Peak {
			t.Errorf("batch %d weight %d exceeds reported peak %d", i, b.Weight, plan.Peak)
		}
		prev = b.End
	}
	if prev != n {
		t.Errorf("batches end at %d, want %d (all orders must be covered)", prev, n)
	}
}

func TestSingleBatch(t *testing.T) {
	weights := []int64{4, 1, 7, 3}
	plan := Optimal(weights, 1, 1, 10)
	if len(plan.Batches) != 1 || plan.Batches[0].Start != 0 || plan.Batches[0].End != 4 {
		t.Fatalf("unexpected batches: %+v", plan.Batches)
	}
	if plan.Peak != 15 || plan.Batches[0].Weight != 15 {
		t.Errorf("peak = %d, want 15", plan.Peak)
	}
	if len(plan.Cuts) != 0 {
		t.Errorf("cuts = %v, want empty", plan.Cuts)
	}
}

func TestFixedItemsPerBatch(t *testing.T) {
	// Every batch must hold exactly 2 orders: the partition is forced.
	weights := []int64{9, 1, 8, 2, 7, 3}
	plan := Optimal(weights, 3, 2, 2)
	wantCuts := []int{2, 4}
	if !slices.Equal(plan.Cuts, wantCuts) {
		t.Errorf("cuts = %v, want %v", plan.Cuts, wantCuts)
	}
	if plan.Peak != 10 {
		t.Errorf("peak = %d, want 10", plan.Peak)
	}
	assertPlan(t, weights, 3, 2, 2, plan)
}

func TestManyTiedPartitions(t *testing.T) {
	// Ten unit weights into three batches of 2..4 items: many partitions share
	// the optimal peak 4; the lexicographically smallest cuts must win.
	weights := make([]int64, 10)
	for i := range weights {
		weights[i] = 1
	}
	plan := Optimal(weights, 3, 2, 4)
	wantCuts := []int{2, 6}
	if plan.Peak != 4 {
		t.Errorf("peak = %d, want 4", plan.Peak)
	}
	if !slices.Equal(plan.Cuts, wantCuts) {
		t.Errorf("cuts = %v, want %v", plan.Cuts, wantCuts)
	}
	assertPlan(t, weights, 3, 2, 4, plan)
}

func TestTiedPeakPrefersEarlierCuts(t *testing.T) {
	// Five unit orders into two batches of 2..3 items: cut at 2 (2|3) and cut
	// at 3 (3|2) both have peak 3; the lexicographically smaller cut must win.
	weights := []int64{1, 1, 1, 1, 1}
	plan := Optimal(weights, 2, 2, 3)
	if plan.Peak != 3 {
		t.Errorf("peak = %d, want 3", plan.Peak)
	}
	if want := []int{2}; !slices.Equal(plan.Cuts, want) {
		t.Errorf("cuts = %v, want %v", plan.Cuts, want)
	}
	assertPlan(t, weights, 2, 2, 3, plan)
}

func TestCountFeasible(t *testing.T) {
	cases := []struct {
		n, k, min, max int
		want           bool
	}{
		{10, 3, 2, 4, true},
		{5, 3, 2, 4, false},  // too few orders
		{13, 3, 2, 4, false}, // too many orders
		{3, 3, 1, 1, true},
		{1, 1, 1, 600, true},
		{600, 30, 1, 20, true},
		{601, 30, 1, 20, false},
	}
	for _, c := range cases {
		if got := CountFeasible(c.n, c.k, c.min, c.max); got != c.want {
			t.Errorf("CountFeasible(%d,%d,%d,%d) = %v, want %v", c.n, c.k, c.min, c.max, got, c.want)
		}
	}
}

// TestAgainstBruteForce cross-checks the solver against exhaustive cut
// enumeration on many small random instances, including single-batch cases,
// fixed per-batch sizes and inputs with heavy ties.
func TestAgainstBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(20260923))

	for trial := 0; trial < 4000; trial++ {
		n := 1 + rng.Intn(12)
		k := 1 + rng.Intn(min(5, n))
		minItems := 1 + rng.Intn(3)
		maxItems := minItems + rng.Intn(4)
		if !CountFeasible(n, k, minItems, maxItems) {
			continue
		}

		weights := make([]int64, n)
		switch rng.Intn(4) {
		case 0: // heavy ties: tiny weight alphabet
			for i := range weights {
				weights[i] = int64(1 + rng.Intn(2))
			}
		case 1: // all equal: maximal number of tied partitions
			for i := range weights {
				weights[i] = int64(1 + rng.Intn(3))
			}
			w := weights[0]
			for i := range weights {
				weights[i] = w
			}
		default: // general case
			for i := range weights {
				weights[i] = int64(1 + rng.Intn(30))
			}
		}

		plan := Optimal(weights, k, minItems, maxItems)
		assertPlan(t, weights, k, minItems, maxItems, plan)
	}
}

// TestLargeInputsAreFastAndDeterministic runs upper-bound-sized inputs and
// verifies repeated calls return identical plans (no drift between ties).
func TestLargeInputsAreFastAndDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	weights := make([]int64, 600)
	for i := range weights {
		weights[i] = int64(1 + rng.Intn(1_000_000))
	}

	first := Optimal(weights, 30, 1, 20)
	for i := 0; i < 5; i++ {
		again := Optimal(weights, 30, 1, 20)
		if !slices.Equal(first.Cuts, again.Cuts) || first.Peak != again.Peak {
			t.Fatalf("non-deterministic plan: %+v vs %+v", first, again)
		}
	}

	// All-equal worst case for ties at maximum size.
	flat := make([]int64, 600)
	for i := range flat {
		flat[i] = 1
	}
	plan := Optimal(flat, 30, 20, 20)
	if plan.Peak != 20 || len(plan.Batches) != 30 {
		t.Errorf("flat partition: peak=%d batches=%d", plan.Peak, len(plan.Batches))
	}
}

func BenchmarkOptimalMaxSize(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	weights := make([]int64, 600)
	for i := range weights {
		weights[i] = int64(1 + rng.Intn(1_000_000))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Optimal(weights, 30, 1, 20)
	}
}
