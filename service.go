package gracefully

import (
	"context"
	"reflect"
	"sync"
)

// ManagerStateEnum describe the state of the ServiceManager state machine
// State flows thusly:
// StateUnconfigured -> StateNew -> StateRunning <-> StateRestarting
//                           V
//                         StateDying -> StateDead
// Services can be restarted, the function provided to start is simply re-run in a new go-routine
type ManagerStateEnum uint8

const (
	// StateUnconfigured is the default state of a new service manager. You need to call New to ensure it's working properly and moves to the StateNew state
	StateUnconfigured ManagerStateEnum = iota
	// StateNew means that StateNew was called and it's awaiting being started
	StateNew
	// StateRunning indicates that the service is running, or, at least, the goroutine has started
	StateRunning
	// StateRestarting means that the restart signal has been sent. When the goroutine re-enters, state will go back to StateRunning
	StateRestarting
	// StateDying means that the system is in the same state as StateRestarting, but there is no intention to start up again. Next state will be StateDead once everything finishes
	StateDying
	// StateDead means that this service is no longer running and all child routines should be shutdown
	StateDead
)

// ServiceManager contains the logic to control a contained service
// Service Manager is intended to abstract away the control logic boiler plate for running services.
// By implementing SignalSelecter's you can add in any custom logic that controls the service manager from separate goroutines
type ServiceManager struct {
	// ServiceManager is inherently tied to goroutines, and as such we require synchronization to change some states
	mu sync.Mutex
	// signalers is a list of SignalSelectors, all of which are selected on and waited until a messages is pushed to their channels
	signalers []SignalSelecter
	// state is the current state of the ServiceManager state-machine
	state ManagerStateEnum
	// cancelFunc is the function for the context provided to the underlying service invocation
	// It must be called once the context is created to clean up resources
	cancelFunc context.CancelFunc
	// waitForIteratorDone is how we know that the inner-goroutine has completed. The error from that function is returned, or nil if no error
	waitForIteratorDone chan error
	// waitForRunning is how we know that the inner-goroutine has started
	waitForRunning chan bool
}

// New creates a new ServiceManager, initialized and ready for use
func New() *ServiceManager {
	return &ServiceManager{
		signalers:           make([]SignalSelecter, 0),
		state:               StateNew,
		waitForIteratorDone: make(chan error, 1),
		waitForRunning:      make(chan bool, 1),
	}
}

// State gets the current state of the graceful service
func (s *ServiceManager) State() ManagerStateEnum {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// setState updates the current state
func (s *ServiceManager) setState(st ManagerStateEnum) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = st
}

// AddSignaler appends a signaler interface to allow that signaler to interrupt this service while Waiting in either Wait or Run.
// The behavior is undefined if a signaler is added after Run or Start are called before Wait completes.
func (s *ServiceManager) AddSignaler(si SignalSelecter) {
	s.signalers = append(s.signalers, si)
}

// Start runs the routine in a goroutine and returns immediately if there was an error that prevented the process from starting
// If no error is returned, the goroutine is running. You can re-join the thread by calling Wait
//
// Once the goroutine is running, the state of this ServiceManager becomes "StateRunning"
//
// @param routine is the main service GoRoutine. This is your function that will be maintained by ServiceManager
// You are provided a context value. You must abide by it's deadline rules and return if Done is closed
// routine should return any errors that caused it to stop abnormally. When you return an error, ServiceManager will
// enter the StateDying state and eventually Die. Return nil to indicate no errors
// Errors returned cause ServiceManager to exit and that error will be returned by Wait/Run
//
// Once routine exits, you do not have control over ServiceManager. ServiceManager will restart it if it is told to do so, or it will not if told to stop
func (s *ServiceManager) Start(routine func(ctx context.Context) error) {
	var subCtx context.Context
	s.mu.Lock()
	subCtx, s.cancelFunc = context.WithCancel(context.Background())
	s.mu.Unlock()
	go func() {
		s.setState(StateRunning)
		s.waitForRunning <- true

		var err error
		running := true
		for running {
			// Run the function provided by the user
			err = routine(subCtx)
			// Clean up the context to release resources
			s.mu.Lock()
			if s.cancelFunc != nil {
				s.cancelFunc()
				s.cancelFunc = nil
			}
			// ServiceManager is out of control, if we ended, there is no way to shut this puppy down, so we should assume that we should end
			if len(s.signalers) == 0 {
				s.state = StateDying
			}
			currentState := s.state
			s.mu.Unlock()
			// function returned, it's 1 of 3 reasons:
			// #1: the method had an error and returned abnormally, in which case, by-pass restart, and end
			if err != nil {
				currentState = StateDying
				s.setState(StateDying)
			}

			// #2: If no error, then the process could have been cancelled by the context
			// If cancelled, we'll know about it because the context will be closed and the state will no longer be running
			// If we've been cancelled by some other Signaler, do not hang on the context cancel
			switch currentState {
			case StateRunning, StateRestarting:
				// #3: The function may have just returned for some reason
				// we're still running, this means that the function just ended itself. Since we're still running, we consider this a restart-able situation
				// we're restarting, so just create a new context and re-loop
				// Create a new context
				s.mu.Lock()
				subCtx, s.cancelFunc = context.WithCancel(context.Background())
				s.mu.Unlock()
			default:
				// Includes any state other than StateRunning or StateRestarting, including StateNew, StateDying, StateDead
				// StateNew should be impossible, as we wait until the system is running to get to this point
				// StateDead is also impossible as we set that in Wait, nothing else ever sets that state
				// we're not restarting, but stopping
				running = false
			}
		}

		// Signal that we finished, pass error received or nil
		s.waitForIteratorDone <- err
	}()
}

