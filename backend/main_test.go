package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRootReturnsStatusPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	handleRoot(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	for _, expected := range []string{"GeoTrack backend", "API ONLINE", "/vehicles", "/geofences"} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Errorf("status page does not contain %q", expected)
		}
	}
}

func TestHandleRootKeepsUnknownPathsAsNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/unknown", nil)

	handleRoot(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if !strings.Contains(recorder.Body.String(), "route not found") {
		t.Fatalf("body = %q, want route not found error", recorder.Body.String())
	}
}
