package gracefully

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestNewServiceManager_State(t *testing.T) {
	//t.SkipNow()
	sm := New()
	state := sm.State()
	if state != StateNew {
		t.Error("state was not StateNew, got: ", state)
	}
}

func TestNewServiceManager_ContextSignalStop(t *testing.T) {
	//t.SkipNow()
	sm := New()
	sm.AddSignaler(DefaultSignals())
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	sm.Start(func(iCtx context.Context) error {
		<-iCtx.Done()
		return nil
	})
	cs.Stop()
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_ContextSignalRestart(t *testing.T) {
	//t.SkipNow()
	count := 0
	c := make(chan int, 1)
	sm := New()
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	sm.Start(func(iCtx context.Context) error {
		<-iCtx.Done()
		count++
		c <- 1
		return nil
	})
	go func() {
		cs.Restart()
		<-c
		cs.Stop()
	}()
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
	if 2 != count {
		t.Error("expected count to be 2, but got: ", count)
	}
}

// Tests that SIgnalers whose channels are closed are removed from the system properly
func TestNewServiceManager_CloseChannel(t *testing.T) {
	//t.SkipNow()
	eventuallyExit := false
	syncer := make(chan bool)

	sm := New()
	sigs := DefaultSignals()
	sm.AddSignaler(sigs)
	sm.Start(func(iCtx context.Context) error {
		if !eventuallyExit {
			<-syncer
			eventuallyExit = true
		}
		return nil
	})
	go func() {
		close(sigs.OnSignal)
		syncer <- true
	}()
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_CloseChannelWithOtherChannels(t *testing.T) {
	//t.SkipNow()
	sm := New()
	sigs := DefaultSignals()
	sm.AddSignaler(sigs)
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	sm.Start(func(iCtx context.Context) error {
		return nil
	})
	go func() {
		// force-close the signal channel selecter
		close(sigs.OnSignal)
		// This will trigger the signal to be removed, but not the shutdown of the system.

		// Spin-lock for testing: wait until only 1 signaler remains
		for {
			sm.mu.Lock()
			l := len(sm.signalers)
			sm.mu.Unlock()
			if l == 1 {
				break
			}
		}
		cs.Stop()
	}()
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_RunWithCloseChannel(t *testing.T) {
	sm := New()
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	go func() {
		cs.Stop()
	}()
	err := sm.Run(func(iCtx context.Context) error {
		select {
		case <-time.After(time.Second / 4):
			return errors.New("not expected to close with timeout")
		case <-iCtx.Done():
			return nil
		}
	})
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_RunWithCloseChannelWithError(t *testing.T) {
	sm := New()
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	go func() {
		cs.Stop()
	}()
	err := sm.Run(func(iCtx context.Context) error {
		return errors.New("expecting this error")
	})
	if err == nil {
		t.Error("expected an error")
	}
}

func TestNewServiceManager_SimulateSignal(t *testing.T) {
	syncer := make(chan bool)
	sm := New()
	sigs := DefaultSignals()
	sm.AddSignaler(sigs)
	go func() {
		<-syncer
		sigs.signalChan <- os.Interrupt
	}()
	err := sm.Run(func(iCtx context.Context) error {
		syncer <- true
		<-iCtx.Done()
		return nil
	})
	if err != nil {
		t.Error("no error expected")
	}
}

func TestNewServiceManager_RunReturnsError(t *testing.T) {
	sm := New()
	err := sm.Run(func(iCtx context.Context) error {
		return errors.New("expecting this error")
	})
	if err == nil {
		t.Error("expected an error")
	}
}
