package main

import "testing"

func TestVersionSet(t *testing.T) {
	if version == "" {
		t.Fatal("version must not be empty")
	}
}
