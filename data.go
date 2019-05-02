package scs

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"github.com/labstack/echo/v4"
	"net/http"
	"sort"
	"sync"
	"time"
)

// This interface matches the `Get` and `Set` found in echo.Context.
type SessionContext interface {
	Get(key string) interface{}
	Set(key string, val interface{})
	Cookie(name string) (*http.Cookie, error)
	Response() *echo.Response
}

// Status represents the state of the session data during a request cycle.
type Status int

const (
	// Unmodified indicates that the session data hasn't been changed in the
	// current request cycle.
	Unmodified Status = iota

	// Modified indicates that the session data has been changed in the current
	// request cycle.
	Modified

	// Destroyed indicates that the session data has been destroyed in the
	// current request cycle.
	Destroyed
)

type sessionData struct {
	Deadline time.Time // Exported for gob encoding.
	status   Status
	token    string
	Values   map[string]interface{} // Exported for gob encoding.
	mu       sync.Mutex
}

func (sd *sessionData) Token() string {
	return sd.token
}

func (sd *sessionData) SetStatus(status Status) {
	sd.status = status
}

func newSessionData(lifetime time.Duration) *sessionData {
	return &sessionData{
		Deadline: time.Now().Add(lifetime).UTC(),
		status:   Unmodified,
		Values:   make(map[string]interface{}),
	}
}

// Load retrieves the session data for the given token from the session store,
// and returns a new context.Context containing the session data. If no matching
// token is found then this will create a new session.
//
// Most applications will use the LoadAndSave() middleware and will not need to
// use this method.
func (s *Session) Load(c SessionContext, token string) (*sessionData, error) {
	val := c.Get(string(s.contextKey))
	if val != nil {
		sd, ok := val.(*sessionData)
		if ok {
			return sd, nil
		}
	}

	if token == "" {
		sd := newSessionData(s.Lifetime)
		c.Set(string(s.contextKey), sd)
		return sd, nil
	}

	b, found, err := s.Store.Find(token)
	if err != nil {
		return nil, err
	} else if !found {
		sd := newSessionData(s.Lifetime)
		c.Set(string(s.contextKey), sd)
		return sd, nil
	}

	sd := &sessionData{
		status: Unmodified,
		token:  token,
	}
	err = sd.decode(b)
	if err != nil {
		return nil, err
	}
	// Mark the session data as modified if an idle timeout is being used. This
	// will force the session data to be re-committed to the session store with
	// a new expiry time.
	if s.IdleTimeout > 0 {
		sd.status = Modified
	}

	c.Set(string(s.contextKey), sd)
	return sd, nil
}

// Commit saves the session data to the session store and returns the session
// token and expiry time.
//
// Most applications will use the LoadAndSave() middleware and will not need to
// use this method.
func (s *Session) Commit(c SessionContext) (string, time.Time, error) {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.token == "" {
		var err error
		sd.token, err = generateToken()
		if err != nil {
			return "", time.Time{}, err
		}
	}

	b, err := sd.encode()
	if err != nil {
		return "", time.Time{}, err
	}

	expiry := sd.Deadline
	if s.IdleTimeout > 0 {
		ie := time.Now().Add(s.IdleTimeout)
		if ie.Before(expiry) {
			expiry = ie
		}
	}

	err = s.Store.Commit(sd.token, b, expiry)
	if err != nil {
		return "", time.Time{}, err
	}

	return sd.token, expiry, nil
}

// Destroy deletes the session data from the session store and sets the session
// status to Destroyed. Any futher operations in the same request cycle will
// result in a new session being created.
func (s *Session) Destroy(c SessionContext) error {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	err := s.Store.Delete(sd.token)
	if err != nil {
		return err
	}

	sd.status = Destroyed

	// Reset everything else to defaults.
	sd.token = ""
	sd.Deadline = time.Now().Add(s.Lifetime).UTC()
	for key := range sd.Values {
		delete(sd.Values, key)
	}

	return nil
}

// Put adds a key and corresponding value to the session data. Any existing
// value for the key will be replaced. The session data status will be set to
// Modified.
func (s *Session) Put(c SessionContext, key string, val interface{}) {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	sd.Values[key] = val
	sd.status = Modified
	sd.mu.Unlock()
}

