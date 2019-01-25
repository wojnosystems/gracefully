package gracefully

import (
	"os"
	"os/signal"
	"syscall"
)

// Signals is how we configured the operating system signals for our Gracefully service manager
// Do not instantiate yourself, call: NewSignals or DefaultSignals
type Signals struct {
	// signalChan is where to put incoming signals from the OS
	signalChan chan os.Signal
	// actions is what the ServiceManager should do when a signal is received
	actions map[os.Signal]GracefulAction
	BaseSignaler
}

// NewSignals creates a new SignalSelecter that listens for operating system signals that you specify, such as SIGINT, SIGHUP, SIGTERM, etc.
func NewSignals(signalsAndActions map[os.Signal]GracefulAction) *Signals {
	s := &Signals{
		// opting for size 2 to ensure that the os.Notify does not block
		signalChan: make(chan os.Signal, 2),
		// actions allows users to specify how they want to handle signals
		actions:      signalsAndActions,
		BaseSignaler: NewBaseSignaler(),
	}

	// Extract signals
	sigs := make([]os.Signal, 0, len(signalsAndActions))
	for key := range signalsAndActions {
		sigs = append(sigs, key)
	}

	// Listen for those signals
	signal.Notify(s.signalChan, sigs...)

	// Listen for signals
	// This goroutine will wait for signals to come in from the OS, look up what to do from the map then signal to the
	// ServiceManager what it needs to do.
	go func(routineSig *Signals) {
		for {
			select {
			case gotSignal := <-routineSig.signalChan:
				// Operating system sent us an error
				routineSig.OnSignal <- func(manager *ServiceManager) GracefulAction {
					return routineSig.actions[gotSignal]
				}
			case <-routineSig.OnCancel:
				// We got a OnCancel, end the loop to prevent go routine from leaking
				return
			}
		}
	}(s)
	return s
}

// DefaultSignals creates a new Signals SignalSelecter pre-configured with:
// SIGHUP = GracefulRestart
// SIGINT = GracefulStop
// SIGTERM = GracefulStop
func DefaultSignals() *Signals {
	return NewSignals(defaultSignals)
}

// defaultSignals specifies a map of the default actions most services take when a signal arrives
var defaultSignals = map[os.Signal]GracefulAction{
	os.Interrupt:    GracefulStop,
	syscall.SIGTERM: GracefulStop,
	syscall.SIGHUP:  GracefulRestart,
}
