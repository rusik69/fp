package main

import "testing"

func TestListenAddressDefaultsToLoopback(t *testing.T) {
	t.Setenv("FP_LISTEN_ADDR", "")
	if got := listenAddress(); got != "127.0.0.1:8080" {
		t.Fatalf("listenAddress() = %q, want loopback default", got)
	}
}

func TestListenAddressHonorsExplicitOverride(t *testing.T) {
	t.Setenv("FP_LISTEN_ADDR", "0.0.0.0:9090")
	if got := listenAddress(); got != "0.0.0.0:9090" {
		t.Fatalf("listenAddress() = %q, want configured address", got)
	}
}
