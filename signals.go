package gracefully

import (
	"os"
	"os/signal"
	"syscall"
)

// Signals is how we configured the operating system signals for our Gracefully service manager
// Do not instantiate yourself, call: NewSignals or NewDefaultSignals
type Signals struct {
	signalChan chan os.Signal
	actions    map[os.Signal]GracefulAction
	BaseSignaler
}

// NewSignals creates a new SignalSelecter that listens for operating system signals that you specify, such as SIGINT, SIGHUP, SIGTERM, etc.
func NewSignals(signalsAndActions map[os.Signal]GracefulAction) *Signals {
	s := &Signals{
		signalChan:   make(chan os.Signal, 2),
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

// NewDefaultSignals creates a new Signals SignalSelecter pre-configured with:
// SIGHUP = GracefulRestart
// SIGINT = GracefulStop
// SIGTERM = GracefulStop
func NewDefaultSignals() *Signals {
	commonSignals := make(map[os.Signal]GracefulAction)
	commonSignals[os.Interrupt] = GracefulStop
	commonSignals[syscall.SIGTERM] = GracefulStop
	commonSignals[syscall.SIGHUP] = GracefulRestart
	return NewSignals(commonSignals)
}
