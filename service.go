package gracefully

import (
	"context"
	"reflect"
	"sync"
)

type ManagerStateEnum uint8

const (
	// Unconfigured is the default state of a new service manager. You need to call NewServiceManager to ensure it's working properly
	Unconfigured ManagerStateEnum = iota
	// New means that New was called and it's awaiting being started
	New
	// Running indicates that the service is running, or, at least, the goroutine has started
	Running
	// Restarting means that the restart signal has been sent. When the goroutine re-enters, state will go back to Running
	Restarting
	// Dying means that the system is in the same state as Restarting, but there is no intention to start up again. Next state will be Dead once everything finishes
	Dying
	// Dead means that this service is no longer running and all child routines should be shutdown
	Dead
)

// ServiceManager contains the logic to control a contained service
type ServiceManager struct {
	signalers           []SignalSelecter
	state               ManagerStateEnum
	cancelFunc          context.CancelFunc
	lock                sync.Mutex
	waitForIteratorDone chan error
}

// NewServiceManager creates a new ServiceManager, initialized and ready for use
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		signalers:           make([]SignalSelecter, 0),
		state:               New,
		waitForIteratorDone: make(chan error, 1),
	}
}

// State gets the current state of the graceful service
func (s *ServiceManager) State() ManagerStateEnum {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.state
}

// setState updates the current state
func (s *ServiceManager) setState(st ManagerStateEnum) {
	s.lock.Lock()
	s.state = st
	s.lock.Unlock()
}

// AddSignaler appends a signaler interface to allow that signaler to interrupt this service while Waiting in either Wait or Run.
func (s *ServiceManager) AddSignaler(si SignalSelecter) {
	s.signalers = append(s.signalers, si)
}

// Start runs the routine in a goroutine and returns immediately if there was an error that prevented the process from starting
// If no error is returned, the goroutine is running. You can re-join the thread by calling Wait
func (s *ServiceManager) Start(routine func(ictx context.Context) (GracefulAction, error)) {
	var subCtx context.Context
	subCtx, s.cancelFunc = context.WithCancel(context.Background())
	go func() {
		s.setState(Running)

		var action GracefulAction
		var err error
		running := true
		for running {
			// Run the function provided by the user
			action, err = routine(subCtx)

			// action is what the routine wants the Service Manager to do. Usually this is "GracefulContinue" to allow the service to restart gracefully
			switch action {
			case GracefulContinue:
				// If we've been cancelled by some other Signaler, do not hang on the context cancel
				if s.State() != Running {
					running = false
					break
				}
				select {
				case <-subCtx.Done():
					// context received was done
					switch s.State() {
					case Restarting:
						// we're restarting, so just create a new context and re-loop
						s.cancelFunc()
						subCtx, s.cancelFunc = context.WithCancel(context.Background())
						continue
					case Dying:
						// we're not restarting, but stopping
						running = false
					default:
						// Some other state... stop
						running = false
					}
				}
			case GracefulStop:
				running = false
			case GracefulRestart:
				// nothing to do, just re-call the user's function without waiting for the context to be cancelled
			}
		}

		// Signal that we finished, pass error received or nil
		s.waitForIteratorDone <- err
	}()
}

// Wait will block the caller and wait for the configured Signalers to push an item onto their channels.
func (s *ServiceManager) Wait() (err error) {
	cases := s.buildSelectCases()

	running := true
	for running {
		// chosen is the index of the selected case
		// recv is the value obtained, which will always be a SignalControl function
		// ok = false if the channel is closed
		chosen, recv, ok := reflect.Select(cases)
		if !ok {
			// not OK, channel was closed, remove from the list as we'll never receive any messages on it
			// It makes no sense to listen to it any more
			s.removeSignaler(chosen)
			if len(s.signalers) == 0 {
				// we're out of channels, stop the for loop
				// We'll need to wait for the server to end-itself. Closing channels does not stop the service, but only the means of stopping that service
				break
			}
			// we need to re-build the missing cases as now one is missing
			cases = s.buildSelectCases()
			continue
		}
		switch chanType := recv.Interface().(type) {
		case SignalControl:
			// Our signal was OK, channel is not closed. Let's see what it says:
			switch chanType(s) {
			case GracefulContinue:
			// We do nothing, just keep going, ignore
			case GracefulRestart:
				// We need to gracefully restart
				s.setState(Restarting)
				s.cancelFunc()
			case GracefulStop:
				// We need to stop the service
				s.setState(Dying)
				s.cancelFunc()
				// Recover goroutine leak, if any
				s.cancelSignalers()

				// We're stopping, we need to wait for the goroutine to signal that it completed
				err = <-s.waitForIteratorDone

				running = false
			}
		case error:
			// This means our routine completed and is no longer running
			// Recover goroutine leak, if any
			s.cancelSignalers()
			s.cancelFunc()

			// We're stopping, we need to wait for the goroutine to signal that it completed
			err = chanType

			running = false
		}
	}

	s.setState(Dead)

	return
}

// cancelSignalers instructs the added Signalers to bail out of their goroutines, if any
func (s *ServiceManager) cancelSignalers() {
	// We need to OnCancel all of the signalers to have them stop their routines and clean up
	for _, value := range s.signalers {
		// This should not block. It's OK if it does, but it should not
		value.Cancel()
	}
}

// removeSignaler removes a signaler from the list. Signaler's goroutine should already have ended or be on it's way to ending without the ability to recover. THis is called when a Signaler's channel is closed and can no longer send the ServiceManager messages
func (s *ServiceManager) removeSignaler(index int) bool {
	if index < len(s.signalers) {
		s.signalers[index] = s.signalers[len(s.signalers)-1]
		s.signalers = s.signalers[:len(s.signalers)-1]
	}
	return false
}

// buildSelectCases given the current Signalers creates the reflect.SelectCase's for all Signalers, plus the service routine's completion channel
func (s *ServiceManager) buildSelectCases() []reflect.SelectCase {
	cases := make([]reflect.SelectCase, len(s.signalers)+1)
	for i, value := range s.signalers {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(value.Select()),
		}
	}
	// add the waitForIterationDone
	cases[len(cases)-1].Chan = reflect.ValueOf(s.waitForIteratorDone)
	cases[len(cases)-1].Dir = reflect.SelectRecv
	return cases
}

// Run is like calling Start + Wait together
func (s *ServiceManager) Run(routine func(ictx context.Context) (GracefulAction, error)) (err error) {
	s.Start(routine)
	return s.Wait()
}
