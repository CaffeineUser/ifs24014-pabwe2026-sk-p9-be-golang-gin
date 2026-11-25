package auth

import "time"

type RegisterRequest struct {
	Username string `json:"username" example:"budi_santoso"`
	Email    string `json:"email" example:"budi@example.com"`
	Password string `json:"password" example:"rahasia123"`
}

type LoginRequest struct {
	Username string `json:"username" example:"budi_santoso"`
	Password string `json:"password" example:"rahasia123"`
}

type UpdateProfileRequest struct {
	Username string `json:"username" example:"budi_baru"`
	Email    string `json:"email" example:"budi.baru@example.com"`
	Password string `json:"password,omitempty" example:"rahasia123"`
}

type UserResponse struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

// ToUserResponse mengubah model User menjadi respons tanpa field sensitif
func ToUserResponse(u User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
	}
}
