package github

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

func getAppID() (int64, error) {
	appIdStr := os.Getenv("GITHUB_APP_ID")
	appId, err := strconv.ParseInt(appIdStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return appId, nil
}

func getInstallID() (int64, error) {
	installIDStr := os.Getenv("GITHUB_INSTALL_ID")
	installID, err := strconv.ParseInt(installIDStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return installID, nil
}

func getPrivateKey() string {
	return os.Getenv("GITHUB_APP_PRIVATE_KEY")
}

func getTransport() (*ghinstallation.Transport, error) {
	appId, err := getAppID()
	if err != nil {
		return nil, fmt.Errorf("failed to get app id: %w", err)
	}
	installId, err := getInstallID()
	if err != nil {
		return nil, fmt.Errorf("failed to get install id: %w", err)
	}

	tr := http.DefaultTransport
	return ghinstallation.NewKeyFromFile(tr, appId, installId, getPrivateKey())
}

type bearerTokenTransport struct {
	itr  *ghinstallation.Transport
	http http.RoundTripper
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBodyClosed := false
	if req.Body != nil {
		defer func() {
			if !reqBodyClosed {
				req.Body.Close()
			}
		}()
	}

	token, err := t.itr.Token(req.Context())
	if err != nil {
		return nil, err
	}

	creq := cloneRequest(req) // per RoundTripper contract
	creq.Header.Set("Authorization", "Bearer "+token)

	if creq.Header.Get("Accept") == "" { // We only add an "Accept" header to avoid overwriting the expected behavior.
		creq.Header.Add("Accept", "application/vnd.github.v3+json")
	}
	reqBodyClosed = true // req.Body is assumed to be closed by the tr RoundTripper.
	resp, err := t.http.RoundTrip(creq)
	return resp, err
}

// cloneRequest returns a clone of the provided *http.Request.
// The clone is a shallow copy of the struct and its Header map.
func cloneRequest(r *http.Request) *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *r
	// deep copy of the Header
	r2.Header = make(http.Header, len(r.Header))
	for k, s := range r.Header {
		r2.Header[k] = append([]string(nil), s...)
	}
	return r2
}
