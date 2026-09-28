package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		headerVal      string
		queryVal       string
		expectedUserID int64
	}{
		{
			name:           "Valid X-Init-Data with plain JSON user",
			headerVal:      `query_id=AAHdF6IQAAAAAN0XohDhrOrc&user={"id":12345,"username":"john"}&auth_date=1662771648`,
			expectedUserID: 12345,
		},
		{
			name:           "Valid X-Init-Data with urlencoded JSON user",
			headerVal:      "query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=" + url.QueryEscape(`{"id":67890,"username":"jane"}`) + "&auth_date=1662771648",
			expectedUserID: 67890,
		},
		{
			name:           "initData in query parameter is strictly ignored",
			queryVal:       "query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=" + url.QueryEscape(`{"id":99999}`),
			expectedUserID: 0,
		},
		{
			name:           "Header present but query param also provided: only header is used",
			headerVal:      `user={"id":11111}`,
			queryVal:       `user={"id":22222}`,
			expectedUserID: 11111,
		},
		{
			name:           "Missing X-Init-Data header falls back to 0",
			headerVal:      "",
			expectedUserID: 0,
		},
		{
			name:           "Whitespace-only X-Init-Data header falls back to 0",
			headerVal:      "   ",
			expectedUserID: 0,
		},
		{
			name:           "Missing user field in initData falls back to 0",
			headerVal:      "query_id=AAHdF6IQAAAAAN0XohDhrOrc&auth_date=1662771648",
			expectedUserID: 0,
		},
		{
			name:           "Malformed JSON in user field falls back to 0",
			headerVal:      "user={malformed-json}",
			expectedUserID: 0,
		},
		{
			name:           "User ID is 0 in user payload falls back to 0",
			headerVal:      `user={"id":0,"username":"bot"}`,
			expectedUserID: 0,
		},
		{
			name:           "Malformed query string in header falls back to 0",
			headerVal:      "user=%ZZ",
			expectedUserID: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var recordedUserID int64
			var recordedOk bool
			handlerCalled := false

			nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				recordedUserID, recordedOk = UserIDFromContext(r.Context())
			})

			middleware := AuthMiddleware(nextHandler)

			targetURL := "/api/test"
			if tc.queryVal != "" {
				targetURL += "?initData=" + url.QueryEscape(tc.queryVal)
			}

			req := httptest.NewRequest(http.MethodGet, targetURL, nil)
			if tc.headerVal != "" {
				req.Header.Set("X-Init-Data", tc.headerVal)
			}
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if !handlerCalled {
				t.Fatal("expected downstream handler to be called")
			}

			if !recordedOk {
				t.Fatal("expected UserID to be present in context")
			}

			if recordedUserID != tc.expectedUserID {
				t.Fatalf("expected userID %d, got %d", tc.expectedUserID, recordedUserID)
			}
		})
	}
}
