package scs

import (
	"fmt"
	"net/http"
	"time"

	"github.com/aberlorn/scs/v2/memstore"
)

// Session holds the configuration settings for your sessions.
type Session struct {
	// IdleTimeout controls the maximum length of time a session can be inactive
	// before it expires. For example, some applications may wish to set this so
	// there is a timeout after 20 minutes of inactivity.  By default IdleTimeout
	// is not set and there is no inactivity timeout.
	IdleTimeout time.Duration

	// Lifetime controls the maximum length of time that a session is valid for
	// before it expires. The lifetime is an 'absolute expiry' which is set when
	// the session is first created and does not change. The default value is 24
	// hours.
	Lifetime time.Duration

	// Store controls the session store where the session data is persisted.
	Store Store

	// Cookie contains the configuration settings for session cookies.
	Cookie SessionCookie     `json:"cookie"`

	// contextKey is the key used to set and retrieve the session data from a
	// context.Context. It's automatically generated to ensure uniqueness.
	contextKey contextKey
}

// SessionCookie contains the configuration settings for session cookies.
type SessionCookie struct {
	// Name sets the name of the session cookie. It should not contain
	// whitespace, commas, colons, semicolons, backslashes, the equals sign or
	// control characters as per RFC6265. The default cookie name is "session".
	// If your application uses two different sessions, you must make sure that
	// the cookie name for each is unique.
	Name string  `json:"name"`

	// Domain sets the 'Domain' attribute on the session cookie. By default
	// it will be set to the domain name that the cookie was issued from.
	Domain string  `json:"domain"`

	// HttpOnly sets the 'HttpOnly' attribute on the session cookie. The
	// default value is true.
	HttpOnly bool `json:"httpOnly"`

	// Path sets the 'Path' attribute on the session cookie. The default value
	// is "/". Passing the empty string "" will result in it being set to the
	// path that the cookie was issued from.
	Path string  `json:"path"`

	// Persist sets whether the session cookie should be persistent or not
	// (i.e. whether it should be retained after a user closes their browser).
	// The default value is true, which means that the session cookie will not
	// be destroyed when the user closes their browser and the appropriate
	// 'Expires' and 'MaxAge' values will be added to the session cookie.
	Persist bool `json:"persist"`

	// SameSite controls the value of the 'SameSite' attribute on the session
	// cookie. By default this is set to 'SameSite=Lax'. If you want no SameSite
	// attribute or value in the session cookie then you should set this to 0.
	SameSite http.SameSite `json:"sameSite"`

	// Secure sets the 'Secure' attribute on the session cookie. The default
	// value is false. It's recommended that you set this to true and serve all
	// requests over HTTPS in production environments.
	// See https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/Session_Management_Cheat_Sheet.md#transport-layer-security.
	Secure bool `json:"secure"`
}

// NewSession returns a new session manager with the default options. It is
// safe for concurrent use.
func NewSession() *Session {
	s := &Session{
		IdleTimeout: 0,
		Lifetime:    24 * time.Hour,
		Store:       memstore.New(),
		contextKey:  generateContextKey(),
		Cookie: SessionCookie{
			Name:     "session",
			Domain:   "",
			HttpOnly: true,
			Path:     "/",
			Persist:  true,
			Secure:   false,
			SameSite: http.SameSiteLaxMode,
		},
	}
	return s
}

// LoadFromMiddleware provides middleware which automatically loads session
// data for the current `echo` request from the client cookie.
// Override this function to implement non-cookie sessions (eg "X-SESSION")
func (s *Session) LoadFromMiddleware(c SessionContext) error {
	var token string
	cookie, err := c.Cookie(s.Cookie.Name)
	if err == nil {
		token = cookie.Value
	}

	_, err = s.Load(c, token)
	if err != nil {
		return fmt.Errorf("func s.Load failed in Session.LoadFromMiddleware; %v", err)
	}

	// Always require a token.
	// Override this function to cmment in this behavior.
	// if sd.Token() == "" {
	// 	sd.SetStatus(Modified)
	// }

	return nil
}

// SaveFromMiddleware provides middleware which saves session
// data for the current `echo` request and communicates the session token to
// the client in a cookie.
// Override this function to implement non-cookie sessions (eg "X-SESSION")
func (s *Session) SaveFromMiddleware(c SessionContext) error {
	switch s.Status(c) {
	case Modified:
		token, expiry, err := s.Commit(c)
		if err != nil {
			// log.Output(2, err.Error())
			// http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return err
		}
		s.WriteSessionCookie(c, token, expiry)
	case Destroyed:
		s.WriteSessionCookie(c, "", time.Time{})
	}
	return nil
}

// WriteSessionCookie writes the cookie to the response header.
// In echo, this must be written before a echo.Redirect.
// It is a public function in case the developer wants override
// this functionality or access from an overridden SaveFromMiddleware.
func (s *Session) WriteSessionCookie(c SessionContext, token string, expiry time.Time) {
	cookie := &http.Cookie{
		Name:     s.Cookie.Name,
		Value:    token,
		Path:     s.Cookie.Path,
		Domain:   s.Cookie.Domain,
		Secure:   s.Cookie.Secure,
		HttpOnly: s.Cookie.HttpOnly,
		SameSite: s.Cookie.SameSite,
	}

	if expiry.IsZero() {
		cookie.Expires = time.Unix(1, 0)
		cookie.MaxAge = -1
	} else if s.Cookie.Persist {
		cookie.Expires = time.Unix(expiry.Unix()+1, 0)        // Round up to the nearest second.
		cookie.MaxAge = int(time.Until(expiry).Seconds() + 1) // Round up to the nearest second.
	}

	// https://blog.fortrabbit.com/mastering-http-caching
	c.Response().Header().Add("Set-Cookie", cookie.String())
	AddHeaderIfMissing(c, "Cache-Control", `no-cache="Set-Cookie"`)
	AddHeaderIfMissing(c, "Vary", "Cookie")
}

// Add if the key/value pair is not found in the response header.
func AddHeaderIfMissing(c SessionContext, key, value string) {
	for _, h := range c.Response().Header()[key] {
		if h == value {
			return
		}
	}
	c.Response().Header().Add(key, value)
}
