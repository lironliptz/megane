package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	ContextUserID = "userID"
	ContextRole   = "role"
)

// User role constants — match the values stored in users.role.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// AuthRequired validates the Bearer token and sets userID and role in the Gin context.
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"data": nil, "error": "missing Authorization header"})
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"data": nil, "error": "invalid Authorization header format"})
			return
		}
		claims, err := ValidateToken(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"data": nil, "error": "invalid or expired token"})
			return
		}
		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextRole, claims.Role)
		c.Next()
	}
}

// AdminRequired is a middleware chain: AuthRequired followed by an admin role check.
func AdminRequired() gin.HandlerFunc {
	auth := AuthRequired()
	return func(c *gin.Context) {
		auth(c)
		if c.IsAborted() {
			return
		}
		role, _ := c.Get(ContextRole)
		if role != RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"data": nil, "error": "admin access required"})
			return
		}
		c.Next()
	}
}
