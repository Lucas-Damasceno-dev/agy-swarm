package main

import "time"

// PollUntil executa polling determinístico avaliando cond a cada interval até atingir timeout.
// Retorna true assim que a condição for satisfeita ou false se esgotar o timeout.
func PollUntil(timeout, interval time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}
