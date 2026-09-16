package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type logoutRuntimeService struct {
	RuntimeService
	projectID string
	publicKey string
	token     string
}

func (s *logoutRuntimeService) Logout(_ context.Context, projectID, publicKey, token string) error {
	s.projectID, s.publicKey, s.token = projectID, publicKey, token
	return nil
}

func TestRuntimeLogoutReturnsDataEnvelope(t *testing.T) {
	service := &logoutRuntimeService{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/projects/project-1/auth/logout", nil)
	request.Header.Set("X-Vivatom-App-Key", "public-key")
	request.Header.Set("Authorization", "Bearer session-token")
	NewRouter(Dependencies{Runtime: service}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || service.projectID != "project-1" || service.publicKey != "public-key" || service.token != "session-token" {
		t.Fatalf("logout response = %d, call = %#v", recorder.Code, service)
	}
	var body struct {
		Data struct {
			LoggedOut bool `json:"loggedOut"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || !body.Data.LoggedOut {
		t.Fatalf("unexpected logout body: %s", recorder.Body.String())
	}
}
