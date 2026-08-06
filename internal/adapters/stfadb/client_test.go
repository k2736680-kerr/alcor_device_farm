package stfadb

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeRunner struct {
	args     []string
	outputs  []string
	errors   []error
	attempts int
}

func (runner *fakeRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	runner.args = append([]string(nil), args...)
	index := runner.attempts
	runner.attempts++
	var output string
	var err error
	if index < len(runner.outputs) {
		output = runner.outputs[index]
	}
	if index < len(runner.errors) {
		err = runner.errors[index]
	}
	return output, err
}

func TestRegisterUsesExistingADBClientAndAcceptsIdempotentResponse(t *testing.T) {
	runner := &fakeRunner{outputs: []string{"already connected to 10.0.30.171:32799"}}
	client, err := New(Config{Binary: "adb", ServerAddress: "127.0.0.1:5038", Attempts: 1, runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), "10.0.30.171:32799"); err != nil {
		t.Fatal(err)
	}
	want := []string{"-H", "127.0.0.1", "-P", "5038", "connect", "10.0.30.171:32799"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("args=%v", runner.args)
	}
}

func TestRegisterRetriesAndRejectsUnsafeServerAddress(t *testing.T) {
	runner := &fakeRunner{
		outputs: []string{"", "connected to 10.0.30.171:32799"},
		errors:  []error{errors.New("temporary failure")},
	}
	client, err := New(Config{ServerAddress: "localhost:5038", Attempts: 2, RetryDelay: time.Millisecond, runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), "10.0.30.171:32799"); err != nil || runner.attempts != 2 {
		t.Fatalf("attempts=%d error=%v", runner.attempts, err)
	}
	for _, value := range []string{"", "10.0.30.171:5038", "127.0.0.1:0", "127.0.0.1:70000"} {
		if _, err := New(Config{ServerAddress: value}); err == nil {
			t.Fatalf("unsafe server address %q accepted", value)
		}
	}
}
