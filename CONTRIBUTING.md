# Contributing

Thanks for taking the time to improve Kine.

## Requirements

- Go 1.24 or later.
- JDK 17 or later for JVM, desktop, Android, and Minecraft examples.
- Android SDK and `adb` for Android work.

Kine does not download JDKs. Set `JAVA_HOME` or install a JDK under a path Kine scans. On Windows, see the Windows section in the README.

## Build and test

```sh
go build ./...
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
```

`gofmt -l .` must print nothing.

From Linux, check that the Windows build still compiles:

```sh
GOOS=windows go build ./...
GOOS=windows go vet ./...
```

## Examples

Examples are part of the test surface. If a change touches a provider, run the matching example from its directory:

```sh
cd examples/demo
go run ../../cmd/kine
```

Android and desktop examples need the external tools listed above.

## Pull requests

- Keep the change focused.
- Add or update tests for behavior changes.
- Update the README when a user-facing command or config field changes.
- Run the build and tests before you open the pull request.
- Describe what changed and why. Include the commands you ran.

## License

By contributing, you agree that your contribution is licensed under the MIT License.
