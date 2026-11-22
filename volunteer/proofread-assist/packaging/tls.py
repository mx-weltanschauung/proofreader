"""Use bundled CA roots when the build interpreter's CA file is absent."""
import os
from pathlib import Path
import ssl
import sys

if not ssl.get_default_verify_paths().cafile:
    os.environ.setdefault("SSL_CERT_FILE", str(Path(sys._MEIPASS) / "cacert.pem"))
