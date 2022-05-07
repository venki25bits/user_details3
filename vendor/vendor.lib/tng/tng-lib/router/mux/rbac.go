package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	common "vendor.lib/tng/tng-lib/http"

	"github.com/gorilla/context"
	"github.com/pkg/errors"
)

const (
	ForbiddenMSG    = "The request is forbidden"
	UnauthorizedMSG = "The request is not authorized"
	userKey         = "User"
)

var (
	ErrNoToken                   = errors.New("no token")
	ErrUnsupportedAuthentication = errors.New("unsupported authentication")
	ErrMalformedToken            = errors.New("malformed token")
)

type Config struct {
	LoginService  common.Config
	MemberWrapper common.Config
}

type RBAC struct {
	loginService  *common.Client
	memberWrapper *common.Client
}

type User struct {
	Authentication Authentication `json:"-"`
	Info           []Info         `json:"userInfo"`
	ExpTime        int64          `json:"expTime"`
	Roles          Strings        `json:"chpRoles"`
	BusinessLines  Strings        `json:"-"`
	BusinessUnits  Ints           `json:"-"`
}

func GetUser(r *http.Request) User {
	if user, ok := context.Get(r, userKey).(User); ok {
		return user
	}
	return User{}
}

func (u *User) HasBusinessUnitAndLine(unit int, line string) bool {
	return u.BusinessUnits.Contains(unit) && u.BusinessLines.ContainsIgnoreCase(line)
}

type Strings []string

func (b Strings) Contains(s string) bool {
	for _, a := range b {
		if a == s {
			return true
		}
	}
	return false
}

func (b Strings) ContainsIgnoreCase(s string) bool {
	for _, a := range b {
		if strings.ToUpper(a) == strings.ToUpper(s) {
			return true
		}
	}
	return false
}

type Ints []int

func (b Ints) Strings() Strings {
	strs := make(Strings, 0)
	for _, a := range b {
		strs = append(strs, strconv.Itoa(a))
	}
	return strs
}

func (b Ints) Contains(s int) bool {
	for _, a := range b {
		if a == s {
			return true
		}
	}
	return false
}

type Authentication struct {
	Type         string
	Credenteials string
}

type Info struct {
	Cn             string   `json:"cn"`
	SAMAccountName string   `json:"sAMAccountName"`
	Mail           string   `json:"mail"`
	MemberOf       []string `json:"memberOf"`
}

type memberWrapperResponse struct {
	RefData []refData `json:"refData"`
}

type refData struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type errResonse struct {
	Code    int    `json:"-"`
	Message string `json:"message"`
}

type UnauthorizedResponse struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewAuthentication(header string) Authentication {
	split := strings.Split(header, " ")
	if len(split) == 2 {
		return Authentication{Type: split[0], Credenteials: split[1]}
	} else if len(split) == 1 {
		return Authentication{Credenteials: split[0]}
	}
	return Authentication{}
}

func (a Authentication) String() string {
	if a.Type != "" {
		return fmt.Sprintf("%s %s", a.Type, a.Credenteials)
	}
	return a.Credenteials
}

func NewRBAC(cfg Config) (*RBAC, error) {
	loginService, err := common.New(cfg.LoginService)
	if err != nil {
		return nil, err
	}
	memberWrapper, _ := common.New(cfg.MemberWrapper)
	if err != nil {
		return nil, err
	}
	return &RBAC{loginService: loginService, memberWrapper: memberWrapper}, nil
}

