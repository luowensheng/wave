// ./easyserver/auth/store.go
package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidPassword   = errors.New("invalid password")
)

// InMemoryUserStore
type InMemoryUserStore struct {
	users       map[int]*User
	usersByName map[string]*User
	nextID      int
	mu          sync.RWMutex
}

func NewInMemoryUserStore() *InMemoryUserStore {
	return &InMemoryUserStore{
		users:       make(map[int]*User),
		usersByName: make(map[string]*User),
		nextID:      1,
	}
}

func (s *InMemoryUserStore) GetUserByID(id int) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[id]
	if !exists {
		return nil, ErrUserNotFound
	}

	userCopy := *user
	return &userCopy, nil
}

func (s *InMemoryUserStore) GetUserByUsername(username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.usersByName[username]
	if !exists {
		return nil, ErrUserNotFound
	}

	userCopy := *user
	return &userCopy, nil
}

func (s *InMemoryUserStore) ValidatePassword(username, password string) error {
	user, err := s.GetUserByUsername(username)
	if err != nil {
		return err
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(password))
	if err != nil {
		return ErrInvalidPassword
	}

	return nil
}

func (s *InMemoryUserStore) CreateUser(username string, hashedPassword []byte) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usersByName[username]; exists {
		return nil, ErrUserAlreadyExists
	}

	user := &User{
		ID:        s.nextID,
		Username:  username,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		IsDefault: false,
	}

	s.users[user.ID] = user
	s.usersByName[username] = user
	s.nextID++

	userCopy := *user
	return &userCopy, nil
}

func (s *InMemoryUserStore) UserExists(username string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.usersByName[username]
	return exists, nil
}

// SQLiteUserStore
type SQLiteUserStore struct {
	db *sql.DB
	mu sync.Mutex
}

func NewSQLiteUserStore(dbPath string) (*SQLiteUserStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	store := &SQLiteUserStore{db: db}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

func (s *SQLiteUserStore) initSchema() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password BLOB NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_username ON users(username);
	`

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(query)
	return err
}

func (s *SQLiteUserStore) GetUserByID(id int) (*User, error) {
	query := `SELECT id, username, password, created_at FROM users WHERE id = ?`

	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	var createdAtStr string

	err := s.db.QueryRow(query, id).Scan(
		&user.ID,
		&user.Username,
		&user.Password,
		&createdAtStr,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	user.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	user.IsDefault = false

	return &user, nil
}

func (s *SQLiteUserStore) GetUserByUsername(username string) (*User, error) {
	query := `SELECT id, username, password, created_at FROM users WHERE username = ?`

	s.mu.Lock()
	defer s.mu.Unlock()

	var user User
	var createdAtStr string

	err := s.db.QueryRow(query, username).Scan(
		&user.ID,
		&user.Username,
		&user.Password,
		&createdAtStr,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}

	user.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	user.IsDefault = false

	return &user, nil
}

func (s *SQLiteUserStore) ValidatePassword(username, password string) error {
	user, err := s.GetUserByUsername(username)
	if err != nil {
		return err
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(password))
	if err != nil {
		return ErrInvalidPassword
	}

	return nil
}

func (s *SQLiteUserStore) CreateUser(username string, hashedPassword []byte) (*User, error) {
	query := `INSERT INTO users (username, password) VALUES (?, ?)`

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check existence under the same lock (avoids deadlock from calling UserExists which also locks)
	var existsInt int
	err := s.db.QueryRow(`SELECT 1 FROM users WHERE username = ? LIMIT 1`, username).Scan(&existsInt)
	if err == nil {
		return nil, ErrUserAlreadyExists
	}

	result, err := s.db.Exec(query, username, hashedPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return &User{
		ID:        int(id),
		Username:  username,
		Password:  hashedPassword,
		CreatedAt: time.Now(),
		IsDefault: false,
	}, nil
}

func (s *SQLiteUserStore) UserExists(username string) (bool, error) {
	query := `SELECT 1 FROM users WHERE username = ? LIMIT 1`

	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	err := s.db.QueryRow(query, username).Scan(&exists)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check if user exists: %w", err)
	}

	return true, nil
}

func (s *SQLiteUserStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
