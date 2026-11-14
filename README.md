# HIDI

Flexible HID to MIDI translation layer: play your computer keyboard as a MIDI
instrument, and use game controllers for notes, pitch bend and control changes.

![HIDI logo](./docs/hidi-logo.png)

> **This is the development branch for HIDI 2.0.** It is a rebuild and is not
> usable yet. For the released version, see the
> [`main` branch](https://github.com/gethiox/HIDI/tree/main) and the
> [releases page](https://github.com/gethiox/HIDI/releases).

## What 2.0 changes

- Runs on Linux and Windows 11 (25H2 or later).
- Every input device becomes its own MIDI device, which appears and disappears
  with the hardware.
- A terminal interface for headless use, plus a tray icon and a graphical
  interface.
- A new configuration format. Configuration files from HIDI 1.x are not
  compatible.

The planned structure is described in [docs/architecture.md](docs/architecture.md).

## Building

HIDI needs Go 1.24 or later and no C toolchain.

```bash
go build ./cmd/hidi
```

Cross-compiling works with the usual Go variables, for example:

```bash
GOOS=windows GOARCH=amd64 go build ./cmd/hidi
```

## Development

- `main` holds the released 1.x code and is not changed until 2.0 is ready.
- `dev` is the integration branch for 2.0. Changes reach it only through pull
  requests.
- Every pull request must pass CI: `gofmt`, `go vet`, `staticcheck`, the tests,
  and builds for all supported targets.

Run the same checks locally before opening a pull request:

```bash
gofmt -l .
```

```bash
go vet ./...
```

```bash
go test -race ./...
```

## License

HIDI is released under **GPLv3**, see [LICENSE](./LICENSE).
