# Silo Sportarr Plugin

First-party Silo metadata plugin backed by [Sportarr](https://sportarr.net). Provides sports league metadata as TV shows, with series, seasons, and episodes mapped from leagues, seasons, and events.

## Dependency Model

This repository consumes `github.com/Silo-Server/silo-plugin-sdk` as a normal Go module dependency. CI and release builds run with `GOWORK=off` and expect the SDK version in `go.mod` to resolve from a published semver tag.

For local multi-repo development, use a temporary `replace` or a local `go.work` that points at `dev/github/silo-plugin-sdk`. Do not commit machine-local filesystem replaces as the supported release path.

## Development

```sh
go test ./...
go build .
```

## Attribution

Metadata provided by [Sportarr](https://sportarr.net).

## License

`silo-plugin-metadata-sportarr` is licensed under `AGPL-3.0-or-later`. See [LICENSE](LICENSE).
