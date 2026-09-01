package main

import (
	"net/http"
	"strings"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
)

// authFromRequest extracts the caller's Hatcheck auth from an incoming Deep
// request. Deep doesn't validate the session itself — that stays entirely
// Hatcheck's responsibility. A missing or malformed header fails fast here
// with a clear message; a present-but-invalid session surfaces naturally as
// a 401 the first time Deep calls Hatcheck with it.
func authFromRequest(req *http.Request) (hatcheckclient.AuthContext, bool) {
	authHeader := req.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return hatcheckclient.AuthContext{}, false
	}
	jwt := strings.TrimPrefix(authHeader, "Bearer ")
	if jwt == "" {
		return hatcheckclient.AuthContext{}, false
	}
	return hatcheckclient.AuthContext{
		SessionJWT:      jwt,
		CapabilityToken: req.Header.Get("X-Capability-Token"),
	}, true
}
