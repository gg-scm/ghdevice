// Copyright 2020 The gg Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// Package ghdevice provides a function to obtain a GitHub access token using
// the OAuth device flow. This is used to authorize command-line interfaces or
// other non-browser-based applications to access GitHub on behalf of a GitHub
// user.
// See https://docs.github.com/en/developers/apps/authorizing-oauth-apps#device-flow
// for more details.
//
// You will need to register your application with GitHub to use this flow.
// See https://docs.github.com/en/free-pro-team@latest/developers/apps/creating-an-oauth-app
// for instructions on how to create an OAuth application.
package ghdevice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Options holds arguments for Flow.
type Options struct {
	// ClientID is the GitHub OAuth application client ID. It is required.
	// See https://docs.github.com/en/free-pro-team@latest/developers/apps/creating-an-oauth-app
	// for instructions on how to create an OAuth application.
	ClientID string

	// Prompter is a function called to inform the user of the URL to visit and
	// enter in a code. It may be called more than once if the user doesn't enter
	// the code in a timely manner. If the function returns an error, Flow returns
	// the error, wrapped with additional detail.
	Prompter func(context.Context, Prompt) error

	// Scopes specifies the OAuth scopes to request for the token.
	// See https://docs.github.com/en/free-pro-team@latest/developers/apps/scopes-for-oauth-apps
	// for scope names. If empty, then only public information can be accessed.
	Scopes []string

	// HTTPClient specifies the client to make HTTP requests from.
	// If it is nil, http.DefaultClient is used.
	HTTPClient *http.Client

	// GitHubURL is the root URL used for the login endpoints.
	// If it is nil, defaults to "https://github.com".
	GitHubURL *url.URL

	// UserAgent is the User-Agent header sent to the GitHub API.
	// If it is empty, a generic header is used.
	// See https://docs.github.com/en/free-pro-team@latest/rest/overview/resources-in-the-rest-api#user-agent-required
	// for guidance on acceptable values.
	UserAgent string
}

func (opts Options) client() *http.Client {
	if opts.HTTPClient == nil {
		return http.DefaultClient
	}
	return opts.HTTPClient
}

func (opts Options) url(path string) *url.URL {
	if opts.GitHubURL == nil {
		return &url.URL{
			Scheme: "https",
			Host:   "github.com",
			Path:   path,
		}
	}
	u := new(url.URL)
	*u = *opts.GitHubURL
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	return u
}

// Prompt holds the information shown to prompt the user to enter a code in
// their web browser.
type Prompt struct {
	// VerificationURL is the URL of the webpage the user should enter their code in.
	VerificationURL string
	// UserCode is the code the user should enter into the GitHub webpage.
	UserCode string
}

// Flow runs the GitHub device flow, waiting until the user has authorized the
// application to access their GitHub account, the Context is cancelled, the
// Context's deadline is reached, or an unrecoverable error occurs. On success,
// Flow returns a GitHub Bearer access token.
//
// Flow calls opts.Prompter with a URL and code that need to be presented to the
// user for them to authorize the application. It is up to the caller to present
// this information in a suitable manner, like printing to the console. If the
// user does not complete the GitHub prompt in time, then Flow may call
// opts.Prompter again to present a new URL and/or code. If opts.Prompter
// returns an error, then Flow returns the error wrapped with additional detail.
func Flow(ctx context.Context, opts Options) (string, error) {
	if opts.ClientID == "" {
		return "", fmt.Errorf("github authorization flow: client ID not provided")
	}
	if opts.Prompter == nil {
		return "", fmt.Errorf("github authorization flow: prompter not provided")
	}

	for {
		// Obtain device code.
		codeData, err := post(ctx, opts.client(), opts.UserAgent, opts.url("/login/device/code"), url.Values{
			"client_id": {opts.ClientID},
			"scope":     {strings.Join(opts.Scopes, " ")},
		})
		if err != nil {
			return "", fmt.Errorf("github authorization flow: get device code: %w", err)
		}

		// Set up Context for the user to poll.
		expiry := parseSeconds(codeData.Get("expires_in"), 15*time.Minute)
		pollCtx, cancelPoll := context.WithDeadline(ctx, time.Now().Add(expiry))

		// Present the user with the URL and user code.
		err = opts.Prompter(pollCtx, Prompt{
			VerificationURL: codeData.Get("verification_uri"),
			UserCode:        codeData.Get("user_code"),
		})
		if err != nil {
			cancelPoll()
			return "", fmt.Errorf("github authorization flow: prompt: %w", err)
		}

		// Wait for GitHub to reply with the access token.
		interval := parseSeconds(codeData.Get("interval"), 5*time.Second)
		token, err := waitForAccessToken(pollCtx, opts, codeData.Get("device_code"), interval)
		cancelPoll()
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("github authorization flow: %w", err)
		}
		select {
		case <-ctx.Done():
			// If the overall Context has been cancelled or its deadline exceeded, then
			// return that error.
			return "", fmt.Errorf("github authorization flow: %w", ctx.Err())
		default:
			// Otherwise, we need to prompt the user again.
		}
	}
}

