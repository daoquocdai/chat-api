package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const AuthenticatedUserIDKey = "authenticated_user_id"

type TokenVerifier interface {
	Verify(token string) (string, error)
}

func RequireAuthentication(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		value := strings.Fields(c.GetHeader("Authorization"))
		if len(value) != 2 || !strings.EqualFold(value[0], "Bearer") {
			abortUnauthorized(c)
			return
		}

		userID, err := verifier.Verify(value[1])
		if err != nil {
			abortUnauthorized(c)
			return
		}

		c.Set(AuthenticatedUserIDKey, userID)
		c.Next()
	}
}

func AuthenticatedUserID(c *gin.Context) (string, bool) {
	userID, ok := c.Get(AuthenticatedUserIDKey)
	if !ok {
		return "", false
	}

	value, ok := userID.(string)
	return value, ok
}

func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}
