package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": data, "message": "success"})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"code": 201, "data": data, "message": "created"})
}

func BadRequest(c *gin.Context, message string) {
	if message == "" {
		message = "bad request"
	}
	c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": message})
}

func NotFound(c *gin.Context, message string) {
	if message == "" {
		message = "not found"
	}
	c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": message})
}

func Conflict(c *gin.Context, message string) {
	if message == "" {
		message = "conflict"
	}
	c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": message})
}

func ServerError(c *gin.Context, message string) {
	if message == "" {
		message = "internal error"
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": message})
}

func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
