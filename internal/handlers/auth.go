package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"megane/internal/auth"
	"megane/internal/db"
)

// AuthHandler handles authentication-related endpoints.
type AuthHandler struct {
	DB *db.DB
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login validates credentials and returns a signed JWT.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": err.Error()})
		return
	}

	user, err := h.DB.GetUserByEmail(req.Email)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"data": nil, "error": "invalid credentials"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"data": nil, "error": "invalid credentials"})
		return
	}

	token, err := auth.GenerateToken(strconv.FormatInt(user.ID, 10), user.Role, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "could not generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"token": token,
			"role":  user.Role,
			"email": user.Email,
		},
		"error": nil,
	})
}

// Me returns the currently authenticated user's information.
func (h *AuthHandler) Me(c *gin.Context) {
	userIDStr, _ := c.Get(auth.ContextUserID)
	role, _ := c.Get(auth.ContextRole)

	userID, err := strconv.ParseInt(userIDStr.(string), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid user id in token"})
		return
	}

	user, err := h.DB.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":         user.ID,
			"email":      user.Email,
			"role":       role,
			"created_at": user.CreatedAt,
		},
		"error": nil,
	})
}
