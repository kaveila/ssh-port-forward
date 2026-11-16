package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestEmbeddedAssetServer(t *testing.T) {
	handler := application.BundledAssetFileServer(embeddedAssets)
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/", "text/html", "SSH Port Forward"},
		{"/styles.css", "text/css", "--green"},
		{"/app.js", "javascript", "GetHosts"},
		{"/wails/runtime.js", "javascript", "Call"},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		handler.ServeHTTP(recorder, request)
		response := recorder.Result()
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s returned %d: %s", test.path, response.StatusCode, body)
			continue
		}
		if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), test.contentType) {
			t.Errorf("%s content-type = %q", test.path, response.Header.Get("Content-Type"))
		}
		if !strings.Contains(string(body), test.contains) {
			t.Errorf("%s body does not contain %q", test.path, test.contains)
		}
	}
}
