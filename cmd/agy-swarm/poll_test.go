package main

import (
	"testing"
	"time"
)

func TestPollUntil_ImmediateSuccess(t *testing.T) {
	called := 0
	ok := PollUntil(100*time.Millisecond, 10*time.Millisecond, func() bool {
		called++
		return true
	})
	if !ok {
		t.Fatalf("esperava true, obteve false")
	}
	if called != 1 {
		t.Fatalf("esperava 1 chamada, obteve %d", called)
	}
}

func TestPollUntil_SuccessAfterRetries(t *testing.T) {
	attempts := 0
	ok := PollUntil(200*time.Millisecond, 10*time.Millisecond, func() bool {
		attempts++
		return attempts >= 3
	})
	if !ok {
		t.Fatalf("esperava true após tentativas, obteve false")
	}
	if attempts < 3 {
		t.Fatalf("esperava pelo menos 3 tentativas, obteve %d", attempts)
	}
}

func TestPollUntil_Timeout(t *testing.T) {
	ok := PollUntil(50*time.Millisecond, 10*time.Millisecond, func() bool {
		return false
	})
	if ok {
		t.Fatalf("esperava timeout (false), obteve true")
	}
}
