// Package auth provides functions related to game authentication.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/telecter/cmd-launcher/internal/network"
)

const scope = "XboxLive.signin offline_access"

var ClientID string      // Client ID for the Azure application
var RedirectURI *url.URL // Redirect URI for the OAuth2 authorization code grant

// A Session holds the necessary information to start Minecraft authenticated.
type Session struct {
	UUID        string
	Username    string
	AccessToken string
}

// AuthCodeURL returns an authorization code URL for the user to navigate to
//
// Used for the OAuth2 authorization code grant
func AuthCodeURL() *url.URL {
	query := url.Values{
		"client_id":     {ClientID},
		"response_type": {"code"},
		"redirect_uri":  {RedirectURI.String()},
		"scope":         {scope},
		"response_mode": {"query"},
	}
	uri, _ := url.Parse("https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize")
	uri.RawQuery = query.Encode()
	return uri
}

// A deviceCodeResponse contains information about device codes to be entered by the user to complete authentication, when they expire, and how often they should be polled for.
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

// FetchDeviceCode returns a device code for the user to input to authenticate
//
// Used for the OAuth2 device code grant
func FetchDeviceCode() (deviceCodeResponse, error) {
	params := url.Values{
		"client_id": {ClientID},
		"scope":     {scope},
	}
	resp, err := http.Post("https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode", "application/x-www-form-urlencoded", strings.NewReader(params.Encode()))
	if err != nil {
		return deviceCodeResponse{}, err
	}
	defer resp.Body.Close()
	if err := network.CheckResponse(resp); err != nil {
		return deviceCodeResponse{}, err
	}
	var data deviceCodeResponse
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &data); err != nil {
		return deviceCodeResponse{}, err
	}

	return data, nil
}

type msaResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	// error response

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func authenticateMSA(payload url.Values) (msaResponse, error) {
	var data msaResponse
	resp, err := http.Post("https://login.microsoftonline.com/consumers/oauth2/v2.0/token", "application/x-www-form-urlencoded", strings.NewReader(payload.Encode()))
	if err != nil {
		return msaResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &data); err != nil {
		return msaResponse{}, err
	}
	return data, nil
}

