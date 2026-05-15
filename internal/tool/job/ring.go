package job

// ring is a fixed-capacity byte ring buffer. Write always succeeds and
// overwrites the oldest bytes when the buffer fills up. Snapshot
// returns a copy of the last min(tail, size) bytes in chronological
// order (oldest first, newest last). Total reports the true number of
// bytes ever written, unaffected by overwrites.
//
// ring is NOT safe for concurrent use. Callers (the job struct) must
// hold a mutex around Write / Snapshot / Total.
type ring struct {
	buf   []byte
	head  int // index of the next byte to write
	size  int // current count, 0..cap(buf)
	total int // bytes ever written
}

func newRing(capacity int) *ring {
	return &ring{buf: make([]byte, capacity)}
}

func (r *ring) Write(p []byte) (int, error) {
	r.total += len(p)
	if len(r.buf) == 0 {
		return len(p), nil
	}
	for _, b := range p {
		r.buf[r.head] = b
		r.head = (r.head + 1) % len(r.buf)
		if r.size < len(r.buf) {
			r.size++
		}
	}
	return len(p), nil
}

// Snapshot returns up to tail bytes in chronological order. tail <= 0
// returns the full ring contents. The returned slice is freshly
// allocated and safe for the caller to retain.
func (r *ring) Snapshot(tail int) []byte {
	if r.size == 0 {
		return nil
	}
	n := r.size
	if tail > 0 && tail < n {
		n = tail
	}
	out := make([]byte, n)
	// The oldest of the kept-n bytes sits at head - n (mod cap).
	start := (r.head - n + len(r.buf)) % len(r.buf)
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%len(r.buf)]
	}
	return out
}

func (r *ring) Total() int { return r.total }
