// Package partition solves the ordered heat-treatment batching problem.
//
// A fixed sequence of work orders (each with a positive weight) must be split
// into exactly k non-empty contiguous batches (furnaces). Every batch must
// contain between minItems and maxItems orders. Orders are never reordered.
//
// Among all feasible partitions the solver first minimizes the maximum total
// weight of any batch (the "peak"), and on ties picks the lexicographically
// smallest sequence of cut indices. The algorithm is threshold feasibility
// (binary search over the peak) plus dynamic programming; it never enumerates
// candidate cuts.
package partition

import "sort"

// Batch is one furnace: orders [Start, End) with total weight Weight.
type Batch struct {
	Start  int   `json:"start"`
	End    int   `json:"end"`
	Weight int64 `json:"weight"`
}

// Plan is the optimal partition of the whole order sequence.
type Plan struct {
	Batches []Batch `json:"batches"`
	// Cuts are the k-1 internal boundary indices (each in 1..n-1).
	// Batch boundaries are 0, cuts[0], cuts[1], ..., n.
	Cuts  []int `json:"cuts"`
	Peak  int64 `json:"peak"`
	Items int   `json:"items"`
}

// CountFeasible reports whether n orders can be split into exactly k
// non-empty batches with every batch holding between minItems and maxItems.
// It only checks counts; weights are irrelevant here.
func CountFeasible(n, k, minItems, maxItems int) bool {
	if n <= 0 || k <= 0 || minItems <= 0 || maxItems <= 0 || minItems > maxItems {
		return false
	}
	return minItems*k <= n && n <= maxItems*k
}

// feasible reports whether the sequence can be partitioned into exactly k
// batches, each within the per-batch item bounds and with total weight <= limit.
//
// Prefix DP: reach[j] means the first j orders are coverable by the batches
// placed so far. A batch (i, j) needs minItems <= j-i <= maxItems and
// prefix[j]-prefix[i] <= limit. As j grows, the admissible i range is a
// sliding window over reach, so each DP layer is O(n).
func feasible(prefix []int64, k, minItems, maxItems int, limit int64) bool {
	n := len(prefix) - 1

	reach := make([]bool, n+1)
	next := make([]bool, n+1)
	reach[0] = true

	// [lo, hi) is the current window of candidate i values for end j;
	// cnt counts reach[i] == true inside it.
	var lo, hi, cnt int

	for range k {
		clear(next)
		lo, hi, cnt = 0, 0, 0
		for j := 1; j <= n; j++ {
			// Include every i <= j-minItems into the window.
			for hi <= j-minItems {
				if reach[hi] {
					cnt++
				}
				hi++
			}
			// Drop i <= j-maxItems-1 (batch would hold more than maxItems).
			for lo <= j-maxItems-1 {
				if reach[lo] {
					cnt--
				}
				lo++
			}
			// Weight condition needs prefix[i] >= prefix[j]-limit. Prefix sums
			// are strictly increasing, so too-small prefixes leave from the
			// left and never return for later j.
			need := prefix[j] - limit
			for lo < hi && prefix[lo] < need {
				if reach[lo] {
					cnt--
				}
				lo++
			}
			if cnt > 0 {
				next[j] = true
			}
		}
		reach, next = next, reach
	}
	return reach[n]
}

// suffixTable computes can[t][j]: whether suffix [j, n) can be split into t
// batches within the item bounds and weight limit. Each layer is O(n): the
// admissible end-index interval [j+minItems, j+maxItems] is intersected with
// the weight-valid end indices (prefix[e] <= prefix[j]+limit, a right bound
// that only moves left as j decreases), and true entries of the previous layer
// are counted through a prefix-count array.
func suffixTable(prefix []int64, k, minItems, maxItems int, limit int64) [][]bool {
	n := len(prefix) - 1

	can := make([][]bool, k+1)
	can[0] = make([]bool, n+1)
	can[0][n] = true

	for t := 1; t <= k; t++ {
		prev := can[t-1]
		pc := make([]int, n+2)
		for i := 0; i <= n; i++ {
			pc[i+1] = pc[i]
			if prev[i] {
				pc[i+1]++
			}
		}

		cur := make([]bool, n+1)
		r := n // weight-valid right bound; only moves left while j decreases
		for j := n - 1; j >= 0; j-- {
			cap := prefix[j] + limit
			for r >= 0 && prefix[r] > cap {
				r--
			}
			lo := j + minItems
			hi := j + maxItems
			if hi > n {
				hi = n
			}
			if r < hi {
				hi = r
			}
			if lo <= hi && pc[hi+1]-pc[lo] > 0 {
				cur[j] = true
			}
		}
		can[t] = cur
	}
	return can
}

// lexCuts returns the lexicographically smallest cut sequence for a limit
// known to be feasible. Given the suffix feasibility table, the smallest
// feasible boundary is chosen at each step; that greedy choice yields the
// lexicographically smallest full sequence.
func lexCuts(can [][]bool, prefix []int64, k, minItems, maxItems int, limit int64) []int {
	n := len(prefix) - 1
	cuts := make([]int, 0, k-1)

	start, remaining := 0, k
	for remaining > 1 {
		chosen := -1
		upper := start + maxItems
		if upper > n {
			upper = n
		}
		for e := start + minItems; e <= upper; e++ {
			if prefix[e]-prefix[start] <= limit && can[remaining-1][e] {
				chosen = e
				break
			}
		}
		if chosen < 0 {
			// Unreachable: the caller proved this limit feasible.
			panic("partition: no feasible continuation during reconstruction")
		}
		cuts = append(cuts, chosen)
		start = chosen
		remaining--
	}
	return cuts
}

// Optimal returns the peak-minimal partition of weights into exactly k
// batches honoring minItems/maxItems per batch; ties are broken by the
// lexicographically smallest cut sequence. CountFeasible must hold; otherwise
// behavior is undefined (the API layer rejects such inputs).
func Optimal(weights []int64, k, minItems, maxItems int) Plan {
	n := len(weights)

	prefix := make([]int64, n+1)
	maxW := int64(0)
	for i, w := range weights {
		prefix[i+1] = prefix[i] + w
		if w > maxW {
			maxW = w
		}
	}
	total := prefix[n]

	// Lower bound for the peak: ceil(total/k) and the heaviest single order.
	lo := total / int64(k)
	if total%int64(k) != 0 {
		lo++
	}
	if maxW > lo {
		lo = maxW
	}
	hi := total // always feasible: every batch weighs no more than the total

	// Threshold feasibility: monotone predicate, binary search for the
	// smallest feasible peak instead of enumerating any cuts.
	limit := hi
	if !feasible(prefix, k, minItems, maxItems, lo) {
		limit = lo + int64(sort.Search(int(hi-lo), func(d int) bool {
			return feasible(prefix, k, minItems, maxItems, lo+int64(d))
		}))
	} else {
		limit = lo
	}

	can := suffixTable(prefix, k, minItems, maxItems, limit)
	cuts := lexCuts(can, prefix, k, minItems, maxItems, limit)

	batches := make([]Batch, k)
	prev := 0
	for b := 0; b < k; b++ {
		end := n
		if b < k-1 {
			end = cuts[b]
		}
		batches[b] = Batch{Start: prev, End: end, Weight: prefix[end] - prefix[prev]}
		prev = end
	}

	return Plan{Batches: batches, Cuts: cuts, Peak: limit, Items: n}
}
