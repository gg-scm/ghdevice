# `gg-scm.io/pkg/ghdevice`

[![Reference](https://pkg.go.dev/badge/gg-scm.io/pkg/ghdevice?tab=doc)](https://pkg.go.dev/gg-scm.io/pkg/ghdevice?tab=doc)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-v2.0%20adopted-ff69b4.svg)](CODE_OF_CONDUCT.md)

`gg-scm.io/pkg/ghdevice` is a Go library to obtain a GitHub access token
using the [OAuth device flow][]. This is used to authorize command-line interfaces or
other non-browser-based applications to access GitHub on behalf of a GitHub
user. It was developed for [gg][], but this library is useful for any program
that wishes to interact with GitHub.

This repository also provides a small CLI, `ghtoken`, to demonstrate the OAuth
device flow.

If you find this package useful, consider [sponsoring @zombiezen][],
the author and maintainer.

[gg]: https://gg-scm.io/
[sponsoring @zombiezen]: https://github.com/sponsors/zombiezen
[OAuth device flow]: https://docs.github.com/en/developers/apps/authorizing-oauth-apps#device-flow

## Example

```go
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
  return err
}

// Use the access token to make GitHub API requests.
ts := oauth2.StaticTokenSource(&oauth2.Token{
  AccessToken: token,
})
ghClient := github.NewClient(oauth2.NewClient(ctx, ts))
ghClient.UserAgent = userAgent
repos, _, err := ghClient.Repositories.List(ctx, "", nil)
if err != nil {
  return err
}
```

## Installation

```shell
go get gg-scm.io/pkg/ghdevice
```

To run the CLI:

```shell
go get gg-scm.io/pkg/ghdevice/cmd/ghtoken
ghtoken -help
```

## License

[Apache 2.0](LICENSE)
