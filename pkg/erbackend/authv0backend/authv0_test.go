package authv0backend

import (
	"github.com/function61/gokit/assert"
	"net/http"
	"testing"
)

func TestAuthorize(t *testing.T) {
	makeReq := func(authHeader string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		assert.Ok(t, err)

		req.Header.Set("Authorization", authHeader)

		return req
	}

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
