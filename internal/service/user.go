package service

import (
	"context"
	"fmt"
	"time"

	"github.com/yoanesber/go-consumer-api-with-jwt/config/database"
	"github.com/yoanesber/go-consumer-api-with-jwt/internal/entity"
	"github.com/yoanesber/go-consumer-api-with-jwt/internal/repository"
	metacontext "github.com/yoanesber/go-consumer-api-with-jwt/pkg/context-data/meta-context"
	"gorm.io/gorm"
)

// Interface for user service
// This interface defines the methods that the user service should implement
type UserService interface {
	GetAllUsers(page int, limit int) ([]entity.User, error)
	GetUserByID(id int64) (entity.User, error)
	GetUserByUsername(username string) (entity.User, error)
	GetUserByEmail(email string) (entity.User, error)
	CreateUser(ctx context.Context, user entity.User) (entity.User, error)
	UpdateUser(ctx context.Context, id int64, user entity.User) (entity.User, error)
	UpdateLastLogin(id int64, lastLogin time.Time) (bool, error)
}

// This struct defines the UserService that contains a repository field of type UserRepository
// It implements the UserService interface and provides methods for user-related operations
type userService struct {
	repo repository.UserRepository
}

// NewUserService creates a new instance of UserService with the given repository.
// It initializes the userService struct and returns it.
func NewUserService(repo repository.UserRepository) UserService {
	return &userService{repo: repo}
}

// GetAllUsers retrieves all users from the database.
func (s *userService) GetAllUsers(page int, limit int) ([]entity.User, error) {
	db := database.GetPostgres()
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	// Retrieve all users from the repository
	users, err := s.repo.GetAllUsers(db, page, limit)
	if err != nil {
		return nil, err
	}

	return users, nil
}

// GetUserByID retrieves a user by its ID from the database.
func (s *userService) GetUserByID(id int64) (entity.User, error) {
	db := database.GetPostgres()
	if db == nil {
		return entity.User{}, fmt.Errorf("database connection is nil")
	}

	// Retrieve the user by ID from the repository
	user, err := s.repo.GetUserByID(db, id)
	if err != nil {
		return entity.User{}, err
	}

	return user, nil
}

// GetUserByUsername retrieves a user by their username from the database.
func (s *userService) GetUserByUsername(username string) (entity.User, error) {
	db := database.GetPostgres()
	if db == nil {
		return entity.User{}, fmt.Errorf("database connection is nil")
	}

	// Retrieve the user by username from the repository
	user, err := s.repo.GetUserByUsername(db, username)
	if err != nil {
		return entity.User{}, err
	}

	return user, nil
}

// GetUserByEmail retrieves a user by their email from the database.
func (s *userService) GetUserByEmail(email string) (entity.User, error) {
	db := database.GetPostgres()
	if db == nil {
		return entity.User{}, fmt.Errorf("database connection is nil")
	}

	// Retrieve the user by email from the repository
	user, err := s.repo.GetUserByEmail(db, email)
	if err != nil {
		return entity.User{}, err
	}

	return user, nil
}

