package gracefully

// ContextSignal is a convenience signaler for telling ServiceManager we need to stop. This is really useful for testing
// Do not instantiate yourself, call: NewContextSignal
type ContextSignal struct {
	BaseSignaler
}

// NewContextSignal creates a new ContextSignal, ready to be passed to a ServiceManager
func NewContextSignal() *ContextSignal {
	s := &ContextSignal{
		BaseSignaler: NewBaseSignaler(),
	}

	return s
}

// Stop triggers the system to stop
func (c *ContextSignal) Stop() {
	c.OnSignal <- func(manager *ServiceManager) GracefulAction {
		return GracefulStop
	}
}
