import os
import subprocess
import sys
from pathlib import Path

from hatchling.builders.hooks.plugin.interface import BuildHookInterface

# Go GOOS/GOARCH -> Python wheel platform tag fragment.
_LINUX_ARCH = {"amd64": "x86_64", "arm64": "aarch64"}
_MACOS_ARCH = {"amd64": "x86_64", "arm64": "arm64"}


def _host_goos() -> str:
    if sys.platform.startswith("linux"):
        return "linux"
    if sys.platform == "darwin":
        return "darwin"
    if sys.platform == "win32":
        return "windows"
    return sys.platform


def _host_goarch() -> str:
    machine = os.uname().machine.lower() if hasattr(os, "uname") else ""
    if machine in ("x86_64", "amd64"):
        return "amd64"
    if machine in ("arm64", "aarch64"):
        return "arm64"
    return machine or "amd64"


def _platform_tag(goos: str, goarch: str) -> str:
    if goos == "linux":
        arch = _LINUX_ARCH.get(goarch, goarch)
        # Static binary => broadly compatible; manylinux2014 (glibc 2.17) is safe.
        return f"manylinux_2_17_{arch}.manylinux2014_{arch}"
    if goos == "darwin":
        arch = _MACOS_ARCH.get(goarch, goarch)
        return f"macosx_11_0_{arch}"
    if goos == "windows":
        return "win_amd64" if goarch == "amd64" else f"win_{goarch}"
    return f"{goos}_{goarch}"


class CustomBuildHook(BuildHookInterface):
    """Compile `cmd/masker` and package the resulting binary."""

    def initialize(self, version: str, build_data: dict) -> None:  # noqa: ARG002, D102
        root = Path(self.root)

        goos = os.environ.get("GOOS") or _host_goos()
        goarch = os.environ.get("GOARCH") or _host_goarch()

        binary_name = "masker.exe" if goos == "windows" else "masker"
        out_dir = root / "src" / "pdfmasker" / "_bin"
        out_dir.mkdir(parents=True, exist_ok=True)
        binary = out_dir / binary_name

        env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"}
        cmd = ["go", "build", "-trimpath", "-o", str(binary), "./cmd/masker"]
        self.app.display_info(f"pdfmasker: building {binary_name} for {goos}/{goarch}")
        subprocess.run(cmd, cwd=root, check=True, env=env)  # noqa: S603

        build_data["pure_python"] = False
        build_data["tag"] = f"py3-none-{_platform_tag(goos, goarch)}"
        build_data["force_include"][str(binary)] = f"pdfmasker/_bin/{binary_name}"
