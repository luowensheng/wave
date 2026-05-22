// Package usecases wires the injected function variables declared in each
// usecases/<name> package to the concrete feature implementations that live
// in orchestrator/features/*.  Call WireAll() once, after all feature
// managers (auth, storage) have been initialized.
package usecases

import (
	"net/http"

	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/infra/plugins/kinds"
	authfeature "github.com/luowensheng/wave/orchestrator/features/auth"
	storagefeature "github.com/luowensheng/wave/orchestrator/features/storage"

	authlg "github.com/luowensheng/wave/usecases/auth_login"
	authlo "github.com/luowensheng/wave/usecases/auth_logout"
	authsg "github.com/luowensheng/wave/usecases/auth_signup"
	servefile "github.com/luowensheng/wave/usecases/serve_file"
	storageaccess "github.com/luowensheng/wave/usecases/storage_access"
)

// WireAll binds all package-level function variables in usecases/* to
// their concrete feature implementations. Must be called after
// authfeature.InitAuthManager and storagefeature.InitStorage.
func WireAll() {
	wireAuthLogin()
	wireAuthSignup()
	wireAuthLogout()
	wireStorage()
	wireServeFile()
}

// ── auth_login ────────────────────────────────────────────────────────────────

func wireAuthLogin() {
	convert := func(r *authfeature.LoginResponse) *authlg.LoginResponse {
		if r == nil {
			return &authlg.LoginResponse{Success: false, Error: "login failed", Code: "internal_error"}
		}
		out := &authlg.LoginResponse{
			Success:       r.Success,
			Location:      r.Location,
			Error:         r.Error,
			Code:          r.Code,
			Message:       r.Message,
			Details:       r.Details,
			Name:          r.Name,
			Value:         r.Value,
			TokenDuration: r.TokenDuration,
			RedirectTo:    r.RedirectTo,
			ExtraCookies:  r.ExtraCookies,
		}
		if r.User != nil {
			out.UserID = r.User.ID
			out.Username = r.User.Username
		}
		return out
	}
	authlg.LoginFn = func(username, password, authConfigName string) *authlg.LoginResponse {
		return convert(authfeature.Login(authfeature.LoginForm{Username: username, Password: password}, authConfigName))
	}
	authlg.LoginFnWithRequest = func(username, password, authConfigName string, r *http.Request) *authlg.LoginResponse {
		return convert(authfeature.LoginWithRequest(authfeature.LoginForm{Username: username, Password: password}, authConfigName, r))
	}
}

// ── auth_signup ───────────────────────────────────────────────────────────────

func wireAuthSignup() {
	authsg.SignupFn = func(username, password, passwordRepeat, authConfigName string) *authsg.LoginResponse {
		r := authfeature.Signup(authfeature.SignupForm{
			Username:       username,
			Password:       password,
			PasswordRepeat: passwordRepeat,
		}, authConfigName)
		if r == nil {
			return &authsg.LoginResponse{Success: false, Error: "signup failed", Code: "internal_error"}
		}
		out := &authsg.LoginResponse{
			Success: r.Success,
			Error:   r.Error,
			Code:    r.Code,
			Message: r.Message,
			Details: r.Details,
		}
		if r.User != nil {
			out.UserID = r.User.ID
			out.Username = r.User.Username
		}
		return out
	}

	// Auto-login after successful signup delegates to the same auth feature.
	authsg.LoginFn = func(username, password, authConfigName string) *authsg.LoginResponse {
		r := authfeature.Login(authfeature.LoginForm{Username: username, Password: password}, authConfigName)
		if r == nil {
			return &authsg.LoginResponse{Success: false, Error: "login failed", Code: "internal_error"}
		}
		out := &authsg.LoginResponse{
			Success:       r.Success,
			Location:      r.Location,
			Error:         r.Error,
			Code:          r.Code,
			Message:       r.Message,
			Details:       r.Details,
			Name:          r.Name,
			Value:         r.Value,
			TokenDuration: r.TokenDuration,
			RedirectTo:    r.RedirectTo,
		}
		if r.User != nil {
			out.UserID = r.User.ID
			out.Username = r.User.Username
		}
		return out
	}
}

// ── auth_logout ───────────────────────────────────────────────────────────────

func wireAuthLogout() {
	authlo.LogoutFn = func(r *http.Request, authConfigName string) *authlo.LogoutResponse {
		res := authfeature.Logout(r, authConfigName)
		if res == nil {
			return &authlo.LogoutResponse{Success: false, Error: "logout failed", Code: "internal_error"}
		}
		return &authlo.LogoutResponse{
			Success:    res.Success,
			Location:   res.Location,
			Name:       res.Name,
			Value:      res.Value,
			Message:    res.Message,
			Error:      res.Error,
			Code:       res.Code,
			RedirectTo: res.RedirectTo,
		}
	}
}

// ── storage_access ────────────────────────────────────────────────────────────

func wireStorage() {
	// Snapshot plugin-backed storage backends once at boot so per-request
	// lookups don't take the registry lock or rebuild adapters. Built-in
	// names (Config.Storage) take precedence so existing setups are
	// untouched; plugin names fill in for anything else.
	storagePlugins := kinds.LoadStorage(plugins.Default())
	storageaccess.GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		if ref, ok := storagefeature.GetFromStorage(name); ok {
			return ref, true
		}
		if p, ok := storagePlugins[name]; ok {
			return &kinds.StorageRefAdapter{Plugin: p}, true
		}
		return nil, false
	}
}

// ── serve_file ────────────────────────────────────────────────────────────────

func wireServeFile() {
	servefile.GetUserFn = func(r *http.Request) interface{} {
		return r.Context().Value(authfeature.UserContextKey)
	}
}