// Get returns the value for a given key from the session data. The return
// value has the type interface{} so will usually need to be type asserted
// before you can use it. For example:
//
//	foo, ok := session.Get(r, "foo").(string)
//	if !ok {
//		return errors.New("type assertion to string failed")
//	}
//
// Also see the GetString(), GetInt(), GetBytes() and other helper methods which
// wrap the type conversion for common types.
func (s *Session) Get(c SessionContext, key string) interface{} {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	return sd.Values[key]
}

// Pop acts like a one-time Get. It returns the value for a given key from the
// session data and deletes the key and value from the session data. The
// session data status will be set to Modified. The return value has the type
// interface{} so will usually need to be type asserted before you can use it.
func (s *Session) Pop(c SessionContext, key string) interface{} {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	val, exists := sd.Values[key]
	if !exists {
		return nil
	}
	delete(sd.Values, key)
	sd.status = Modified

	return val
}

// Remove deletes the given key and corresponding value from the session data.
// The session data status will be set to Modified. If the key is not present
// this operation is a no-op.
func (s *Session) Remove(c SessionContext, key string) {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	_, exists := sd.Values[key]
	if !exists {
		return
	}

	delete(sd.Values, key)
	sd.status = Modified
}

// Exists returns true if the given key is present in the session data.
func (s *Session) Exists(c SessionContext, key string) bool {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	_, exists := sd.Values[key]
	sd.mu.Unlock()

	return exists
}

// Keys returns a slice of all key names present in the session data, sorted
// alphabetically. If the data contains no data then an empty slice will be
// returned.
func (s *Session) Keys(c SessionContext) []string {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	keys := make([]string, len(sd.Values))
	i := 0
	for key := range sd.Values {
		keys[i] = key
		i++
	}
	sd.mu.Unlock()

	sort.Strings(keys)
	return keys
}

// RenewToken updates the session data to have a new session token while
// retaining the current session data. The session lifetime is also reset and
// the session data status will be set to Modified.
//
// The old session token and accompanying data are deleted from the session store.
//
// To mitigate the risk of session fixation attacks, it's important that you call
// RenewToken before making any changes to privilege levels (e.g. login and
// logout operations). See https://github.com/OWASP/CheatSheetSeries/blob/master/cheatsheets/Session_Management_Cheat_Sheet.md#renew-the-session-id-after-any-privilege-level-change
// for additional information.
func (s *Session) RenewToken(c SessionContext) error {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	err := s.Store.Delete(sd.token)
	if err != nil {
		return err
	}

	newToken, err := generateToken()
	if err != nil {
		return err
	}

	sd.token = newToken
	sd.Deadline = time.Now().Add(s.Lifetime).UTC()
	sd.status = Modified

	return nil
}

// Status returns the current status of the session data.
func (s *Session) Status(c SessionContext) Status {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	return sd.status
}

