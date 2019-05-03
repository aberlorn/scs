package middleware

import (
	"encoding/gob"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aberlorn/scs/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

type MyEchoSession struct {
	*EchoSessionSCS
}

func (s *MyEchoSession) LoadFromMiddleware(c scs.SessionContext) error {
	var token string
	cookie, err := c.Cookie(s.Cookie.Name)
	if err == nil {
		token = cookie.Value
	}

	sd, err := s.Load(c, token)
	if err != nil {
		return fmt.Errorf("func s.Load failed in MyEchoSession.LoadFromMiddleware; %v", err)
	}

	// Always require a token.
	// Override this function to remove this behavior.
	if sd.Token() == "" {
		sd.SetStatus(scs.Modified)
	}

	return nil
}

func (s *MyEchoSession) SaveFromMiddleware(c scs.SessionContext) error {
	switch s.Status(c) {
	case scs.Modified:
		token, expiry, err := s.Commit(c)
		if err != nil {
			// log.Output(2, err.Error())
			// http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return err
		}
		s.WriteSessionCookie(c, token, expiry)
	case scs.Destroyed:
		s.WriteSessionCookie(c, "", time.Time{})
	}
	return nil
}

type MyEchoSessionForcePanic struct {
	*EchoSessionSCS
}

func (s *MyEchoSessionForcePanic) Initialize() error {
	return fmt.Errorf("testing panic")
}

func TestMiddlewareSkipper(t *testing.T) {
	if SessionCache().Length() != 0 {
		t.Fatalf("pre-test session cache should be 0 but it is %d", SessionCache().Length())
	}

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	req := httptest.NewRequest(echo.GET, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// ----------------------------------------------------------
	// Skipper
	scNil := &SessionsConfig{
		Skipper: func(c echo.Context) bool {
			return true
		},
	}
	mw := SessionsWithConfig(scNil)

	h := mw(echo.NotFoundHandler)
	assert.Error(t, h(c)) // 404
	assert.Nil(t, c.Get(scNil.Session.GetSession().Cookie.Name))

	if SessionCache().Length() != 0 {
		t.Fatalf("session cache should be 0 but it is %d", SessionCache().Length())
	}
}

func TestMiddlewarePanic(t *testing.T) {
	if SessionCache().Length() != 0 {
		t.Fatalf("pre-test session cache should be 0 but it is %d", SessionCache().Length())
	}

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	req := httptest.NewRequest(echo.GET, "/", nil)
	rec := httptest.NewRecorder()
	e.NewContext(req, rec)

	// ----------------------------------------------------------
	// Panic
	scPanic := &SessionsConfig{
		Session: &MyEchoSessionForcePanic{EchoSessionSCS: &EchoSessionSCS{Session: scs.NewSession()}},
	}
	assert.Panics(t, func() {
		SessionsWithConfig(scPanic)
	})

	if SessionCache().Length() != 0 {
		t.Fatalf("session cache should be 0 but it is %d", SessionCache().Length())
	}
}

func TestMiddlewareDefault(t *testing.T) {
	if SessionCache().Length() != 0 {
		t.Fatalf("pre-test session cache should be 0 but it is %d", SessionCache().Length())
	}

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	req := httptest.NewRequest(echo.GET, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// ----------------------------------------------------------
	// Session - Default
	mw := Sessions()
	h := mw(func(c echo.Context) error {
		session := SessionCache().Get("session")

		message := session.Session.GetSession().GetString(c, "message")

		if message != "" {
			return fmt.Errorf("message should be empty")
		}

		message = "Ipso Facto"
		session.Session.GetSession().Put(c, "message", message)

		if err := session.Session.GetSession().SaveFromMiddleware(c); err != nil {
			return fmt.Errorf("cannot save session; %v", err)
		}

		return c.String(http.StatusOK, message)
	})
	assert.NoError(t, h(c))
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), "session")
	assert.Contains(t, rec.Body.String(), "Ipso Facto")

	if SessionCache().Length() != 1 {
		t.Fatalf("session cache should be 1 but it is %d", SessionCache().Length())
	}

	SessionCache().Remove("session")

	if SessionCache().Length() != 0 {
		t.Fatalf("post-test session cache should be 0 but it is %d", SessionCache().Length())
	}
}

func TestLoadFromMiddlewareOverride(t *testing.T) {
	if SessionCache().Length() != 0 {
		t.Fatalf("pre-test session cache should be 0 but it is %d", SessionCache().Length())
	}

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	req := httptest.NewRequest(echo.GET, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// ----------------------------------------------------------
	// Session - Overriding LoadFromMiddleware
	scMyEchoSession := &SessionsConfig{
		Session: &MyEchoSession{EchoSessionSCS: &EchoSessionSCS{Session: scs.NewSession()}},
		DoCache: true,
	}

	var tokenid string
	mw := SessionsWithConfig(scMyEchoSession)
	h := mw(func(c echo.Context) error {
		session := SessionCache().Get(scMyEchoSession.Session.GetSession().Cookie.Name)
		tokenid = session.Session.GetSession().Token(c)
		return c.String(http.StatusOK, tokenid)
	})
	assert.NoError(t, h(c))
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), scMyEchoSession.Session.GetSession().Cookie.Name)
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), tokenid)
	assert.Contains(t, rec.Body.String(), tokenid)

	if SessionCache().Length() != 1 {
		t.Fatalf("session cache should be 1 but it is %d", SessionCache().Length())
	}

	SessionCache().Remove("session")

	if SessionCache().Length() != 0 {
		t.Fatalf("post-test session cache should be 0 but it is %d", SessionCache().Length())
	}
}

