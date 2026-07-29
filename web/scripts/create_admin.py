#!/usr/bin/env python3
"""Create (or reset the password of) an admin user.

Self-registration is disabled in the API, so this is the only way to get the
first account into a fresh install.

Usage:
    /opt/imapsync-web/venv/bin/python3 scripts/create_admin.py
    /opt/imapsync-web/venv/bin/python3 scripts/create_admin.py --username admin --email admin@example.com

The password is read interactively; pass --password only for unattended
installs, and remember it will show up in your shell history.
"""

import argparse
import getpass
import os
import sys
from pathlib import Path

# The app reads .env and resolves DATABASE_URL relative to the working
# directory, so run from wherever .env actually lives: the install root in
# production (systemd sets the same WorkingDirectory), backend/ in a dev tree.
ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))
os.chdir(next(
    (d for d in (ROOT, ROOT / "backend") if (d / ".env").is_file()),
    ROOT,
))

from backend.app.database import SessionLocal, init_db  # noqa: E402
from backend.app.models import User  # noqa: E402
from backend.app.security import hash_password  # noqa: E402


def main() -> int:
    parser = argparse.ArgumentParser(description="Create an admin user for imapsync Web UI")
    parser.add_argument("--username", help="Admin username")
    parser.add_argument("--email", help="Admin email")
    parser.add_argument("--password", help="Admin password (prompted if omitted)")
    args = parser.parse_args()

    username = args.username or input("Username: ").strip()
    email = args.email or input("Email: ").strip()

    if not username or not email:
        print("Username and email are required.", file=sys.stderr)
        return 1

    password = args.password
    if not password:
        password = getpass.getpass("Password: ")
        if password != getpass.getpass("Confirm password: "):
            print("Passwords do not match.", file=sys.stderr)
            return 1

    if len(password) < 8:
        print("Password must be at least 8 characters.", file=sys.stderr)
        return 1

    init_db()
    db = SessionLocal()
    try:
        existing = db.query(User).filter(User.username == username).first()
        if existing:
            existing.hashed_password = hash_password(password)
            existing.is_admin = True
            db.commit()
            print(f"Updated password for existing user '{username}'.")
            return 0

        db.add(User(
            username=username,
            email=email,
            hashed_password=hash_password(password),
            is_admin=True,
        ))
        db.commit()
        print(f"Admin user '{username}' created.")
        return 0
    finally:
        db.close()


if __name__ == "__main__":
    raise SystemExit(main())