// Wait will block the caller and wait for the configured Signalers to push an item onto their channels.
//
// Wait will block until the main service GoRoutine has ended. This is signalled by a push to the waitForIteratorDone channel
//
// @return err the error returned from the function passed to Start/Run
func (s *ServiceManager) Wait() (err error) {
	cases := s.buildSelectCases()

	// waits for the ServiceManager to confirm running state
	<-s.waitForRunning
	// waitForRunning will never be used again, discard memory
	close(s.waitForRunning)
	s.waitForRunning = nil

	running := true
	for running {
		// chosen is the index of the selected case
		// recv is the value obtained, which will always be a SignalControl function
		// ok = false if the channel is closed
		chosen, recv, ok := reflect.Select(cases)
		if !ok {
			// not OK: channel was closed, remove from the list as we'll never receive any messages on it
			// It makes no sense to listen to it any more
			l := 0
			s.mu.Lock()
			s.removeSignaler(chosen)
			l = len(s.signalers)
			s.mu.Unlock()
			if l == 0 {
				// we're out of channels, stop the for loop
				// We'll need to wait for the server to end-itself. Closing channels does not stop the service,
				// but only the means of stopping that service
				err = <-s.waitForIteratorDone
				break
			}
			// we need to re-build the missing cases as now one is missing
			cases = s.buildSelectCases()
			continue
		}

		// Channel was not closed, we received a message
		switch chanType := recv.Interface().(type) {
		case SignalControl:
			// Our signal was OK, channel is not closed. Let's see what it says:
			switch chanType(s) {
			case GracefulRestart:
				// We need to gracefully restart
				// Trigger cancelling the context
				// We copy the value and set it to nil here to avoid having the inner go-routine call cancel a second time
				s.mu.Lock()
				s.state = StateRestarting
				if s.cancelFunc != nil {
					s.cancelFunc()
					s.cancelFunc = nil
				}
				s.mu.Unlock()

			case GracefulStop:
				// We need to stop the service
				running = false

				s.mu.Lock()
				s.state = StateDying
				if s.cancelFunc != nil {
					s.cancelFunc()
					s.cancelFunc = nil
				}
				s.mu.Unlock()

				// We're stopping, we need to wait for the goroutine to signal that it completed
				err = <-s.waitForIteratorDone
			}
		case error:
			// This means our routine completed and is no longer running
			running = false
			s.setState(StateDying)

			// The main service routine ended so we CANNOT wait for the goroutine to signal that it completed as it's already done
			// this also means that the context has already cleaned itself up, so no need to call s.cancelFunc
			// chanType could be nil, meaning no error
			err = chanType

		}
	}

	// Recover goroutine leak, if any
	s.cancelSignalers()

	s.setState(StateDead)

	close(s.waitForIteratorDone)

	return
}

// cancelSignalers instructs the added Signalers to bail out of their goroutines, if any
// some Signalers may be running their own goroutines, they need to be told to exit
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
func (s *ServiceManager) Run(routine func(ctx context.Context) error) (err error) {
	s.Start(routine)
	return s.Wait()
}
