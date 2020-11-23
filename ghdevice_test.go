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

package ghdevice

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestFlow(t *testing.T) {
	const clientID = "cafe1234"
	const verificationURL = "https://example.com/login/device"
	const userCode = "DED-BEF"
	const deviceCode = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	type accessTokenResponse struct {
		statusCode int
		values     url.Values
	}
	tests := []struct {
		name        string
		scopes      []string
		responses   []accessTokenResponse
		want        string
		wantPrompts int
		wantErr     bool
	}{
		{
			name: "BasicSuccess",
			responses: []accessTokenResponse{
				{
					statusCode: http.StatusOK,
					values: url.Values{
						"access_token": {"xyzzy"},
						"token_type":   {"bearer"},
						"scope":        {""},
					},
				},
			},
			want:        "xyzzy",
			wantPrompts: 1,
		},
		{
			name:   "Scopes",
			scopes: []string{"repo", "user"},
			responses: []accessTokenResponse{
				{
					statusCode: http.StatusOK,
					values: url.Values{
						"access_token": {"xyzzy"},
						"token_type":   {"bearer"},
						"scope":        {"repo user"},
					},
				},
			},
			want:        "xyzzy",
			wantPrompts: 1,
		},
		{
			name: "Wait",
			responses: []accessTokenResponse{
				{
					statusCode: http.StatusBadRequest,
					values: url.Values{
						"error":             {"authorization_pending"},
						"error_description": {"authorization pending: waiting for user input"},
					},
				},
				{
					statusCode: http.StatusOK,
					values: url.Values{
						"access_token": {"xyzzy"},
						"token_type":   {"bearer"},
						"scope":        {""},
					},
				},
			},
			want:        "xyzzy",
			wantPrompts: 1,
		},
		{
			name: "UserRejected",
			responses: []accessTokenResponse{
				{
					statusCode: http.StatusBadRequest,
					values: url.Values{
						"error":             {"access_denied"},
						"error_description": {"User clicked cancel"},
					},
				},
			},
			wantErr:     true,
			wantPrompts: 1,
		},
		{
			name: "ExpiredToken",
			responses: []accessTokenResponse{
				{
					statusCode: http.StatusBadRequest,
					values: url.Values{
						"error":             {"expired_token"},
						"error_description": {"User took too long"},
					},
				},
				{
					statusCode: http.StatusOK,
					values: url.Values{
						"access_token": {"xyzzy"},
						"token_type":   {"bearer"},
						"scope":        {""},
					},
				},
			},
			want:        "xyzzy",
			wantPrompts: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()

			mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					t.Error("read device code body:", err)
				}
				values, err := url.ParseQuery(string(body))
				if err != nil {
					t.Error("parse device code body:", err)
				}
				wantValues := url.Values{
					"client_id": {clientID},
					"scope":     {strings.Join(test.scopes, " ")},
				}
				if diff := cmp.Diff(wantValues, values); diff != "" {
					t.Errorf("device code request (-want +got):\n%s", diff)
				}

				respBody := url.Values{
					"device_code":      {deviceCode},
					"user_code":        {userCode},
					"verification_uri": {verificationURL},
					"expires_in":       {"10"},
					"interval":         {"1"},
				}.Encode()
				w.Header().Set("Content-Type", formMediaType+"; charset=utf-8")
				w.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
				if _, err := io.WriteString(w, respBody); err != nil {
					t.Error("Write body:", err)
				}
			})

			var responseProgress struct {
				mu  sync.Mutex
				idx int
			}
			mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					t.Error("read access token body:", err)
				}
				values, err := url.ParseQuery(string(body))
				if err != nil {
					t.Error("parse access token body:", err)
				}
				wantValues := url.Values{
					"client_id":   {clientID},
					"device_code": {deviceCode},
					"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
				}
				if diff := cmp.Diff(wantValues, values); diff != "" {
					t.Errorf("access token request (-want +got):\n%s", diff)
				}

				responseProgress.mu.Lock()
				i := responseProgress.idx
				if i+1 < len(test.responses) {
					responseProgress.idx++
				}
				responseProgress.mu.Unlock()

				respBody := test.responses[i].values.Encode()
				w.Header().Set("Content-Type", formMediaType+"; charset=utf-8")
				w.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
				w.WriteHeader(test.responses[i].statusCode)
				if _, err := io.WriteString(w, respBody); err != nil {
					t.Error("Write body:", err)
				}
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			u, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			var prompts struct {
				mu    sync.Mutex
				count int
			}
			got, err := Flow(context.Background(), Options{
				ClientID:   clientID,
				GitHubURL:  u,
				HTTPClient: srv.Client(),
				Prompter: func(_ context.Context, got Prompt) error {
					prompts.mu.Lock()
					prompts.count++
					prompts.mu.Unlock()
					want := Prompt{
						UserCode:        userCode,
						VerificationURL: verificationURL,
					}
					if diff := cmp.Diff(want, got); diff != "" {
						t.Errorf("prompt (-want +got):\n%s", diff)
					}
					return nil
				},
				Scopes: test.scopes,
			})
			prompts.mu.Lock()
			finalPromptCount := prompts.count
			prompts.mu.Unlock()
			if finalPromptCount != test.wantPrompts {
				t.Errorf("%d prompt(s) delivered; want %d", finalPromptCount, test.wantPrompts)
			}
			if err != nil {
				t.Log("Flow:", err)
				if !test.wantErr {
					t.Fail()
				}
				return
			}
			if test.wantErr {
				t.Fatalf("Flow(...) = %q, <nil>; want _, <error>", got)
			}
			if got != test.want {
				t.Errorf("Flow(...) = %q, <nil>; want %q, <nil>", got, test.want)
			}
		})
	}
}

