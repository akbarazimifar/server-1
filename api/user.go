package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/eikendev/pushbits/authentication"
	"github.com/eikendev/pushbits/model"

	"github.com/gin-gonic/gin"
)

// The UserDatabase interface for encapsulating database access.
type UserDatabase interface {
	CreateUser(user model.ExternalUserWithCredentials) (*model.User, error)
	DeleteUser(user *model.User) error
	UpdateUser(user *model.User) error
	GetUserByID(ID uint) (*model.User, error)
	GetUserByName(name string) (*model.User, error)
	GetApplications(user *model.User) ([]model.Application, error)
	AdminUserCount() (int64, error)
}

// The UserDispatcher interface for relaying notifications.
type UserDispatcher interface {
	DeregisterApplication(a *model.Application) error
}

// UserHandler holds information for processing requests about users.
type UserHandler struct {
	DB         UserDatabase
	Dispatcher ApplicationDispatcher
}

func (h *UserHandler) userExists(name string) bool {
	user, _ := h.DB.GetUserByName(name)
	return user != nil
}

func (h *UserHandler) ensureIsNotLastAdmin(ctx *gin.Context) (int, error) {
	if count, err := h.DB.AdminUserCount(); err != nil {
		return http.StatusInternalServerError, err
	} else if count == 1 {
		return http.StatusBadRequest, errors.New("instance needs at least one privileged user")
	}

	return 0, nil
}

// CreateUser creates a new user.
func (h *UserHandler) CreateUser(ctx *gin.Context) {
	var externalUser model.ExternalUserWithCredentials

	if success := successOrAbort(ctx, http.StatusBadRequest, ctx.Bind(&externalUser)); !success {
		return
	}

	if h.userExists(externalUser.Name) {
		ctx.AbortWithError(http.StatusBadRequest, errors.New("username already exists"))
		return
	}

	user, err := h.DB.CreateUser(externalUser)

	if success := successOrAbort(ctx, http.StatusInternalServerError, err); !success {
		return
	}

	ctx.JSON(http.StatusOK, user.IntoExternalUser())
}

// DeleteUser deletes a user with a certain ID.
func (h *UserHandler) DeleteUser(ctx *gin.Context) {
	var deleteUser model.DeleteUser

	if success := successOrAbort(ctx, http.StatusBadRequest, ctx.BindUri(&deleteUser)); !success {
		return
	}

	user, err := h.DB.GetUserByID(deleteUser.ID)
	if success := successOrAbort(ctx, http.StatusNotFound, err); !success {
		return
	}

	// Last privileged user must not be deleted.
	if user.IsAdmin {
		if status, err := h.ensureIsNotLastAdmin(ctx); err != nil {
			ctx.AbortWithError(status, err)
			return
		}
	}

	log.Printf("Deleting user %s.\n", user.Name)

	applications, err := h.DB.GetApplications(user)
	if success := successOrAbort(ctx, http.StatusInternalServerError, err); !success {
		return
	}

	for _, app := range applications {
		if success := successOrAbort(ctx, http.StatusInternalServerError, h.Dispatcher.DeregisterApplication(&app)); !success {
			return
		}
	}

	if success := successOrAbort(ctx, http.StatusInternalServerError, h.DB.DeleteUser(user)); !success {
		return
	}

	ctx.JSON(http.StatusOK, gin.H{})
}

// UpdateUser updates a user with a certain ID.
func (h *UserHandler) UpdateUser(ctx *gin.Context) {
	var updateUser model.UpdateUser

	if success := successOrAbort(ctx, http.StatusBadRequest, ctx.BindUri(&updateUser)); !success {
		return
	}

	user, err := h.DB.GetUserByID(updateUser.ID)
	if success := successOrAbort(ctx, http.StatusNotFound, err); !success {
		return
	}

	currentUser := authentication.GetUser(ctx)

	// Last privileged user must not be taken privileges. Assumes that the current user has privileges.
	if user.ID == currentUser.ID && !updateUser.IsAdmin {
		if status, err := h.ensureIsNotLastAdmin(ctx); err != nil {
			ctx.AbortWithError(status, err)
			return
		}
	}

	log.Printf("Updating user %s.\n", user.Name)

	// If users can later update their own user, make sure they cannot give themselves privileges.
	// TODO: Handle unbound members.
	// TODO: Allow updating of password.
	// TODO: Update rooms of applications when the user's MatrixID changes.
	user.Name = updateUser.Name
	user.MatrixID = updateUser.MatrixID
	user.IsAdmin = updateUser.IsAdmin

	if success := successOrAbort(ctx, http.StatusInternalServerError, h.DB.UpdateUser(user)); !success {
		return
	}

	ctx.JSON(http.StatusOK, gin.H{})
}
