package gracefully

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewServiceManager_ContextSignalStop(t *testing.T) {
	sm := NewServiceManager()
	sm.AddSignaler(NewDefaultSignals())
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	sm.Start(func(ictx context.Context) (GracefulAction, error) {
		<-ictx.Done()
		return GracefulStop, nil
	})
	cs.Stop()
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_CloseChannel(t *testing.T) {
	sm := NewServiceManager()
	sigs := NewDefaultSignals()
	sm.AddSignaler(sigs)
	sm.Start(func(ictx context.Context) (GracefulAction, error) {
		<-sigs.OnSignal
		return GracefulStop, nil
	})
	close(sigs.OnSignal)
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_CloseChannelWithOtherChannels(t *testing.T) {
	sm := NewServiceManager()
	sigs := NewDefaultSignals()
	sm.AddSignaler(sigs)
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	sm.Start(func(ictx context.Context) (GracefulAction, error) {
		<-sigs.OnSignal
		return GracefulStop, nil
	})
	close(sigs.OnSignal)
	time.Sleep(250)
	go func() {
		cs.Stop()
	}()
	err := sm.Wait()
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_RunWithCloseChannel(t *testing.T) {
	sm := NewServiceManager()
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	go func() {
		cs.Stop()
	}()
	err := sm.Run(func(ictx context.Context) (GracefulAction, error) {
		select {
		case <-time.After(time.Second / 4):
			return GracefulContinue, errors.New("not expected to close with timeout")
		case <-ictx.Done():
			return GracefulContinue, nil
		}
	})
	if err != nil {
		t.Error(err)
	}
}

func TestNewServiceManager_RunWithCloseChannelWithError(t *testing.T) {
	sm := NewServiceManager()
	cs := NewContextSignal()
	sm.AddSignaler(cs)
	go func() {
		cs.Stop()
	}()
	err := sm.Run(func(ictx context.Context) (GracefulAction, error) {
		return GracefulContinue, errors.New("expecting this error")
	})
	if err == nil {
		t.Error("expected an error")
	}
}

func TestNewServiceManager_RunReturnsError(t *testing.T) {
	sm := NewServiceManager()
	err := sm.Run(func(ictx context.Context) (GracefulAction, error) {
		return GracefulStop, errors.New("expecting this error")
	})
	if err == nil {
		t.Error("expected an error")
	}
}
