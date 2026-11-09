package symbols

import (
	"reflect"
	"testing"
)

func TestUses(t *testing.T) {
	src := `package com.example.sample.data

class SessionRepository(private val sessionManager: SessionManager) {
    // legacyToken() is going away.
    fun authHeader(): String = "Bearer ${sessionManager.legacyToken()}"
    fun token() = sessionManager.legacyToken() ?: "legacyToken"
    fun legacyTokenLength() = 0
}
`
	got := Uses(table(t, "kotlin"), src, "legacyToken")
	want := []Position{{Line: 5, Column: 57}, {Line: 6, Column: 34}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uses = %v, want %v", got, want)
	}
	if got := Uses(table(t, "kotlin"), src, "absent"); got != nil {
		t.Errorf("uses of an absent name = %v", got)
	}
}
