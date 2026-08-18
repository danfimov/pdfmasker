# Contributing to pdfmasker

pdfmasker is a pure-Python library that masks sensitive text in PDFs in-process. Masking is composed from four
pluggable roles, so each axis can vary on its own. The architecture and repository layout are described below; deeper
engine notes live in [`AGENTS.md`](AGENTS.md).

## Architecture

```
mask_pdf(data, patterns) / Masker.mask(data, patterns)
  ├─ Backend     — read & rewrite the document (pikepdf now, OCR later)
  ├─ Detector    — decide what to mask from the text (optional)
  ├─ Editor      — decide how each match is rendered
  └─ SubstitutionStore — keep replacements consistent & collision-free
```

`Masker` picks the first backend that applies, gathers detections (literal `patterns` plus any configured detectors),
resolves each distinct target to a substitution via the editor, and lets the backend apply the plan. Passing only
`patterns` keeps every document on the single-pass path (no text extraction).

## Repository layout

```
src/pdfmasker/
  abc/            interfaces (Protocols) + the data types they exchange
  backends/       backend implementations (pikepdf: engine + adapter)
  detectors/      LiteralDetector, RegexDetector
  editors/      FixedCharEditor, FixedStringEditor, PseudonymizeEditor, KeyedPseudonymizeEditor
  stores/         InMemorySubstitutionStore
  masker.py       the orchestrator + mask_pdf convenience
  errors.py       the exception hierarchy
tests/            end-to-end tests + fixtures under tests/fixtures/
```

## Prerequisites

- **[uv](https://docs.astral.sh/uv/)** — manages the Python environment, dependency groups, and builds.
- **Python 3.10+** (the supported floor; CI tests 3.12–3.14).

## Getting started

```bash
make init      # create the virtualenv and install the git hooks
make test      # run the full suite
```

`make init` also installs the [prek](https://prek.j178.dev/) git hooks from `.pre-commit-config.yaml`: ruff (check with
autofix, and format) on staged Python, and — whenever anything under `.github/` changes — a zizmor audit of the
workflows. Run them by hand across the whole tree with `uv run prek run --all-files`.

## Common commands

All day-to-day tasks are wrapped as Makefile targets — run `make help` to list them.

| Command       | What it does                                            |
| ------------- | ------------------------------------------------------- |
| `make test`   | Run the test suite (`uv run pytest`)                    |
| `make lint`   | `ruff check` the sources and audit the workflows        |
| `make format` | Auto-format with ruff                                   |
| `make build`  | Build the wheel                                         |
| `make clean`  | Remove build artifacts                                  |

## Running a single test

```bash
uv run pytest tests/pikepdf/test_mask.py::test_masks_object_stream_pdf
```

## Benchmarks

Performance tests live in `tests/pikepdf/test_perfomance.py` and run under [CodSpeed](https://codspeed.io):

```bash
uv run pytest tests/ --codspeed
```

## Building wheels

The package is pure Python, so a single universal wheel (`py3-none-any`) covers every platform:

```bash
make build          # or: uv build --wheel
uv build            # sdist + wheel
```

## Releasing

Releases are automated by [`.github/workflows/release.yml`](.github/workflows/release.yml). Pushing a `v*` tag (e.g.
`v1.2.3`) sets the version from the tag, builds the sdist and wheel, and publishes to PyPI via trusted publishing
(OIDC — no API tokens). This requires a configured PyPI publisher and the `release` GitHub Environment.

## Before opening a pull request

Run `make lint` and `make test` locally. CI ([`.github/workflows/tests.yml`](.github/workflows/tests.yml)) runs the same
checks — ruff and the Python suite across Python 3.12–3.14 — on every pull request to `main`.
