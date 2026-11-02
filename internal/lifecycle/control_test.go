package lifecycle

import (
	"context"
	"testing"
	"time"
)

func TestLockRejectsDuplicateAndReleasesOnClose(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Acquire(dir); err == nil {
		second.Close()
		first.Close()
		t.Fatal("duplicate service lock accepted")
	}
	first.Close()
	second, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
}

func TestControlStopsOnlyMatchingInstance(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control, err := Start(dir, "device", cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	record, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	status, err := Call(context.Background(), record, false)
	if err != nil {
		t.Fatal(err)
	}
	if status["state"] != "starting" || status["ready"] != false {
		t.Fatal("new service advertised readiness before initialization")
	}
	control.Ready("http://127.0.0.1:12345")
	status, err = Call(context.Background(), record, false)
	if err != nil {
		t.Fatal(err)
	}
	if status["state"] != "running" || status["ready"] != true || status["hubUrl"] != "http://127.0.0.1:12345" {
		t.Fatal("readiness did not include the bound Hub endpoint")
	}
	wrong := record
	wrong.InstanceID = "another-instance"
	if _, err = Call(context.Background(), wrong, true); err == nil {
		t.Fatal("wrong instance stopped service")
	}
	if ctx.Err() != nil {
		t.Fatal("wrong request canceled service")
	}
	if _, err = Call(context.Background(), record, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop was not delivered")
	}
}
