package middleware

import (
	"encoding/gob"
	"fmt"
	"time"

	"github.com/aberlorn/scs/v2"
	"github.com/labstack/echo/v4"
	emidware "github.com/labstack/echo/v4/middleware"
)

type IEchoSessionSCS interface {
	GetSession() *EchoSessionSCS
	Initialize() error
	LoadCheck(c scs.SessionContext) error
	SaveCheck(c scs.SessionContext) error
}

type EchoSessionSCS struct {
	*scs.Session

	IdleTimeoutMinutes int    `json:"idleTimeoutMinutes"`
	LifetimeMinutes    int    `json:"lifetimeMinutes"`

	GOBInterfaces []interface{}
}

func (s *EchoSessionSCS) GetSession() *EchoSessionSCS {
	return s
}

// Initialize translates minute values for IdleTimout and Lifetime
// to Duration. Gobs are registered which is required for scs
// session encoding.
func (s *EchoSessionSCS) Initialize() error {
	s.Session.Lifetime = s.GetLifetime()
	s.IdleTimeout = s.GetIdleTimeout()

	for _, i := range s.GOBInterfaces {
		if i != nil {
			gob.Register(i)
		}
	}

	return nil
}

func (s *EchoSessionSCS) GetIdleTimeout() time.Duration {
	if s.IdleTimeoutMinutes <= 0 {
		return 0
	}
	return time.Duration(s.IdleTimeoutMinutes) * time.Minute
}

func (s *EchoSessionSCS) GetLifetime() time.Duration {
	// Default 1 Day = 24 * 60 = 1440 minutes
	if s.LifetimeMinutes <= 0 {
		return time.Duration(1440) * time.Minute
	}
	return time.Duration(s.LifetimeMinutes) * time.Minute
}

type SessionsConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper emidware.Skipper
	// The scs session manager
	Session IEchoSessionSCS // *EchoSessionSCS
	// Cache this configuration
	DoCache bool
}

var (
	DefaultSessionsConfig = SessionsConfig{
		Skipper: emidware.DefaultSkipper,

		// The default echo scs session
		Session: &EchoSessionSCS{Session: scs.NewSession()},

		// DoCache default is true for "DefaultSessionsConfig" especially
		// for when Sessions() is called - then SessionCache().Get(key)
		// is called by the handler to retrieve the session.
		// All custom configurations will default to DoCache=false and
		// DoCache=true must be explicitly set to enable caching.
		DoCache: true,
	}
)

func Sessions() echo.MiddlewareFunc {
 	return SessionsWithConfig(nil)
}

func SessionsWithConfig(config *SessionsConfig) echo.MiddlewareFunc {
	if config == nil {
		config = &DefaultSessionsConfig
	}
	if config.Skipper == nil {
		config.Skipper = DefaultSessionsConfig.Skipper
	}
	if config.Session == nil {
		config.Session = DefaultSessionsConfig.Session
	}
	if err := config.Session.Initialize(); err != nil {
		panic(fmt.Errorf("cannot initialize session in SessionsWithConfig; %v", err))
	}
	if config.DoCache {
		if err := SessionCache().RegisterWithErrorChecks(config.Session.GetSession().Cookie.Name, config); err != nil {
			panic(fmt.Errorf("cannot initialize session in SessionsWithConfig; %v", err))
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			if err := config.Session.LoadCheck(c); err != nil {
				return fmt.Errorf("could not load the session in SessionsWithConfig; %v", err)
			}

			// If a token has not been created, be certain to save it and write headers.
			// This code only saves to the DB on `Modified` or `Destroyed` or when token == "".
			if err := config.Session.SaveCheck(c); err != nil {
				return fmt.Errorf("could not save the session in SessionsWithConfig; %v", err)
			}

			return next(c)

			// !!! On redirects, echo forces the header to be written/flushed (eg next(c)) so
			// !!! this statement below would not be sent with the header
			// !!! BEST PRACTICE: Save the session when it is modified in the handler.
			// if errSave := config.Session.SaveFromMiddleware(c); errSave != nil {
				//return fmt.Errorf("could not save the session in SessionsWithConfig; %v", errSave)
			//}
		}
	}
}
