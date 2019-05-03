# Echo-SCS Session Middleware

Middleware for [echo](https://github.com/labstack/echo) forked from [alexedwards/scs](https://github.com/alexedwards/scs). This fork stays current with changes to `scs`. 

## Why the fork?

The [alexedwards/scs](https://github.com/alexedwards/scs) source base is structurally incompatible with `echo` but `scs` has a security-enabled, easy-to-understand, extensible, sessions API - which we love. We also love [echo](https://github.com/labstack/echo), so this repository marries the two. (Get a room!) 

## Reintegration with `scs`?

That would be awesome, but if not this fork will stay updated as necessary.

## Issues

If this repo needs a refresher, post an issue on [alexedwards/scs](https://github.com/alexedwards/scs) 

Some advice for getting support

* `scs`-related issues should be posted at [alexedwards/scs](https://github.com/alexedwards/scs) 
* `echo`-related middleware issues are posted at [aberlorn/scs](https://github.com/aberlorn/scs) -> Forks do not get their own issues board, so post at [alexedwards/scs](https://github.com/alexedwards/scs) for now. 

## Getting Started

Below is a starter application using in-memory storage via `memstore`. For persistence across application restarts, use persistent storage (eg `redis`).

This example overrides the `LoadCheck` function to enforce token creation by default. Privacy regimes (eg [GDPR](https://eugdpr.org/)) limit client-side cookies until the user opts-in or satisfies some other legitimate purpose. Reference the rules for your jurisdiction of service. The default `LoadCheck` does not force token creation. 


```golang
package main

import (
	"fmt"
	"net/http"
	
	"github.com/aberlorn/scs/v2"
	"github.com/labstack/echo/v4"
	emidware "github.com/labstack/echo/v4/middleware"
)

type MyEchoSessionSCS struct {
	*EchoSessionSCS
}

func (s *MyEchoSessionSCS) LoadCheck(c scs.SessionContext) error {
	var token string
	cookie, err := c.Cookie(s.Cookie.Name)
	if err == nil {
		token = cookie.Value
	}

	sd, err := s.Load(c, token)
	if err != nil {
		return fmt.Errorf("func s.Load failed in MyEchoSession.LoadCheck; %v", err)
	}

	// Always require a token.
	if sd.Token() == "" {
		sd.SetStatus(scs.Modified)
	}

	return nil
}

func main() {

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	e.Use(emidware.Logger())
	e.Use(emidware.Recover())

	// ----------------------------------------------------------
	// middleware
	session := &MyEchoSessionSCS{EchoSessionSCS: &EchoSessionSCS{Session: scs.NewSession()}}
	e.Use(SessionsWithConfig(&SessionsConfig{Session: session}))

	// ----------------------------------------------------------
	// routes
	e.GET("/", handleHome(session))
	e.GET("/home", handleHome(session))
	e.GET("/user", handleUserHome(session))
	e.GET("/login", handleLogin(session))
	e.GET("/logout", handleLogout(session))

	// ----------------------------------------------------------
	// serve
	e.Start(":4000")
}

func handleHome(session *MyEchoSessionSCS) echo.HandlerFunc {
	return func (c echo.Context) error {
		user := session.GetString(c, "user")
		return c.String(http.StatusOK, fmt.Sprintf("handleHome user=%s\n", user))
	}
}

func handleUserHome(session *MyEchoSessionSCS) echo.HandlerFunc {
	return func (c echo.Context) error {
		user := session.GetString(c, "user")

		if user == "" {
			return c.Redirect(http.StatusSeeOther, "/home")
		}

		return c.String(http.StatusOK, fmt.Sprintf("handleUserHome user=%s\n", user))
	}
}

func handleLogin(session *MyEchoSessionSCS) echo.HandlerFunc {
	return func (c echo.Context) error {
		user := session.GetString(c, "user")

		if user != "" {
			return c.Redirect(http.StatusSeeOther, "/user")
		}

		// First renew the session token...
		err := session.RenewToken(c)
		if err != nil {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("delHandler error=%v\n", err))
		}

		// Then make the privilege-level change.
		user = "Ipso Facto"
		session.Put(c, "user", user)

		if err := session.SaveCheck(c); err != nil {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("handleLogin save session error=%v\n", err))
		}

		return c.Redirect(http.StatusFound, "/user")
	}
}

func handleLogout(session *MyEchoSessionSCS) echo.HandlerFunc {
	return func (c echo.Context) error {
		user := session.GetString(c, "user")

		if user == "" {
			return c.Redirect(http.StatusSeeOther, "/home")
		}

		// First renew the session token...
		err := session.RenewToken(c)
		if err != nil {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("delHandler error=%v\n", err))
		}

		// Then make the privilege-level change.
		session.Put(c, "user", "")

		if err := session.SaveCheck(c); err != nil {
			return c.String(http.StatusInternalServerError, fmt.Sprintf("handleLogout save session error=%v\n", err))
		}

		return c.Redirect(http.StatusSeeOther, "/")
	}
}
```