func TestPost(t *testing.T) {
	t.Run("Request", func(t *testing.T) {
		const userAgent = "me 1.2.3"
		var firstRequest sync.Once
		want := url.Values{
			"foo": {"bar"},
			"baz": {"quux"},
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			first := false
			firstRequest.Do(func() {
				first = true
				if r.Method != http.MethodPost {
					t.Errorf("method = %q; want %q", r.Method, http.MethodPost)
				}
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					t.Error("Read request body:", err)
					return
				}
				if got := r.Header.Get("Content-Type"); got != formMediaType {
					t.Errorf("Content-Type = %q; want %q", want, formMediaType)
				}
				if got := r.Header.Get("Accept"); got != formMediaType {
					t.Errorf("Accept = %q; want %q", want, formMediaType)
				}
				if got := r.Header.Get("User-Agent"); got != userAgent {
					t.Errorf("User-Agent = %q; want %q", got, userAgent)
				}
				got, err := url.ParseQuery(string(body))
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("body values (-want +got):\n%s", diff)
				}
			})
			if !first {
				const msg = "Multiple requests to endpoint"
				t.Error(msg)
				http.Error(w, msg, http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", formMediaType)
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		u, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, err = post(context.Background(), srv.Client(), userAgent, u, want)
		if err != nil {
			t.Error("post:", err)
		}
		received := true
		firstRequest.Do(func() { received = false })
		if !received {
			t.Error("Request never sent")
		}
	})

	t.Run("Request/Empty", func(t *testing.T) {
		var firstRequest sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			first := false
			firstRequest.Do(func() {
				first = true
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					t.Error("Read request body:", err)
					return
				}
				if got := r.Header.Get("User-Agent"); got == "" {
					t.Error("User-Agent empty")
				}
				if len(body) > 0 {
					t.Errorf("body = %q; want \"\"", body)
				}
			})
			if !first {
				const msg = "Multiple requests to endpoint"
				t.Error(msg)
				http.Error(w, msg, http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", formMediaType)
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		u, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, err = post(context.Background(), srv.Client(), "", u, nil)
		if err != nil {
			t.Error("post:", err)
		}
		received := true
		firstRequest.Do(func() { received = false })
		if !received {
			t.Error("Request never sent")
		}
	})

	t.Run("Response", func(t *testing.T) {
		tests := []struct {
			name        string
			statusCode  int
			contentType string
			content     string
			want        url.Values
			wantErr     func(error) bool
		}{
			{
				name:        "Empty",
				statusCode:  http.StatusOK,
				contentType: formMediaType + "; charset=utf-8",
				want:        url.Values{},
			},
			{
				name:        "Values",
				statusCode:  http.StatusOK,
				contentType: formMediaType + "; charset=utf-8",
				content:     "foo=bar&baz=quux",
				want: url.Values{
					"foo": {"bar"},
					"baz": {"quux"},
				},
			},
			{
				name:        "JSON",
				statusCode:  http.StatusOK,
				contentType: "application/json; charset=utf-8",
				content:     `{"foo":"bar"}`,
				wantErr: func(e error) bool {
					var oerr *oauthError
					return !errors.As(e, &oerr)
				},
			},
			{
				name:        "PlainError",
				statusCode:  http.StatusBadRequest,
				contentType: "text/plain; charset=utf-8",
				content:     "Bork bork",
				wantErr: func(e error) bool {
					var oerr *oauthError
					return !errors.As(e, &oerr)
				},
			},
			{
				name:        "AuthorizationPending",
				statusCode:  http.StatusBadRequest,
				contentType: formMediaType + "; charset=utf-8",
				content:     "error=authorization_pending&error_description=Waiting+for+input",
				wantErr: func(e error) bool {
					var oerr *oauthError
					if !errors.As(e, &oerr) {
						return false
					}
					return oerr.code == "authorization_pending" && oerr.description == "Waiting for input"
				},
			},
			{
				name:        "SlowDown",
				statusCode:  http.StatusBadRequest,
				contentType: formMediaType + "; charset=utf-8",
				content:     "error=slow_down&error_description=Too+many+requests&interval=10",
				wantErr: func(e error) bool {
					var oerr *oauthError
					if !errors.As(e, &oerr) {
						return false
					}
					return oerr.code == "slow_down" && oerr.description == "Too many requests" && oerr.interval == 10*time.Second
				},
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", test.contentType)
					w.Header().Set("Content-Length", strconv.Itoa(len(test.content)))
					w.WriteHeader(test.statusCode)
					if _, err := io.WriteString(w, test.content); err != nil {
						t.Errorf("Write response: %v", err)
					}
				}))
				t.Cleanup(srv.Close)

				u, err := url.Parse(srv.URL)
				if err != nil {
					t.Fatal(err)
				}
				got, err := post(context.Background(), srv.Client(), "", u, nil)
				if err != nil {
					t.Log("post:", err)
					if test.wantErr == nil || !test.wantErr(err) {
						t.Fail()
					}
					return
				}
				if test.wantErr != nil {
					t.Fatalf("post(...) = %v, <nil>; want _, <error>", got)
				}
				if diff := cmp.Diff(test.want, got); diff != "" {
					t.Errorf("post(...) = (-want +got):\n%s", diff)
				}
			})
		}
	})
}

func TestParseSeconds(t *testing.T) {
	tests := []struct {
		s               string
		defaultDuration time.Duration
		want            time.Duration
	}{
		{
			s:               "",
			defaultDuration: 0,
			want:            0,
		},
		{
			s:               "",
			defaultDuration: 5 * time.Second,
			want:            5 * time.Second,
		},
		{
			s:               "abc",
			defaultDuration: 5 * time.Second,
			want:            5 * time.Second,
		},
		{
			s:               "0",
			defaultDuration: 5 * time.Second,
			want:            5 * time.Second,
		},
		{
			s:               "60",
			defaultDuration: 5 * time.Second,
			want:            60 * time.Second,
		},
		{
			s:               "-60",
			defaultDuration: 5 * time.Second,
			want:            5 * time.Second,
		},
	}
	for _, test := range tests {
		got := parseSeconds(test.s, test.defaultDuration)
		if got != test.want {
			t.Errorf("parseSeconds(%q, %v) = %v; want %v", test.s, test.defaultDuration, got, test.want)
		}
	}
}
