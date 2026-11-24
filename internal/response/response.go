package response

import "github.com/gin-gonic/gin"

const (
	StatusSuccess = "success"
	StatusFail    = "fail"
	StatusError   = "error"
)

// Body adalah format respons standar seluruh endpoint.
// Tipe ini juga dipakai pada anotasi Swagger (response.Body).

type Body struct {
	Status  string `json:"status" example:"success"`
	Message string `json:"message" example:"Operasi berhasil"`
	Data    any    `json:"data,omitempty"`
}

// Success untuk 2xx.
func Success(c *gin.Context, code int, message string, data any) {
	c.JSON(code, Body{Status: StatusSuccess, Message: message, Data: data})
}

// Fail untuk 4xx (kesalahan klien).
func Fail(c *gin.Context, code int, message string) {
	c.JSON(code, Body{Status: StatusFail, Message: message})
}

// Error untuk 5xx.
func Error(c *gin.Context, code int, message string) {
	c.JSON(code, Body{Status: StatusError, Message: message})
}

// AbortFail seperti Fail, tetapi menghentikan rantai handler (untuk middleware).
func AbortFail(c *gin.Context, code int, message string) {
	c.AbortWithStatusJSON(code, Body{Status: StatusFail, Message: message})
}

// AbortError seperti Error, tetapi menghentikan rantai handler.
func AbortError(c *gin.Context, code int, message string) {
	c.AbortWithStatusJSON(code, Body{Status: StatusError, Message: message})
}
