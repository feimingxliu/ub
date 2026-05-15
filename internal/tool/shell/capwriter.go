package shell

// capWriter is an io.Writer that records up to cap bytes in an
// internal buffer while always counting the true number of bytes
// that flowed through Write. The caller uses Bytes() for the
// captured prefix and Total() for the real byte count, which lets
// the bash tool emit an accurate truncation footer.
type capWriter struct {
	cap   int
	buf   []byte
	total int
}

func newCapWriter(cap int) *capWriter {
	return &capWriter{cap: cap}
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.total += len(p)
	if remaining := w.cap - len(w.buf); remaining > 0 {
		take := min(remaining, len(p))
		w.buf = append(w.buf, p[:take]...)
	}
	return len(p), nil
}

func (w *capWriter) Bytes() []byte { return w.buf }
func (w *capWriter) Total() int    { return w.total }