// GetString returns the string value for a given key from the session data.
// The zero value for a string ("") is returned if the key does not exist or the
// value could not be type asserted to a string.
func (s *Session) GetString(c SessionContext, key string) string {
	val := s.Get(c, key)
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

// GetBool returns the bool value for a given key from the session data. The
// zero value for a bool (false) is returned if the key does not exist or the
// value could not be type asserted to a bool.
func (s *Session) GetBool(c SessionContext, key string) bool {
	val := s.Get(c, key)
	b, ok := val.(bool)
	if !ok {
		return false
	}
	return b
}

// GetInt returns the int value for a given key from the session data. The
// zero value for an int (0) is returned if the key does not exist or the
// value could not be type asserted to an int.
func (s *Session) GetInt(c SessionContext, key string) int {
	val := s.Get(c, key)
	i, ok := val.(int)
	if !ok {
		return 0
	}
	return i
}

// GetFloat returns the float64 value for a given key from the session data. The
// zero value for an float64 (0) is returned if the key does not exist or the
// value could not be type asserted to a float64.
func (s *Session) GetFloat(c SessionContext, key string) float64 {
	val := s.Get(c, key)
	f, ok := val.(float64)
	if !ok {
		return 0
	}
	return f
}

// GetBytes returns the byte slice ([]byte) value for a given key from the session
// data. The zero value for a slice (nil) is returned if the key does not exist
// or could not be type asserted to []byte.
func (s *Session) GetBytes(c SessionContext, key string) []byte {
	val := s.Get(c, key)
	b, ok := val.([]byte)
	if !ok {
		return nil
	}
	return b
}

// GetTime returns the time.Time value for a given key from the session data. The
// zero value for a time.Time object is returned if the key does not exist or the
// value could not be type asserted to a time.Time. This can be tested with the
// time.IsZero() method.
func (s *Session) GetTime(c SessionContext, key string) time.Time {
	val := s.Get(c, key)
	t, ok := val.(time.Time)
	if !ok {
		return time.Time{}
	}
	return t
}

// PopString returns the string value for a given key and then deletes it from the
// session data. The session data status will be set to Modified. The zero
// value for a string ("") is returned if the key does not exist or the value
// could not be type asserted to a string.
func (s *Session) PopString(c SessionContext, key string) string {
	val := s.Pop(c, key)
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

// PopBool returns the bool value for a given key and then deletes it from the
// session data. The session data status will be set to Modified. The zero
// value for a bool (false) is returned if the key does not exist or the value
// could not be type asserted to a bool.
func (s *Session) PopBool(c SessionContext, key string) bool {
	val := s.Pop(c, key)
	b, ok := val.(bool)
	if !ok {
		return false
	}
	return b
}

// PopInt returns the int value for a given key and then deletes it from the
// session data. The session data status will be set to Modified. The zero
// value for an int (0) is returned if the key does not exist or the value could
// not be type asserted to an int.
func (s *Session) PopInt(c SessionContext, key string) int {
	val := s.Pop(c, key)
	i, ok := val.(int)
	if !ok {
		return 0
	}
	return i
}

// PopFloat returns the float64 value for a given key and then deletes it from the
// session data. The session data status will be set to Modified. The zero
// value for an float64 (0) is returned if the key does not exist or the value
// could not be type asserted to a float64.
func (s *Session) PopFloat(c SessionContext, key string) float64 {
	val := s.Pop(c, key)
	f, ok := val.(float64)
	if !ok {
		return 0
	}
	return f
}

// PopBytes returns the byte slice ([]byte) value for a given key and then
// deletes it from the from the session data. The session data status will be
// set to Modified. The zero value for a slice (nil) is returned if the key does
// not exist or could not be type asserted to []byte.
func (s *Session) PopBytes(c SessionContext, key string) []byte {
	val := s.Pop(c, key)
	b, ok := val.([]byte)
	if !ok {
		return nil
	}
	return b
}

// PopTime returns the time.Time value for a given key and then deletes it from
// the session data. The session data status will be set to Modified. The zero
// value for a time.Time object is returned if the key does not exist or the
// value could not be type asserted to a time.Time.
func (s *Session) PopTime(c SessionContext, key string) time.Time {
	val := s.Pop(c, key)
	t, ok := val.(time.Time)
	if !ok {
		return time.Time{}
	}
	return t
}

// Token retrieves the current token or an empty string.
//
// This is used when unit testing and overriding LoadFromMiddleware
// or SaveFromMiddleware.
func (s *Session) Token(c SessionContext) string {
	sd := s.getSessionDataFromContext(c)

	sd.mu.Lock()
	defer sd.mu.Unlock()

	return sd.token
}

func (s *Session) getSessionDataFromContext(c SessionContext) *sessionData {
	sd, ok := c.Get(string(s.contextKey)).(*sessionData)
	if !ok {
		panic("scs: no session data in context")
	}
	return sd
}

func (sd *sessionData) encode() ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(sd)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (sd *sessionData) decode(b []byte) error {
	r := bytes.NewReader(b)
	return gob.NewDecoder(r).Decode(sd)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type contextKey string

var contextKeyID int

func generateContextKey() contextKey {
	contextKeyID = contextKeyID + 1
	return contextKey(fmt.Sprintf("session.%d", contextKeyID))
}
