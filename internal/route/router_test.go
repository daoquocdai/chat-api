package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWebAssetCachePolicy(t *testing.T) {
	router := gin.New()
	router.Use(webAssetCachePolicy)
	paths := []string{"/", "/app.js", "/realtime-core.js", "/style.css", "/health"}
	for _, path := range paths {
		router.GET(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			want := "no-store"
			if path == "/health" {
				want = ""
			}
			if got := response.Header().Get("Cache-Control"); got != want {
				t.Fatalf("Cache-Control = %q, want %q", got, want)
			}
		})
	}
}
