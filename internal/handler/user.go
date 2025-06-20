package handler

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"gopkg.in/go-playground/validator.v9"
	"gorm.io/gorm"

	"github.com/yoanesber/go-consumer-api-with-jwt/internal/entity"
	"github.com/yoanesber/go-consumer-api-with-jwt/internal/service"
	httputil "github.com/yoanesber/go-consumer-api-with-jwt/pkg/util/http-util"
	validation "github.com/yoanesber/go-consumer-api-with-jwt/pkg/util/validation-util"
)

// This struct defines the UserHandler which handles HTTP requests related to users.
// It contains a service field of type UserService which is used to interact with the user data layer.
type UserHandler struct {
	Service service.UserService
}

// NewUserHandler creates a new instance of UserHandler.
// It initializes the UserHandler struct with the provided UserService.
func NewUserHandler(userService service.UserService) *UserHandler {
	return &UserHandler{Service: userService}
}

// GetAllUsers retrieves all users from the database and returns them as JSON.
// @Summary      Get all users
// @Description  Get all users from the database
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        page   query     string  false "Page number (default is 1)"
// @Param        limit  query     string  false "Number of transactions per page (default is 10)"
// @Success      200  {array}   model.HttpResponse for successful retrieval
// @Failure      400  {object}  model.HttpResponse for bad request
// @Failure      404  {object}  model.HttpResponse for not found
// @Failure      500  {object}  model.HttpResponse for internal server error
// @Router       /users [get]
func (h *UserHandler) GetAllUsers(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		httputil.BadRequest(c, "Invalid page number", "Page must be a positive integer")
		return
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		httputil.BadRequest(c, "Invalid limit", "Limit must be a positive integer")
		return
	}

	users, err := h.Service.GetAllUsers(page, limit)
	if err != nil {
		httputil.InternalServerError(c, "Failed to retrieve users", err.Error())
		return
	}

	if len(users) == 0 {
		httputil.NotFound(c, "No users found", "No users available in the database")
		return
	}

	httputil.Success(c, "All Users retrieved successfully", users)
}

// GetUserByID retrieves a user by their ID from the database and returns it as JSON.
// @Summary      Get user by ID
// @Description  Get a user by their ID from the database
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  model.HttpResponse for successful retrieval
// @Failure      400  {object}  model.HttpResponse for bad request
// @Failure      404  {object}  model.HttpResponse for not found
// @Failure      500  {object}  model.HttpResponse for internal server error
// @Router       /users/{id} [get]
func (h *UserHandler) GetUserByID(c *gin.Context) {
	// Parse the ID from the URL parameter
	// and convert it to an int64
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httputil.BadRequest(c, "Invalid ID format", err.Error())
		return
	}

	// Retrieve the user by ID from the service
	user, err := h.Service.GetUserByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httputil.NotFound(c, "User not found", "No user found with the given ID")
			return
		}

		// If the error is not a record not found error, return a generic internal server error
		// This is to avoid exposing internal details of the error
		httputil.InternalServerError(c, "Failed to retrieve user", err.Error())
		return
	}

	httputil.Success(c, "User retrieved successfully", user)
}

// CreateUser creates a new user in the database and returns it as JSON.
// @Summary      Create user
// @Description  Create a new user in the database
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        user  body      model.User  true  "User object"
// @Success      201  {object}  model.HttpResponse for successful creation
// @Failure      400  {object}  model.HttpResponse for bad request
// @Failure      500  {object}  model.HttpResponse for internal server error
// @Router       /users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	// Bind the JSON request body to the user struct
	// and validate the input using ShouldBindJSON
	var user entity.User
	if err := c.ShouldBindJSON(&user); err != nil {
		httputil.BadRequest(c, "Invalid request body", err.Error())
		return
	}

	// Create a new user in the database
	createdUser, err := h.Service.CreateUser(c.Request.Context(), user)
	if err != nil {
		// Check if the error is a validation error
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			httputil.BadRequestMap(c, "Failed to create user", validation.FormatValidationErrors(err))
			return
		}

		// If the error is not a validation error, return a generic error message
		// This is to avoid exposing internal details of the error
		httputil.InternalServerError(c, "Failed to create user", err.Error())
		return
	}

	httputil.Created(c, "User created successfully", createdUser)
}
