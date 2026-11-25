package auth

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"app/internal/middleware"
)

// TokenTTL adalah masa berlaku token JWT.
const TokenTTL = 24 * time.Hour

const minPasswordLength = 8

var (
	ErrUserNotFound       = errors.New("Akun tidak ditemukan")
	ErrUsernameTaken      = errors.New("Username sudah digunakan")
	ErrEmailTaken         = errors.New("Email sudah digunakan")
	ErrInvalidCredentials = errors.New("Username atau password salah")
)

// ValidationError dipetakan handler menjadi HTTP 400.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,30}$`)
	emailPattern    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

	// Hash pengecoh agar waktu respons login sama baik username ada maupun tidak.
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("password-pengecoh"), bcrypt.DefaultCost)
)

type Service struct {
	repo Repository
	key  any
	ttl  time.Duration
	cost int
	now  func() time.Time
}

func NewService(repo Repository, secretKey string, ttl time.Duration) *Service {
	return &Service{
		repo: repo,
		key:  []byte(secretKey),
		ttl:  ttl,
		cost: bcrypt.DefaultCost,
		now:  time.Now,
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateIdentity(username, email string) error {
	if !usernamePattern.MatchString(username) {
		return &ValidationError{Message: "Username harus 3-30 karakter dan hanya berisi huruf, angka, atau garis bawah"}
	}
	if len(email) > 255 || !emailPattern.MatchString(email) {
		return &ValidationError{Message: "Format email tidak valid"}
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return &ValidationError{Message: "Password minimal 8 karakter"}
	}
	return nil
}

// ensureUnique memastikan username & email belum dipakai akun lain.
func (s *Service) ensureUnique(username, email string, excludeID uint) error {
	taken, err := s.repo.UsernameExists(username, excludeID)
	if err != nil {
		return err
	}
	if taken {
		return ErrUsernameTaken
	}

	taken, err = s.repo.EmailExists(email, excludeID)
	if err != nil {
		return err
	}
	if taken {
		return ErrEmailTaken
	}
	return nil
}

// Register membuat akun baru dan menerbitkan token JWT. Role SELALU member.
func (s *Service) Register(req RegisterRequest) (*AuthResponse, error) {
	username := strings.TrimSpace(req.Username)
	email := normalizeEmail(req.Email)

	if err := validateIdentity(username, email); err != nil {
		return nil, err
	}
	if err := validatePassword(req.Password); err != nil {
		return nil, err
	}
	if err := s.ensureUnique(username, email, 0); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cost)
	if err != nil {
		// bcrypt hanya gagal bila password melebihi 72 byte.
		return nil, &ValidationError{Message: "Password terlalu panjang (maksimal 72 byte)"}
	}

	user := &User{Username: username, Email: email, Password: string(hash), Role: RoleMember}
	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	return &AuthResponse{
		Token: token,
		User:  ToUserResponse(*user),
	}, nil
}

// Login memverifikasi kredensial (username atau email) lalu menerbitkan JWT.
func (s *Service) Login(req LoginRequest) (*AuthResponse, error) {
	identifier := strings.TrimSpace(req.Username)
	if identifier == "" || req.Password == "" {
		return nil, &ValidationError{Message: "Username dan password wajib diisi"}
	}

	user, err := s.repo.FindByUsername(identifier)
	if errors.Is(err, ErrUserNotFound) {
		user, err = s.repo.FindByEmail(identifier)
	}
	if errors.Is(err, ErrUserNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}
	return &AuthResponse{Token: token, User: ToUserResponse(*user)}, nil
}

func (s *Service) generateToken(user *User) (string, error) {
	now := s.now()
	claims := middleware.Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatUint(uint64(user.ID), 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
}

func (s *Service) GetProfile(id uint) (*User, error) {
	return s.repo.FindByID(id)
}

// UpdateProfile mengubah username, email, dan/atau password (role tidak bisa diubah).
func (s *Service) UpdateProfile(id uint, req UpdateProfileRequest) (*User, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" {
		user, err := s.repo.FindByID(id)
		if err != nil {
			return nil, err
		}
		username = user.Username
	}
	email := normalizeEmail(req.Email)
	if email == "" {
		user, err := s.repo.FindByID(id)
		if err != nil {
			return nil, err
		}
		email = user.Email
	}

	if err := validateIdentity(username, email); err != nil {
		return nil, err
	}

	user, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUnique(username, email, id); err != nil {
		return nil, err
	}

	if req.Password != "" {
		if err := validatePassword(req.Password); err != nil {
			return nil, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cost)
		if err != nil {
			return nil, &ValidationError{Message: "Password terlalu panjang (maksimal 72 byte)"}
		}
		user.Password = string(hash)
	}

	user.Username = username
	user.Email = email
	if err := s.repo.Update(user); err != nil {
		return nil, err
	}
	return user, nil
}
