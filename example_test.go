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

package ghdevice_test

import (
	"context"
	"fmt"
	"os"

	"gg-scm.io/pkg/ghdevice"
	"github.com/google/go-github/v32/github"
	"golang.org/x/oauth2"
)

func ExampleFlow() {
	// Change this to identify you and/or your application to GitHub.
	// See https://docs.github.com/en/free-pro-team@latest/rest/overview/resources-in-the-rest-api#user-agent-required
	// for guidance.
	const userAgent = "myapplicationname"

	// Cancelling or adding a deadline to the context will interrupt the flow.
	ctx := context.Background()

	// Run the device flow, waiting for GitHub to return an access token
	// after the user has finished accepting the permissions.
	token, err := ghdevice.Flow(ctx, ghdevice.Options{
		UserAgent: userAgent,
		// Change this to your OAuth application client ID found in the
		// GitHub web interface.
		ClientID: "replacewithactualclientid",
		// Change these to use the appropriate OAuth scopes for
		// your application.
		Scopes: []string{"public_repo", "read:user"},

		// Prompter is a function to display login instructions to the user.
		Prompter: func(ctx context.Context, p ghdevice.Prompt) error {
			fmt.Fprintf(os.Stderr, "Visit %s in your browser and enter the code %s\n",
				p.VerificationURL, p.UserCode)
			fmt.Fprintf(os.Stderr, "Waiting...\n")
			return nil
		},
	})
	if err != nil {
		// Handle error. For example:
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	// Use the access token to make GitHub API requests.
	ts := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})
	ghClient := github.NewClient(oauth2.NewClient(ctx, ts))
	ghClient.UserAgent = userAgent
	repos, _, err := ghClient.Repositories.List(ctx, "", nil)
	if err != nil {
		// Handle error. For example:
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	_ = repos
}
