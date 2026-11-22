import sys

from proofread_assist.cli import main

if __name__ == "__main__":
    # The Windows ANSI code page cannot represent Cyrillic in redirected output.
    if sys.platform == "win32":
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    raise SystemExit(main())
