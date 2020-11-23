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

package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"

	"gg-scm.io/pkg/ghdevice"
	"golang.org/x/sys/unix"
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), "usage: ghtoken [options]\n\n")
		flag.PrintDefaults()
	}
	opts := ghdevice.Options{
		UserAgent: "gg-scm.io/pkg/ghdevice/cmd/ghtoken",
		Prompter: func(ctx context.Context, p ghdevice.Prompt) error {
			_, err := fmt.Fprintf(os.Stderr, "Go to %s and enter code %s\n", p.VerificationURL, p.UserCode)
			return err
		},
		GitHubURL: &url.URL{
			Scheme: "https",
			Host:   "github.com",
		},
	}
	flag.StringVar(&opts.ClientID, "client-id", "52f432109560ca1046af", "OAuth application client `ID`")
	flag.Var((*stringSlice)(&opts.Scopes), "scope", "OAuth `scope`(s) to request. May be specified more than once or comma-separated.")
	flag.Var(urlFlag{&opts.GitHubURL}, "url", "base `URL` for GitHub")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, unix.SIGTERM, unix.SIGINT)
	signal.Ignore(unix.SIGPIPE)
	go func() {
		<-signals
		cancel()
	}()

	token, err := ghdevice.Flow(ctx, opts)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghtoken:", err)
		os.Exit(1)
	}
	_, err = fmt.Println(token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghtoken:", err)
		os.Exit(1)
	}
}

type urlFlag struct {
	urlPtr **url.URL
}

func (uf urlFlag) String() string {
	if uf.urlPtr == nil {
		return ""
	}
	u := *uf.urlPtr
	if u == nil {
		return ""
	}
	return u.String()
}

func (uf urlFlag) Set(s string) error {
	var err error
	*uf.urlPtr, err = url.Parse(s)
	return err
}

type stringSlice []string

func (ss stringSlice) String() string {
	return strings.Join(ss, ",")
}

func (ss *stringSlice) Set(s string) error {
	for _, elem := range strings.Split(s, ",") {
		elem = strings.TrimSpace(elem)
		if elem != "" {
			*ss = append(*ss, elem)
		}
	}
	return nil
}
