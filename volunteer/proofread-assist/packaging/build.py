"""Build and archive a native, self-contained executable on this host."""
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile

import certifi

ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT / "dist"
with tempfile.TemporaryDirectory() as scratch:
    subprocess.run([
        sys.executable, "-m", "PyInstaller", "--onefile", "--noconfirm",
        "--name", "proofread-assist", "--paths", str(ROOT),
        "--runtime-hook", str(ROOT / "packaging" / "tls.py"),
        "--add-data", f"{certifi.where()}:.",
        "--distpath", str(DIST), "--workpath", scratch, "--specpath", scratch,
        str(ROOT / "packaging" / "entry.py"),
    ], check=True, cwd=ROOT)

windows = platform.system() == "Windows"
binary = DIST / ("proofread-assist.exe" if windows else "proofread-assist")
name, version = subprocess.check_output([binary, "--version"], text=True).strip().split()
subprocess.run([binary, "run", "--help"], check=True)
archive = f"{name}-{version}-{platform.system().lower()}-{platform.machine().lower()}"
print(shutil.make_archive(str(DIST / archive), "zip" if windows else "gztar",
                          root_dir=DIST, base_dir=binary.name))
