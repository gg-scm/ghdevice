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