func waitForAccessToken(ctx context.Context, opts Options, deviceCode string, interval time.Duration) (string, error) {
	params := url.Values{
		"client_id":   {opts.ClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	ticker := time.NewTicker(interval)
	defer func() {
		// The ticker can be reassigned, so evaluate ticker when defer is called.
		ticker.Stop()
	}()
	for {
		select {
		case <-ticker.C:
			resp, err := post(ctx, opts.client(), opts.UserAgent, opts.url("/login/oauth/access_token"), params)
			if oauthErr := (*oauthError)(nil); errors.As(err, &oauthErr) {
				switch oauthErr.code {
				case "authorization_pending":
					// User has not completed input.
					continue
				case "slow_down":
					// Server requesting backoff.
					if oauthErr.interval > 0 {
						ticker.Stop()
						ticker = time.NewTicker(oauthErr.interval)
					}
					continue
				case "expired_token":
					// User took too long, but we didn't hit client-side deadline.
					// Need to re-prompt.
					return "", fmt.Errorf("get access token: %w", context.DeadlineExceeded)
				}

			}
			if err != nil {
				return "", fmt.Errorf("get access token: %w", err)
			}
			token := resp.Get("access_token")
			if token == "" {
				return "", fmt.Errorf("get access token: server did not return an access token")
			}
			return token, nil
		case <-ctx.Done():
			return "", fmt.Errorf("get access token: %w", ctx.Err())
		}
	}
}

const formMediaType = "application/x-www-form-urlencoded"

// post makes a POST request and parses its response.
// We use this over golang.org/x/oauth2 because our needs are simpler and
// we can avoid the dependency.
func post(ctx context.Context, client *http.Client, userAgent string, u *url.URL, form url.Values) (url.Values, error) {
	const contentType = "Content-Type"
	formString := form.Encode()
	req := (&http.Request{
		Method: http.MethodPost,
		URL:    u,
		GetBody: func() (io.ReadCloser, error) {
			return ioutil.NopCloser(strings.NewReader(formString)), nil
		},
		ContentLength: int64(len(formString)),
		Header: http.Header{
			contentType: {formMediaType},
			"Accept":    {formMediaType},
		},
	}).WithContext(ctx)
	req.Body, _ = req.GetBody()
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %v: %w", u, err)
	}
	defer resp.Body.Close()
	var respValues url.Values
	var readErr error
	if mtype, _, err := mime.ParseMediaType(resp.Header.Get(contentType)); err != nil {
		readErr = fmt.Errorf("post %v: invalid Content-Type: %w", u, err)
	} else if mtype != formMediaType {
		readErr = fmt.Errorf("post %v: Content-Type is %q instead of form", u, mtype)
	} else if data, err := ioutil.ReadAll(resp.Body); err != nil {
		readErr = fmt.Errorf("post %v: read response: %w", u, err)
	} else if respValues, err = url.ParseQuery(string(data)); err != nil {
		readErr = fmt.Errorf("post %v: read response: %w", u, err)
	}

	if resp.StatusCode != http.StatusOK || respValues.Get("error") != "" {
		errorObject := newOAuthError(respValues)
		if readErr != nil || errorObject == nil {
			return nil, fmt.Errorf("post %v: http %s", u, resp.Status)
		}
		return nil, fmt.Errorf("post %v: %w", u, errorObject)
	}
	if readErr != nil {
		return nil, readErr
	}
	return respValues, nil
}

type oauthError struct {
	code        string
	description string
	interval    time.Duration
}

func newOAuthError(v url.Values) *oauthError {
	e := &oauthError{
		code:        v.Get("error"),
		description: v.Get("error_description"),
	}
	if e.code == "" {
		return nil
	}
	e.interval = parseSeconds(v.Get("interval"), 0)
	return e
}

func (e *oauthError) Error() string {
	if e.description == "" {
		return "oauth " + e.code
	}
	return e.description
}

func parseSeconds(s string, defaultDuration time.Duration) time.Duration {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 0 {
		return defaultDuration
	}
	return time.Duration(n) * time.Second
}