func (rbac *RBAC) Middleware(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authentication, err := getAuthentication(r)
		if err != nil {
			RespondWithJSON(w, http.StatusUnauthorized, UnauthorizedResponse{
				Message: UnauthorizedMSG,
				Error:   err.Error(),
			})
			return
		}

		var wg sync.WaitGroup
		done := make(chan interface{})
		errChan := make(chan errResonse)
		defer close(errChan)
		defer close(done)

		user, code, err := rbac.getUser(authentication)
		switch code {
		case http.StatusOK:
			// Do nothing
		case http.StatusUnauthorized:
			RespondWithJSON(w, http.StatusUnauthorized, UnauthorizedResponse{
				Message: UnauthorizedMSG,
				Error:   err.Error(),
			})
			return
		default:
			RespondWithError(w, http.StatusBadGateway, err)
			return
		}

		rbac.getBusinessLines(&user, &wg, errChan)
		rbac.getBusinessUnits(&user, &wg, errChan)

		errs := make([]errResonse, 0)
		go func() {
			for {
				select {
				case err := <-errChan:
					errs = append(errs, err)
				case <-done:
					return
				}
			}
		}()

		wg.Wait()
		done <- nil

		if len(errs) > 0 {
			err := errs[0]
			if common.IsServerError(err.Code) {
				RespondWithError(w, http.StatusBadGateway, errors.New(err.Message))
			} else if err.Code > 0 {
				RespondWithJSON(w, http.StatusUnauthorized, UnauthorizedResponse{
					Message: UnauthorizedMSG,
					Error:   err.Message,
				})
			} else {
				RespondWithError(w, http.StatusInternalServerError, errors.New(err.Message))
			}
			return
		}

		context.Set(r, userKey, user)
		next(w, r)
	})
}

func (rbac *RBAC) getUser(authentication Authentication) (User, int, error) {
	var user User
	request := struct {
		Token string `json:"token"`
	}{
		Token: authentication.Credenteials,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return user, 0, err
	}
	resp, err := rbac.loginService.Post(&url.URL{Path: "/token"}, http.Header{"Authorization": []string{authentication.String()}}, bytes.NewReader(body))
	if err != nil {
		return user, 0, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		err = json.Unmarshal(resp.Body, &user)
		if err != nil {
			return user, resp.StatusCode, err
		}
		user.Authentication = authentication
		return user, resp.StatusCode, nil
	default:
		response := struct {
			Error string `json:"error"`
		}{}
		if err := json.Unmarshal(resp.Body, &response); err != nil {
			return user, resp.StatusCode, err
		}
		return user, resp.StatusCode, errors.New(response.Error)
	}
}

func (rbac *RBAC) getBusinessLines(user *User, wg *sync.WaitGroup, ch chan<- errResonse) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		errResponse := errResonse{}
		resp, err := rbac.memberWrapper.Get(&url.URL{Path: "/v2/umvrefdata/businessline"}, http.Header{"Authorization": []string{user.Authentication.String()}})
		if err != nil {
			errResponse.Message = err.Error()
			ch <- errResponse
			return
		}
		errResponse.Code = resp.StatusCode

		switch resp.StatusCode {
		case http.StatusOK:
			var response memberWrapperResponse
			if err := json.Unmarshal(resp.Body, &response); err != nil {
				errResponse.Message = err.Error()
				ch <- errResponse
			} else {
				for i := range response.RefData {
					user.BusinessLines = append(user.BusinessLines, response.RefData[i].Name)
				}
			}
		default:
			if err := json.Unmarshal(resp.Body, &errResponse); err != nil {
				errResponse.Message = err.Error()
			}
			ch <- errResponse
		}
	}()
}

func (rbac *RBAC) getBusinessUnits(user *User, wg *sync.WaitGroup, ch chan<- errResonse) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		errResponse := errResonse{}
		resp, err := rbac.memberWrapper.Get(&url.URL{Path: "/v2/umvrefdata/businessunit"}, http.Header{"Authorization": []string{user.Authentication.String()}})
		if err != nil {
			errResponse.Message = err.Error()
			ch <- errResponse
			return
		}
		errResponse.Code = resp.StatusCode

		switch resp.StatusCode {
		case http.StatusOK:
			var response memberWrapperResponse
			if err := json.Unmarshal(resp.Body, &response); err != nil {
				errResponse.Message = err.Error()
				ch <- errResponse
			} else {
				for i := range response.RefData {
					businessUnit, err := strconv.Atoi(response.RefData[i].Code)
					if err != nil {
						errResponse.Message = err.Error()
						ch <- errResponse
						return
					}
					user.BusinessUnits = append(user.BusinessUnits, businessUnit)
				}
			}
		default:
			if err := json.Unmarshal(resp.Body, &errResponse); err != nil {
				errResponse.Message = err.Error()
			}
			ch <- errResponse
		}
	}()
}

func getAuthentication(r *http.Request) (Authentication, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return Authentication{}, ErrNoToken
	}

	authentication := NewAuthentication(header)
	switch authentication.Type {
	case "Bearer":
		return authentication, nil
	default:
		return Authentication{}, ErrUnsupportedAuthentication
	}
}
