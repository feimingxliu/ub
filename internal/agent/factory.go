package agent

// Factory creates fresh Agent instances from a shared Options template.
// It is deliberately not an object pool: each New call returns a new Agent, and
// conversation state continues to live in Request, runner state, and rollout.
type Factory struct {
	base Options
}

// NewFactory returns a factory using opts as its base template.
func NewFactory(opts Options) *Factory {
	return &Factory{base: opts}
}

// Options returns a copy of the base options. Callers can inspect or derive a
// specialized factory without mutating this factory's template.
func (f *Factory) Options() Options {
	if f == nil {
		return Options{}
	}
	return f.base
}

// New constructs a fresh Agent. configure may override fields on a copy of the
// base options for this one instance.
func (f *Factory) New(configure func(*Options)) (*Agent, error) {
	if f == nil {
		return New(Options{})
	}
	opts := f.base
	if configure != nil {
		configure(&opts)
	}
	return New(opts)
}
