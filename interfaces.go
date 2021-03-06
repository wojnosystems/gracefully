package gracefully

// GracefulAction is how we tell the service what to do after some sort of interrupt is handled
type GracefulAction uint8

const (
	// GracefulRestart : signal to the iteration/ServiceManager that it's time to clean up and then restart. ServiceManager's Wait will continue to remain blocked, but the function passed to Start or Run will re-enter from the top
	GracefulRestart GracefulAction = iota
	// GracefulStop : signal to the iteration/ServiceManager that it's time to stop. Wait/Run will eventually unblock after the internal GoRoutine has ended
	GracefulStop
)

// SignalControl is called back by the thread that called "Wait" or "Run" and executed. This callback is provided the pointer to the service for reference
type SignalControl func(*ServiceManager) GracefulAction

type SignalSelecter interface {
	// Select will be called for this signaler when the ServiceManager is in the Wait state.
	// This channel will never be written to by the receiver, so a read-only channel is returned, but you'll need to
	// keep that channel and push something onto it when you want to trigger the service manager to do something
	// When it's time to wake up and take action (because a signal was received), you add a reference to a SignalControl,
	// SignalControl is a function that takes a reference to the ServiceManager, and returns the GracefulAction the ServiceManager should take
	//
	Select() <-chan SignalControl

	// Cancel stops the Sigaler from listening to signals. This prevents any internal goroutines used from leaking.
	// This call should not block and return as quickly as possible. When called, this should signal to your
	// SignalSelecter that the goroutine(s) you started, if any, need to end.
	// There is no signal to the parent that the goroutine completed and there is no expectation that any information needs to flow to the ServiceManager
	// This is here to prevent leaking any GoRoutines you may have started when you created your SignalSelecter
	Cancel()
}

type BaseSignaler struct {
	// OnSignal: push a function callback to this when you need to signal to ServiceManager to shutdown
	OnSignal chan SignalControl
	// You will receive on this channel when the ServiceManager wants you to shutdown
	OnCancel chan bool
	SignalSelecter
}

func NewBaseSignaler() BaseSignaler {
	return BaseSignaler{
		OnCancel: make(chan bool, 1),
		OnSignal: make(chan SignalControl, 1),
	}
}

// Cancel is called when it's time to clean up the service
func (s *BaseSignaler) Cancel() {
	s.OnCancel <- true
}

// Gracefully calls this to wait for a signal to come in from this signaller
func (s *BaseSignaler) Select() <-chan SignalControl {
	return s.OnSignal
}
