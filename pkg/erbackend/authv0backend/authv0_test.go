package authv0backend

import (
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/assert"
	"net/http"
	"net/http/httptest"
	"testing"
)

// integration test because this is super important we get it right
func TestIntegration(t *testing.T) {
	roundTrip := func(authHeader string) string {
		authMiddleware := New(erconfig.BackendOptsAuthV0{
			BearerToken: "correctToken",
		}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("welcome to admin section"))
		}))

		response := httptest.NewRecorder()

		authMiddleware.ServeHTTP(response, makeReq(authHeader))

		return response.Body.String()
	}

	assert.EqualString(t, roundTrip(""), "Unauthorized\n")
	assert.EqualString(t, roundTrip("Bearer WRONGToken"), "Unauthorized\n")
	assert.EqualString(t, roundTrip("Bearer correctToken"), "welcome to admin section")
}

func TestAuthorize(t *testing.T) {
	authorizeExpectDogs := func(r *http.Request) bool {
		return authorize(r, "DogsRBest")
	}

	// accept correct token, reject everything else
	assert.Assert(t, authorizeExpectDogs(makeReq("Bearer DogsRBest")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Bearer catsAreBest")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Bearer ")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Bearer")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Bear")))

	// accept user=(empty) pass=correct AND user=x pass=correct
	assert.Assert(t, authorizeExpectDogs(makeReq("Basic OkRvZ3NSQmVzdA==")))  // base64(":DogsRBest")
	assert.Assert(t, authorizeExpectDogs(makeReq("Basic eDpEb2dzUkJlc3Q=")))  // base64("x:DogsRBest")
	assert.Assert(t, !authorizeExpectDogs(makeReq("Basic eTpEb2dzUkJlc3Q="))) // base64("y:DogsRBest")
	assert.Assert(t, !authorizeExpectDogs(makeReq("Basic OmNhdHNBcmVCZXN0"))) // base64(":catsAreBest")
	assert.Assert(t, !authorizeExpectDogs(makeReq("Basic notBase64")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Basic ")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Basic")))
	assert.Assert(t, !authorizeExpectDogs(makeReq("Bas")))

	assert.Assert(t, !authorizeExpectDogs(makeReq("")))

	reqWithoutAuthorizationHeader, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.Ok(t, err)

	assert.Assert(t, !authorizeExpectDogs(reqWithoutAuthorizationHeader))
}

func makeReq(authHeader string) *http.Request {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		panic(err)
	}

	req.Header.Set("Authorization", authHeader)

	return req
}
