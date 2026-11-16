package logging

// ring keeps the newest items pushed into it, up to a limit, and counts
// every item it ever received.
type ring[T any] struct {
	items []T    // at most limit of them; the oldest is at index start
	start int    // index of the oldest item once the ring is full
	limit int    // at least 1
	total uint64 // items pushed since the start
}

func newRing[T any](limit int) *ring[T] {
	return &ring[T]{limit: max(limit, 1)}
}

func (r *ring[T]) push(v T) {
	r.total++
	if len(r.items) < r.limit {
		r.items = append(r.items, v)
		return
	}
	r.items[r.start] = v
	r.start = (r.start + 1) % r.limit
}

func (r *ring[T]) len() int { return len(r.items) }

// at returns the i-th kept item, counted from the oldest.
func (r *ring[T]) at(i int) T {
	return r.items[(r.start+i)%len(r.items)]
}

// setLimit changes how many items are kept, dropping the oldest if needed.
func (r *ring[T]) setLimit(limit int) {
	limit = max(limit, 1)
	n := min(len(r.items), limit)
	kept := make([]T, n)
	for i := range n {
		kept[i] = r.at(len(r.items) - n + i)
	}
	r.items, r.start, r.limit = kept, 0, limit
}
