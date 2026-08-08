# Contributing to pdfmasker

pdfmasker is a hybrid Go + Python project: the masking core is a Go engine (`internal/masker`) compiled to a static
binary (`masker`) that is bundled inside the Python wheel and driven over a subprocess pipe. Most changes to masking
behavior live in Go; the Python layer (`src/pdfmasker`) is a thin, extensible pipeline. The architecture and repository
layout are described below; deeper engine notes live in [`CLAUDE.md`](CLAUDE.md).

## Architecture

```
mask_pdf(data, patterns)
  └─ Masker (pipeline.py) — routes to the first strategy that applies
       ├─ TextLayerStrategy  → masker (Go binary) via subprocess   [implemented]
       └─ OCR strategy       → scanned/image PDFs                     [future]
```

- **Text-layer** masking (the common case) is delegated to the `masker` Go binary: PDF on stdin, masked PDF on stdout,
  per-target counts as JSON on stderr.
- **OCR** masking (for scanned / image-only PDFs) is a deliberate extension point. Scanned and text-layer PDFs are
  disjoint inputs, so a future scan-detecting strategy slots into the pipeline ahead of the text one without changing
  it — `Masker` takes an ordered list of strategies and routes to the first that applies:

  ```python
  from pdfmasker import Masker, TextLayerStrategy
  masker = Masker(strategies=[TextLayerStrategy()])
  ```

## Repository layout

```
cmd/masker/         Go stdin->stdout wrapper over the masking core
internal/masker/    the Go masking engine
src/pdfmasker/      the Python package
  strategies/       MaskStrategy implementations
  _bin/             the compiled binary (built into the wheel; git-ignored)
hatch_build.py      build hook: compiles the Go binary into the wheel
```

## Prerequisites

- The **Go toolchain** (version per [`go.mod`](go.mod)) — every `uv run` and `uv build` compiles the Go binary via the
  hatch build hook, so Go must be on PATH even for Python-only work.
- **[uv](https://docs.astral.sh/uv/)** — manages the Python environment, dependency groups, and builds.
- **Python 3.12+** (the supported floor; CI also tests 3.13 and 3.14).

## Getting started

```bash
make init      # create the virtualenv and install the git hooks
make test      # build the binary and run the full suite
```

Dependencies sync lazily: the first `uv run` (or `make test`) resolves the `dev` dependency group and installs the
package, which triggers the Go build.

`make init` also installs the [prek](https://prek.j178.dev/) git hooks from `.pre-commit-config.yaml`: ruff (check with
autofix, and format) on staged Python, and — whenever anything under `.github/` changes — a zizmor audit of the
workflows. Run them by hand across the whole tree with `uv run prek run --all-files`.

## Common commands

All day-to-day tasks are wrapped as Makefile targets — run `make help` to list them.

| Command         | What it does                                                              |
| --------------- | ------------------------------------------------------------------------- |
| `make test`     | Full suite: Go engine tests (`test-go`) then Python tests (`test-py`)     |
| `make test-go`  | `go vet` + `go test ./internal/... -race`                                 |
| `make test-py`  | `uv run pytest` (end-to-end, through the real binary)                     |
| `make lint`     | `ruff check` the sources and audit the workflows with zizmor              |
| `make format`   | Auto-format Python with ruff and Go with gofmt                            |
| `make binary`   | Build the Go binary into `src/pdfmasker/_bin/` for local iteration        |
| `make build`    | Build the native platform wheel                                           |
| `make clean`    | Remove `dist/` and the compiled binary                                    |

## Running a single test

```bash
# one Python test
uv run pytest tests/test_mask.py::test_masks_object_stream_pdf_hybrid_path

# one Go test / a benchmark
go test ./internal/masker -run TestFold
go test ./internal/masker -bench . -benchmem
```

The Go and Python suites share the fixtures under `tests/fixtures/`; the Go tests load them via `../../tests/fixtures`,
so keep that directory in place.

## Faster Go ↔ Python iteration

`uv run pytest` reinstalls the package and recompiles the binary on every run. To skip that while iterating on the Go
engine, build once and point the Python layer at it with the `PDFMASKER_BINARY` environment variable, which takes
precedence over the bundled `_bin/masker`:

```bash
go build -o /tmp/masker ./cmd/masker      # or: make binary
PDFMASKER_BINARY=/tmp/masker uv run pytest
```

## Building wheels

The wheel is `py3-none-<platform>` — Python-agnostic but platform-specific, one wheel per OS/arch. Because the binary is
fully static (`CGO_ENABLED=0`), every platform cross-compiles from any host with no Docker and no auditwheel step; the
Linux wheel is tagged for manylinux2014.

```bash
make build                              # native wheel for this machine
GOOS=linux GOARCH=amd64 uv build --wheel  # cross-build for another platform
```

> **Note:** the wheel's `_bin/*` artifact glob packages whatever is in `src/pdfmasker/_bin/`. When cross-building
> several platforms in one checkout, delete the directory (`make clean`, or `rm -rf src/pdfmasker/_bin`) between builds
> so a previous platform's binary doesn't leak into the next wheel. The release workflow does this automatically.

## Releasing

Releases are automated by [`.github/workflows/release.yml`](.github/workflows/release.yml). Pushing a `v*` tag (e.g.
`v1.2.3`) sets the version from the tag, cross-builds the sdist and every platform wheel on a single Linux runner, and
publishes to PyPI via trusted publishing (OIDC — no API tokens). This requires a configured PyPI publisher and the
`release` GitHub Environment.

## Before opening a pull request

Run `make lint` and `make test` locally. CI ([`.github/workflows/tests.yml`](.github/workflows/tests.yml)) runs the same
checks — ruff, the Go engine tests, and the Python suite across Python 3.12–3.14 — on every pull request to `main`.