func TestDualSessionsLoadFromMiddlewareOverride(t *testing.T) {
	if SessionCache().Length() != 0 {
		t.Fatalf("pre-test session cache should be 0 but it is %d", SessionCache().Length())
	}

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	req := httptest.NewRequest(echo.GET, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// ----------------------------------------------------------
	// Session1
	scMyEchoSession1 := &SessionsConfig{
		Session: &MyEchoSession{EchoSessionSCS: &EchoSessionSCS{Session: scs.NewSession()}},
		DoCache: true,
	}
	scMyEchoSession1.Session.GetSession().Cookie.Name = "session1"

	var tokenid string
	mw := SessionsWithConfig(scMyEchoSession1)
	h := mw(func(c echo.Context) error {
		session := SessionCache().Get(scMyEchoSession1.Session.GetSession().Cookie.Name)
		tokenid = session.Session.GetSession().Token(c)
		return c.String(http.StatusOK, tokenid)
	})
	assert.NoError(t, h(c))
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), scMyEchoSession1.Session.GetSession().Cookie.Name)
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), tokenid)
	assert.Contains(t, rec.Body.String(), tokenid)

	// ----------------------------------------------------------
	// Session2
	scMyEchoSession2 := &SessionsConfig{
		Session: &MyEchoSession{EchoSessionSCS: &EchoSessionSCS{Session: scs.NewSession()}},
		DoCache: true,
	}
	scMyEchoSession2.Session.GetSession().Cookie.Name = "session2"

	mw2 := SessionsWithConfig(scMyEchoSession2)
	h = mw2(func(c echo.Context) error {
		session := SessionCache().Get(scMyEchoSession2.Session.GetSession().Cookie.Name)
		tokenid = session.Session.GetSession().Token(c)
		return c.String(http.StatusOK, tokenid)
	})
	assert.NoError(t, h(c))

	header := rec.Header()

	if val, ok := header[echo.HeaderSetCookie]; !ok {
		t.Fatalf("could not find header property %s", echo.HeaderSetCookie)
	} else if val == nil {
		t.Fatalf("no instance for header property %s", echo.HeaderSetCookie)
	} else if len(val) != 2 {
		t.Fatalf("should have 2 %s instances in header but have %d", echo.HeaderSetCookie, len(val))
	} else {
		assert.Contains(t, val[1], scMyEchoSession2.Session.GetSession().Cookie.Name)
		assert.Contains(t, val[1], tokenid)
	}

	assert.Contains(t, rec.Body.String(), tokenid)

	if SessionCache().Length() != 2 {
		t.Fatalf("session cache should be 1 but it is %d", SessionCache().Length())
	}

	SessionCache().Remove("session1")
	SessionCache().Remove("session2")

	if SessionCache().Length() != 0 {
		t.Fatalf("post-test session cache should be 0 but it is %d", SessionCache().Length())
	}
}