// CreateUser creates a new user in the database.
func (s *userService) CreateUser(ctx context.Context, user entity.User) (entity.User, error) {
	db := database.GetPostgres()
	if db == nil {
		return entity.User{}, fmt.Errorf("database connection is nil")
	}

	// Validate the user struct using the validator
	if err := user.Validate(); err != nil {
		return entity.User{}, err
	}

	// Validate the user's roles
	if len(user.Roles) == 0 {
		return entity.User{}, fmt.Errorf("user must have at least one role")
	}
	for _, userRole := range user.Roles {
		if err := userRole.Validate(); err != nil {
			return entity.User{}, err
		}
	}

	createdUser := entity.User{}
	err := db.Transaction(func(tx *gorm.DB) error {
		// Check if the user's roles are valid
		rRepo := repository.NewRoleRepository()
		rServ := NewRoleService(rRepo)
		for i := range user.Roles {
			existingRole, err := rServ.GetRoleByName(user.Roles[i].Name)
			if err != nil {
				return err
			}
			if existingRole.Equals(&entity.Role{}) {
				return fmt.Errorf("role with name %s does not exist", user.Roles[i].Name)
			}

			// Assign/update the role ID in the user struct
			user.Roles[i].ID = existingRole.ID
		}

		// Check if the username already exists
		existingUser, err := s.repo.GetUserByUsername(db, user.Username)
		if (err == nil) || !(existingUser.Equals(&entity.User{})) {
			return fmt.Errorf("user with username %s already exists", user.Username)
		}

		// Check if the email already exists
		existingUser, err = s.repo.GetUserByEmail(db, user.Email)
		if (err == nil) || !(existingUser.Equals(&entity.User{})) {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}

		// Extract user metadata from the context
		meta, ok := metacontext.ExtractUserInformationMeta(ctx)
		if !ok {
			return fmt.Errorf("missing user context")
		}

		// Create a new user in the database
		user.CreatedBy = &meta.UserID
		user.UpdatedBy = user.CreatedBy
		createdUser, err = s.repo.CreateUser(tx, user)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return entity.User{}, err
	}

	return createdUser, nil
}

// UpdateUser updates an existing user in the database.
func (s *userService) UpdateUser(ctx context.Context, id int64, user entity.User) (entity.User, error) {
	db := database.GetPostgres()
	if db == nil {
		return entity.User{}, fmt.Errorf("database connection is nil")
	}

	// Validate the user struct using the validator
	if err := user.Validate(); err != nil {
		return entity.User{}, err
	}

	updatedUser := entity.User{}
	err := db.Transaction(func(tx *gorm.DB) error {
		// Check if the user exists
		existingUser, err := s.repo.GetUserByID(db, id)
		if err != nil {
			return err
		}

		// Check if the existing user is empty
		if (existingUser.Equals(&entity.User{})) {
			return fmt.Errorf("user with ID %d not found", id)
		}

		// Extract user metadata from the context
		meta, ok := metacontext.ExtractUserInformationMeta(ctx)
		if !ok {
			return fmt.Errorf("missing user context")
		}

		// Update the user in the database
		existingUser.Username = user.Username
		existingUser.Password = user.Password
		existingUser.Email = user.Email
		existingUser.Firstname = user.Firstname
		existingUser.Lastname = user.Lastname
		existingUser.IsEnabled = user.IsEnabled
		existingUser.IsAccountNonExpired = user.IsAccountNonExpired
		existingUser.IsAccountNonLocked = user.IsAccountNonLocked
		existingUser.IsCredentialsNonExpired = user.IsCredentialsNonExpired
		existingUser.IsDeleted = user.IsDeleted
		existingUser.AccountExpirationDate = user.AccountExpirationDate
		existingUser.CredentialsExpirationDate = user.CredentialsExpirationDate
		existingUser.UserType = user.UserType
		existingUser.LastLogin = user.LastLogin
		existingUser.UpdatedBy = &meta.UserID
		existingUser.Roles = user.Roles
		updatedUser, err = s.repo.UpdateUser(tx, existingUser)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return entity.User{}, err
	}

	return updatedUser, nil
}

// UpdateLastLogin updates the last login time of a user in the database.
func (s *userService) UpdateLastLogin(id int64, lastLogin time.Time) (bool, error) {
	db := database.GetPostgres()
	if db == nil {
		return false, fmt.Errorf("database connection is nil")
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		// Check if the user exists
		existingUser, err := s.repo.GetUserByID(db, id)
		if err != nil {
			return err
		}

		// Check if the existing user is empty
		if (existingUser.Equals(&entity.User{})) {
			return fmt.Errorf("user with ID %d not found", id)
		}

		// Update the last login time
		*existingUser.LastLogin = lastLogin
		_, err = s.repo.UpdateUser(tx, existingUser)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return false, err
	}

	return true, nil
}
