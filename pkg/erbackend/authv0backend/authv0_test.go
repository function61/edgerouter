package authv0backend

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/testing/assert"
)

// integration test because this is super important we get it right
func TestIntegration(t *testing.T) {
	roundTrip := func(authHeader string) string {
		authMiddleware := New(erconfig.BackendOptsAuthV0{
			BearerToken: "correctToken",
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(authorizationHeaderKey) != "" {
				panic("got authorization header in origin")
			}

			_, _ = w.Write([]byte("welcome to admin section"))
		}))

		response := httptest.NewRecorder()

		authMiddleware.ServeHTTP(response, makeReq(authHeader))

		return response.Body.String()
	}

	assert.Equal(t, roundTrip(""), "Unauthorized\n")
	assert.Equal(t, roundTrip("Bearer WRONGToken"), "Unauthorized\n")
	assert.Equal(t, roundTrip("Bearer correctToken"), "welcome to admin section")
}

func TestAuthorize(t *testing.T) {
	authorizeExpectDogs := func(r *http.Request) bool {
		return authorize(r, "DogsRBest")
	}

	// accept correct token, reject everything else
	assert.Equal(t, authorizeExpectDogs(makeReq("Bearer DogsRBest")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Bearer catsAreBest")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Bearer ")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Bearer")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Bear")), true)

	// accept user=(empty) pass=correct AND user=x pass=correct
	assert.Equal(t, authorizeExpectDogs(makeReq("Basic OkRvZ3NSQmVzdA==")), true)  // base64(":DogsRBest")
	assert.Equal(t, authorizeExpectDogs(makeReq("Basic eDpEb2dzUkJlc3Q=")), true)  // base64("x:DogsRBest")
	assert.Equal(t, !authorizeExpectDogs(makeReq("Basic eTpEb2dzUkJlc3Q=")), true) // base64("y:DogsRBest")
	assert.Equal(t, !authorizeExpectDogs(makeReq("Basic OmNhdHNBcmVCZXN0")), true) // base64(":catsAreBest")
	assert.Equal(t, !authorizeExpectDogs(makeReq("Basic notBase64")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Basic ")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Basic")), true)
	assert.Equal(t, !authorizeExpectDogs(makeReq("Bas")), true)

	assert.Equal(t, !authorizeExpectDogs(makeReq("")), true)

	reqWithoutAuthorizationHeader, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.Ok(t, err)

	assert.Equal(t, !authorizeExpectDogs(reqWithoutAuthorizationHeader), true)
}

func makeReq(authHeader string) *http.Request {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		panic(err)
	}

	req.Header.Set(authorizationHeaderKey, authHeader)

	return req
}