type MyObject struct {
	AString string
	AInt int
}

func sessionFromContext(c echo.Context) *MyObject {
	obj, ok := c.Get("obj").(MyObject)
	if !ok {
		return nil
	}
	return &obj
}

func TestLoadFromMiddlewareObject(t *testing.T) {
	if SessionCache().Length() != 0 {
		t.Fatalf("pre-test session cache should be 0 but it is %d", SessionCache().Length())
	}

	// ----------------------------------------------------------
	// register with gob
	// required by PutObject
	// see https://github.com/alexedwards/scs/blob/876a0fdbdd8ce6c328b2e8064a7483ff377ddaa4/session.go#L535
	gob.Register(MyObject{})

	// ----------------------------------------------------------
	// init echo
	e := echo.New()

	req := httptest.NewRequest(echo.GET, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// ----------------------------------------------------------
	// Session - Overriding LoadFromMiddleware
	scMyEchoSession := &SessionsConfig{
		Session: &MyEchoSession{EchoSessionSCS: &EchoSessionSCS{Session: scs.NewSession()}},
		DoCache: true,
	}

	var tokenid string
	mw := SessionsWithConfig(scMyEchoSession)
	h := mw(func(c echo.Context) error {
		session := SessionCache().Get(scMyEchoSession.Session.GetSession().Cookie.Name)
		tokenid = session.Session.GetSession().Token(c)

		obj := session.Session.GetSession().Get(c, "obj")
		if obj == nil {
			obj = &MyObject{AString: "mystring", AInt: 100}
			session.Session.GetSession().Put(c, "obj", obj)
			if err := session.Session.GetSession().SaveFromMiddleware(c); err != nil {
				t.Fatal(err)
			}
		}

		// cache "us" inside echo
		c.Set("obj", obj)

		return c.String(http.StatusOK, tokenid)
	})
	assert.NoError(t, h(c))
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), scMyEchoSession.Session.GetSession().Cookie.Name)
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), tokenid)
	assert.Contains(t, rec.Body.String(), tokenid)

	// ----------------------------------------------------------
	// Handler2
	req = httptest.NewRequest(echo.GET, "/", nil)
	req.Header.Add("Cookie", fmt.Sprintf("session=%s", tokenid))
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	var tokenid2 string
	h = mw(func(c echo.Context) error {
		session := SessionCache().Get(scMyEchoSession.Session.GetSession().Cookie.Name)
		tokenid2 = session.Session.GetSession().Token(c)

		if tokenid != tokenid2 {
			t.Fatalf("tokens do not match; tokenid=%s   tokenid2=%s", tokenid, tokenid2)
		}

		obj, _ := session.Session.GetSession().Get(c, "obj").(MyObject)

		// cache "us" inside echo
		c.Set("obj", obj)

		return c.String(http.StatusOK, fmt.Sprintf("%s | %d", obj.AString, obj.AInt))
	})
	assert.NoError(t, h(c))
	assert.Contains(t, rec.Body.String(), "mystring | 100")

	obj := sessionFromContext(c)
	assert.Contains(t, "mystring | 100", fmt.Sprintf("%s | %d", obj.AString, obj.AInt))

	if SessionCache().Length() != 1 {
		t.Fatalf("session cache should be 1 but it is %d", SessionCache().Length())
	}

	SessionCache().Remove("session")

	if SessionCache().Length() != 0 {
		t.Fatalf("post-test session cache should be 0 but it is %d", SessionCache().Length())
	}
}
