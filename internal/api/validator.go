package api

import (
	"net/http"

	"abhiudayg.com/github/gitopsctl/internal/common"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// CustomValidator holds the go-playground validator instance.
// It implements the echo.Validator interface to integrate with Echo's validation system.
type CustomValidator struct {
	validator *validator.Validate
}

// NewCustomValidator creates a new CustomValidator instance with registered validations.
func NewCustomValidator() *CustomValidator {
	v := validator.New()

	// Register custom validation for Git URLs
	_ = v.RegisterValidation("giturl", func(fl validator.FieldLevel) bool {
		return common.IsValidGitURL(fl.Field().String())
	})

	// Register custom validation for repository paths
	_ = v.RegisterValidation("path", func(fl validator.FieldLevel) bool {
		return common.IsValidRepoPath(fl.Field().String())
	})

	// Register custom validation for kubeconfig files
	_ = v.RegisterValidation("kubeconfigfile", func(fl validator.FieldLevel) bool {
		if err := common.ValidateKubeconfigFile(fl.Field().String()); err != nil {
			return false
		}
		return true
	})

	return &CustomValidator{validator: v}
}

// Validate validates the input struct.
// It uses the go-playground validator to check the struct fields based on tags.
// If validation fails, it returns an HTTP error with status 400 Bad Request.
func (cv *CustomValidator) Validate(i any) error {
	if err := cv.validator.Struct(i); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return nil
}
