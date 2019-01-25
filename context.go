package gracefully

// ContextSignal is a convenience signaler for telling ServiceManager we need to stop. This is really useful for testing
// Do not instantiate yourself, call: NewContextSignal
type ContextSignal struct {
	BaseSignaler
}

// NewContextSignal creates a new ContextSignal, ready to be passed to a ServiceManager
func NewContextSignal() *ContextSignal {
	return &ContextSignal{
		BaseSignaler: NewBaseSignaler(),
	}
}

// Stop triggers the system to stop
func (c *ContextSignal) Stop() {
	c.OnSignal <- func(manager *ServiceManager) GracefulAction {
		return GracefulStop
	}
}

// Restart triggers the system to restart
func (c *ContextSignal) Restart() {
	c.OnSignal <- func(manager *ServiceManager) GracefulAction {
		return GracefulRestart
	}
}