type xblResponse struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		Xui []struct {
			Uhs string `string:"uhs"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
	IssueInstant time.Time `json:"IssueInstant"`
	NotAfter     time.Time `json:"NotAfter"`
}

func authenticateXBL(msaAccessToken string) (xblResponse, error) {
	type properties struct {
		AuthMethod string `json:"AuthMethod"`
		SiteName   string `json:"SiteName"`
		RpsTicket  string `json:"RpsTicket"`
	}
	type request struct {
		Properties   properties `json:"Properties"`
		TokenType    string     `json:"TokenType"`
		RelyingParty string     `json:"RelyingParty"`
	}
	req, _ := json.Marshal(
		request{
			Properties: properties{
				AuthMethod: "RPS",
				SiteName:   "user.auth.xboxlive.com",
				RpsTicket:  "d=" + msaAccessToken,
			},
			TokenType:    "JWT",
			RelyingParty: "http://auth.xboxlive.com",
		})
	resp, err := http.Post("https://user.auth.xboxlive.com/user/authenticate", "application/json", strings.NewReader(string(req)))
	if err != nil {
		return xblResponse{}, err
	}
	if err := network.CheckResponse(resp); err != nil {
		return xblResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data xblResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return xblResponse{}, err
	}
	return data, nil
}

type xstsResponse struct {
	Token        string    `json:"Token"`
	IssueInstant time.Time `json:"IssueInstant"`
	NotAfter     time.Time `json:"NotAfter"`
	// error response

	XErr int `json:"XErr"`
}

func authenticateXSTS(xblToken string) (xstsResponse, error) {
	type properties struct {
		SandboxID  string   `json:"SandboxId"`
		UserTokens []string `json:"UserTokens"`
	}
	type request struct {
		Properties   properties `json:"Properties"`
		RelyingParty string     `json:"RelyingParty"`
		TokenType    string     `json:"TokenType"`
	}

	req, _ := json.Marshal(request{
		Properties: properties{
			SandboxID:  "RETAIL",
			UserTokens: []string{xblToken},
		},
		RelyingParty: "rp://api.minecraftservices.com/",
		TokenType:    "JWT",
	})
	resp, err := http.Post("https://xsts.auth.xboxlive.com/xsts/authorize", "application/json", strings.NewReader(string(req)))
	if err != nil {
		return xstsResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data xstsResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return xstsResponse{}, err
	}

	if err := network.CheckResponse(resp); err != nil {
		if data.XErr != 0 {
			return xstsResponse{}, fmt.Errorf("got error %d", data.XErr)
		}
		return xstsResponse{}, err
	}
	return data, nil
}

type minecraftResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}
type minecraftProfile struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	// error response

	Error        string `json:"error"`
	ErrorMessage string `json:"errorMessage"`
}

func authenticateMinecraft(xstsToken string, userhash string) (minecraftResponse, minecraftProfile, error) {
	type request struct {
		IdentityToken string `json:"identityToken"`
	}

	reqBody, _ := json.Marshal(request{
		IdentityToken: fmt.Sprintf("XBL3.0 x=%s;%s", userhash, xstsToken),
	})
	resp, err := http.Post("https://api.minecraftservices.com/authentication/login_with_xbox", "application/json", strings.NewReader(string(reqBody)))
	if err != nil {
		return minecraftResponse{}, minecraftProfile{}, err
	}
	if err := network.CheckResponse(resp); err != nil {
		return minecraftResponse{}, minecraftProfile{}, err
	}
	var data minecraftResponse
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &data); err != nil {
		return minecraftResponse{}, minecraftProfile{}, err
	}

	req, _ := http.NewRequest("GET", "https://api.minecraftservices.com/minecraft/profile", nil)
	req.Header.Add("Authorization", "Bearer "+data.AccessToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return minecraftResponse{}, minecraftProfile{}, err
	}
	var profile minecraftProfile
	body, _ = io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &profile); err != nil {
		return minecraftResponse{}, minecraftProfile{}, err
	}
	if err := network.CheckResponse(resp); err != nil {
		if profile.Error != "" && profile.ErrorMessage != "" {
			return minecraftResponse{}, minecraftProfile{}, errors.New(profile.Error)
		}
		return minecraftResponse{}, minecraftProfile{}, err
	}
	return data, profile, nil
}

var ErrNoAccount = errors.New("no account found")

// refresh makes the account's tokens valid, refreshing whichever of them have expired.
func (account *Account) refresh() error {
	if account.MSA.RefreshToken == "" {
		return ErrNoAccount
	}

	if !account.MSA.isValid() {
		resp, err := authenticateMSA(url.Values{
			"client_id":     {ClientID},
			"scope":         {scope},
			"grant_type":    {"refresh_token"},
			"refresh_token": {account.MSA.RefreshToken},
		})
		if err != nil {
			return fmt.Errorf("authenticate with MSA: %w", err)
		}
		if resp.Error != "" {
			return fmt.Errorf("authenticate with MSA: %s", resp.Error)
		}
		account.MSA.write(resp)
	}
	if !account.XBL.isValid() {
		resp, err := authenticateXBL(account.MSA.AccessToken)
		if err != nil {
			return fmt.Errorf("authenticate with Xbox Live: %w", err)
		}
		if len(resp.DisplayClaims.Xui) == 0 || resp.Token == "" {
			return fmt.Errorf("authenticate with Xbox Live: no user hash was returned")
		}
		account.XBL.write(resp)
	}
	if !account.XSTS.isValid() {
		resp, err := authenticateXSTS(account.XBL.Token)
		if err != nil {
			return fmt.Errorf("authenticate with XSTS: %w", err)
		}
		account.XSTS.write(resp)
	}
	if !account.Minecraft.isValid() {
		resp, profile, err := authenticateMinecraft(account.XSTS.Token, account.XBL.Userhash)
		if err != nil {
			return fmt.Errorf("authenticate with Minecraft: %w", err)
		}
		account.Minecraft.write(resp, profile)
	}
	return nil
}

// session returns the launch session for the account.
func (account *Account) session() Session {
	return Session{
		Username:    account.Minecraft.Username,
		UUID:        account.Minecraft.UUID,
		AccessToken: account.Minecraft.AccessToken,
	}
}

// Authenticate authenticates the active account with all necessary endpoints, or cached data if
// available and returns a Session.
//
// A local account needs no authentication and is returned as an offline session.
func Authenticate() (Session, error) {
	account := Store.Active()
	if account == nil {
		return Session{}, ErrNoAccount
	}
	if account.IsOffline() {
		if account.Username == "" {
			return Session{}, ErrNoAccount
		}
		return account.OfflineSession(), nil
	}
	if account.MSA.RefreshToken == "" {
		return Session{}, ErrNoAccount
	}
	if err := account.refresh(); err != nil {
		return Session{}, err
	}
	if err := Store.WriteToCache(); err != nil {
		return Session{}, fmt.Errorf("write auth store: %w", err)
	}
	return account.session(), nil
}

// finishLogin turns a freshly obtained MSA response into a stored account and returns its session.
//
// A login always adds an account. Re-authenticating an account that is already known would leave two
// entries behind, so duplicates are resolved by Dedupe.
func finishLogin(resp msaResponse) (Session, error) {
	account := Store.Add()
	account.Type = AccountMicrosoft
	account.MSA.write(resp)

	session, err := Authenticate()
	if err != nil {
		return Session{}, err
	}
	Store.Dedupe()
	if err := Store.WriteToCache(); err != nil {
		return Session{}, fmt.Errorf("write auth store: %w", err)
	}
	return session, nil
}

// AuthenticateWithRedirect authenticates using the OAuth2 Code flow.
//
// success is a string to be shown to the user upon successful authentication.
// fail is shown if an authentication error occurs.
//
// This function blocks until a response has been received on the local authentication server.
func AuthenticateWithRedirect(success, fail string) (Session, error) {
	var code string
	var callbackErr error

	port := RedirectURI.Port()
	if port == "" {
		return Session{}, fmt.Errorf("redirect URL must have port specified")
	}

	// A dedicated mux is required: the default one panics when the same pattern is registered twice,
	// and the shell can run this flow more than once in a single process.
	mux := http.NewServeMux()
	server := &http.Server{Addr: ":" + port, Handler: mux}
	mux.HandleFunc(RedirectURI.Path, func(w http.ResponseWriter, req *http.Request) {
		params := req.URL.Query()
		if params.Get("error") != "" {
			fmt.Fprint(w, fail+"\n"+params.Get("error_description"))
			callbackErr = fmt.Errorf("got error: %s", params.Get("error_description"))
		} else {
			fmt.Fprint(w, success)
		}
		code = params.Get("code")
		go server.Shutdown(context.Background())
	})

	// ListenAndServe always returns an error, which is expected once the handler shuts the server down.
	_ = server.ListenAndServe()
	if callbackErr != nil {
		return Session{}, callbackErr
	}
	if code == "" {
		return Session{}, fmt.Errorf("no authorization code was returned")
	}

	resp, err := authenticateMSA(url.Values{
		"client_id":    {ClientID},
		"scope":        {scope},
		"redirect_uri": {RedirectURI.String()},
		"grant_type":   {"authorization_code"},
		"code":         {code},
	})
	if err != nil {
		return Session{}, fmt.Errorf("authenticate with MSA: %w", err)
	}
	if resp.Error != "" {
		return Session{}, fmt.Errorf("authenticate with MSA: %s", resp.Error)
	}

	return finishLogin(resp)
}

// AuthenticateWithCode authenticates with a device code.
//
// This function blocks until the user has been authenticated, or another error has occurred.
func AuthenticateWithCode(codeResp deviceCodeResponse) (Session, error) {
	for {
		resp, err := authenticateMSA(url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {ClientID},
			"device_code": {codeResp.DeviceCode},
		})
		if err != nil {
			return Session{}, fmt.Errorf("authenticate with MSA: %w", err)
		}

		switch resp.Error {
		case "authorization_pending":
			time.Sleep(time.Second * time.Duration(codeResp.Interval))
			continue
		case "authorization_declined":
			return Session{}, fmt.Errorf("authorization was declined")
		case "":
			return finishLogin(resp)
		default:
			return Session{}, fmt.Errorf("authenticate with MSA: %s", resp.Error)
		}
	}
}
